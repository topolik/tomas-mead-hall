package router

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/topolik/llp-llm-proxy/internal/provider"
	"github.com/topolik/llp-llm-proxy/internal/registry"
)

// fakeProvider is a programmable provider for router tests.
type fakeProvider struct {
	name  string
	fn    func(call int) (provider.Response, error)
	delay time.Duration

	calls   int32
	cur     int32
	maxConc int32
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Generate(ctx context.Context, _ provider.Request) (provider.Response, error) {
	n := atomic.AddInt32(&f.calls, 1)
	c := atomic.AddInt32(&f.cur, 1)
	for {
		m := atomic.LoadInt32(&f.maxConc)
		if c <= m || atomic.CompareAndSwapInt32(&f.maxConc, m, c) {
			break
		}
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	atomic.AddInt32(&f.cur, -1)
	if f.fn != nil {
		return f.fn(int(n))
	}
	return provider.Response{Content: f.name}, nil
}

func impl(name string, p provider.Provider, cooldown time.Duration, conc int) *registry.Impl {
	return &registry.Impl{Name: name, Provider: p, Cooldown: cooldown, Concurrency: conc}
}

func ok(content string) (provider.Response, error) {
	return provider.Response{Content: content}, nil
}
func retryable() (provider.Response, error) {
	return provider.Response{}, &provider.Error{Retryable: true, Err: fmt.Errorf("transient")}
}
func rateLimited() (provider.Response, error) {
	return provider.Response{}, &provider.Error{Retryable: true, RateLimit: true, Err: fmt.Errorf("429")}
}
func quotaExhausted() (provider.Response, error) {
	return provider.Response{}, &provider.Error{Retryable: true, RateLimit: true, QuotaExhausted: true, Err: fmt.Errorf("TerminalQuotaError")}
}
func terminal() (provider.Response, error) {
	return provider.Response{}, &provider.Error{Retryable: false, Err: fmt.Errorf("bad request")}
}

// T5: retryable failure on A => B serves.
func TestFailoverOnRetryable(t *testing.T) {
	a := &fakeProvider{name: "A", fn: func(int) (provider.Response, error) { return retryable() }}
	b := &fakeProvider{name: "B", fn: func(int) (provider.Response, error) { return ok("from-B") }}
	r := New(nil)
	used, resp, err := r.Do(context.Background(), []*registry.Impl{impl("A", a, time.Minute, 1), impl("B", b, time.Minute, 1)}, provider.Request{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if used != "B" || resp.Content != "from-B" {
		t.Fatalf("expected B to serve, got used=%s content=%s", used, resp.Content)
	}
}

// T5: terminal failure on A => stop, B never called.
func TestTerminalStopsChain(t *testing.T) {
	a := &fakeProvider{name: "A", fn: func(int) (provider.Response, error) { return terminal() }}
	b := &fakeProvider{name: "B", fn: func(int) (provider.Response, error) { return ok("B") }}
	r := New(nil)
	_, _, err := r.Do(context.Background(), []*registry.Impl{impl("A", a, time.Minute, 1), impl("B", b, time.Minute, 1)}, provider.Request{})
	if err == nil {
		t.Fatal("expected terminal error")
	}
	if atomic.LoadInt32(&b.calls) != 0 {
		t.Fatalf("B should not have been called, calls=%d", b.calls)
	}
}

// T5: all retryable => aggregate error.
func TestAllFail(t *testing.T) {
	a := &fakeProvider{name: "A", fn: func(int) (provider.Response, error) { return retryable() }}
	b := &fakeProvider{name: "B", fn: func(int) (provider.Response, error) { return retryable() }}
	r := New(nil)
	_, _, err := r.Do(context.Background(), []*registry.Impl{impl("A", a, time.Minute, 1), impl("B", b, time.Minute, 1)}, provider.Request{})
	if err == nil {
		t.Fatal("expected aggregate error")
	}
}

// T5: a rate-limited impl is put on cooldown and skipped until it expires.
func TestCooldownSkipsThenRecovers(t *testing.T) {
	a := &fakeProvider{name: "A", fn: func(int) (provider.Response, error) { return rateLimited() }}
	b := &fakeProvider{name: "B", fn: func(int) (provider.Response, error) { return ok("B") }}
	clock := time.Unix(1_000_000, 0)
	r := New(nil)
	r.SetClock(func() time.Time { return clock })

	A := impl("A", a, 60*time.Second, 1)
	B := impl("B", b, time.Minute, 1)
	chain := []*registry.Impl{A, B}

	// 1st call: A rate-limited -> cooldown, B serves.
	used, _, err := r.Do(context.Background(), chain, provider.Request{})
	if err != nil || used != "B" {
		t.Fatalf("call1 used=%s err=%v", used, err)
	}
	callsAfter1 := atomic.LoadInt32(&a.calls)

	// 2nd call within cooldown: A skipped (no new call), B serves.
	clock = clock.Add(10 * time.Second)
	used, _, _ = r.Do(context.Background(), chain, provider.Request{})
	if used != "B" {
		t.Fatalf("call2 expected B, got %s", used)
	}
	if atomic.LoadInt32(&a.calls) != callsAfter1 {
		t.Fatalf("A should have been skipped during cooldown")
	}

	// After cooldown expires, A is tried again (and rate-limits again).
	clock = clock.Add(60 * time.Second)
	_, _, _ = r.Do(context.Background(), chain, provider.Request{})
	if atomic.LoadInt32(&a.calls) <= callsAfter1 {
		t.Fatalf("A should be retried after cooldown expiry")
	}
}

// A quota-exhausted impl gets the longer quota_cooldown, not the throttle cooldown.
func TestQuotaCooldownOutlivesThrottleCooldown(t *testing.T) {
	a := &fakeProvider{name: "A", fn: func(int) (provider.Response, error) { return quotaExhausted() }}
	b := &fakeProvider{name: "B", fn: func(int) (provider.Response, error) { return ok("B") }}
	clock := time.Unix(1_000_000, 0)
	r := New(nil)
	r.SetClock(func() time.Time { return clock })

	A := impl("A", a, 60*time.Second, 1)
	A.QuotaCooldown = 30 * time.Minute
	chain := []*registry.Impl{A, impl("B", b, time.Minute, 1)}

	used, _, err := r.Do(context.Background(), chain, provider.Request{})
	if err != nil || used != "B" {
		t.Fatalf("call1 used=%s err=%v", used, err)
	}
	callsAfter1 := atomic.LoadInt32(&a.calls)

	// Past the 60s throttle cooldown but inside the 30m quota cooldown: A stays skipped.
	clock = clock.Add(5 * time.Minute)
	if used, _, _ = r.Do(context.Background(), chain, provider.Request{}); used != "B" {
		t.Fatalf("call2 expected B, got %s", used)
	}
	if atomic.LoadInt32(&a.calls) != callsAfter1 {
		t.Fatalf("A should still be cooling down after 5m (quota_cooldown=30m)")
	}

	// After the quota cooldown expires, A is tried again.
	clock = clock.Add(30 * time.Minute)
	_, _, _ = r.Do(context.Background(), chain, provider.Request{})
	if atomic.LoadInt32(&a.calls) <= callsAfter1 {
		t.Fatalf("A should be retried after quota cooldown expiry")
	}
}

// Without quota_cooldown configured, a quota-exhausted error falls back to the
// regular cooldown (QuotaExhausted implies RateLimit).
func TestQuotaExhaustedFallsBackToCooldown(t *testing.T) {
	a := &fakeProvider{name: "A", fn: func(int) (provider.Response, error) { return quotaExhausted() }}
	b := &fakeProvider{name: "B", fn: func(int) (provider.Response, error) { return ok("B") }}
	clock := time.Unix(1_000_000, 0)
	r := New(nil)
	r.SetClock(func() time.Time { return clock })

	A := impl("A", a, 60*time.Second, 1) // QuotaCooldown unset
	chain := []*registry.Impl{A, impl("B", b, time.Minute, 1)}

	r.Do(context.Background(), chain, provider.Request{})
	if !r.IsCoolingDown("A") {
		t.Fatal("A should be on the regular cooldown")
	}
	clock = clock.Add(61 * time.Second)
	if r.IsCoolingDown("A") {
		t.Fatal("regular cooldown should have expired after 61s")
	}
}

// Outcome stats: failures accumulate per impl and reset on the next success.
func TestStatsTrackConsecutiveFailures(t *testing.T) {
	calls := 0
	a := &fakeProvider{name: "A", fn: func(int) (provider.Response, error) {
		calls++
		if calls <= 2 {
			return retryable()
		}
		return ok("A")
	}}
	clock := time.Unix(1_000_000, 0)
	r := New(nil)
	r.SetClock(func() time.Time { return clock })
	chain := []*registry.Impl{impl("A", a, 0, 1)} // no cooldown so every call reaches A

	r.Do(context.Background(), chain, provider.Request{})
	r.Do(context.Background(), chain, provider.Request{})
	st := r.StatsFor("A")
	if st.ConsecutiveFailures != 2 || st.LastError == "" || st.LastErrorAt.IsZero() {
		t.Fatalf("after 2 failures: %+v", st)
	}
	if !st.LastOKAt.IsZero() {
		t.Fatalf("no success yet, LastOKAt should be zero: %+v", st)
	}

	r.Do(context.Background(), chain, provider.Request{})
	st = r.StatsFor("A")
	if st.ConsecutiveFailures != 0 || st.LastOKAt.IsZero() {
		t.Fatalf("success should reset consecutive failures: %+v", st)
	}
	if st.LastError == "" {
		t.Fatalf("last error should remain visible for diagnosis: %+v", st)
	}
}

// T5: a stubbed (unavailable) provider is skipped.
func TestSkipUnavailable(t *testing.T) {
	disabled := provider.NewHttp(provider.HttpConfig{Name: "openllm", BaseURL: ""}) // Available() == false
	b := &fakeProvider{name: "B", fn: func(int) (provider.Response, error) { return ok("B") }}
	r := New(nil)
	used, _, err := r.Do(context.Background(), []*registry.Impl{impl("openllm", disabled, time.Minute, 1), impl("B", b, time.Minute, 1)}, provider.Request{})
	if err != nil || used != "B" {
		t.Fatalf("expected B, got used=%s err=%v", used, err)
	}
}

// T6: concurrency cap of 1 serializes calls to an impl.
func TestConcurrencyCapSerializes(t *testing.T) {
	a := &fakeProvider{name: "A", delay: 30 * time.Millisecond, fn: func(int) (provider.Response, error) { return ok("A") }}
	r := New([]*registry.Impl{impl("A", a, time.Minute, 1)})
	chain := []*registry.Impl{impl("A", a, time.Minute, 1)}

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Do(context.Background(), chain, provider.Request{})
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&a.maxConc); got != 1 {
		t.Fatalf("cap=1 should serialize, observed max concurrency %d", got)
	}
	if got := atomic.LoadInt32(&a.calls); got != 6 {
		t.Fatalf("expected 6 calls, got %d", got)
	}
}
