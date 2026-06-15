// Package router walks a failover chain of impls for a single request: it skips
// unavailable or cooling-down impls, bounds per-impl concurrency (the queue that
// guards against token exhaustion), and decides whether a failure fails over to
// the next impl or is returned to the client.
package router

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/topolik/llp-llm-proxy/internal/provider"
	"github.com/topolik/llp-llm-proxy/internal/registry"
)

type implState struct {
	sem           chan struct{} // capacity = concurrency; sending blocks => queues
	mu            sync.Mutex
	cooldownUntil time.Time
	// Outcome tracking so /healthz reflects serveability, not just config:
	// consecFails counts Generate failures since the last success.
	consecFails int
	lastErr     string
	lastErrAt   time.Time
	lastOKAt    time.Time
}

// Stats is a snapshot of an impl's recent outcomes (zero values = no traffic yet).
type Stats struct {
	ConsecutiveFailures int
	LastError           string
	LastErrorAt         time.Time
	LastOKAt            time.Time
}

// Router holds per-impl runtime state (concurrency slots + cooldown).
type Router struct {
	mu     sync.Mutex
	states map[string]*implState
	now    func() time.Time
}

// New pre-creates state for the given impls. Impls encountered later are created
// lazily on first use.
func New(impls []*registry.Impl) *Router {
	r := &Router{states: make(map[string]*implState), now: time.Now}
	for _, im := range impls {
		r.ensure(im)
	}
	return r
}

// SetClock overrides the time source (tests).
func (r *Router) SetClock(now func() time.Time) { r.now = now }

func (r *Router) ensure(im *registry.Impl) *implState {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.states[im.Name]
	if !ok {
		c := im.Concurrency
		if c <= 0 {
			c = 1
		}
		st = &implState{sem: make(chan struct{}, c)}
		r.states[im.Name] = st
	}
	return st
}

// Do runs req through the chain. It returns the name of the impl that served the
// request, the response, and any error. A terminal (non-retryable) error stops
// the walk immediately; retryable errors advance to the next impl.
func (r *Router) Do(ctx context.Context, chain []*registry.Impl, req provider.Request) (string, provider.Response, error) {
	if len(chain) == 0 {
		return "", provider.Response{}, errors.New("no impls available for requested model")
	}
	var lastErr error
	for _, im := range chain {
		if a, ok := im.Provider.(provider.Availabler); ok && !a.Available() {
			lastErr = fmt.Errorf("%s: not configured", im.Name)
			continue
		}
		st := r.ensure(im)
		if r.coolingDown(st) {
			lastErr = fmt.Errorf("%s: cooling down", im.Name)
			continue
		}

		// Acquire a concurrency slot. With capacity 1 this serializes calls to
		// the impl; a full channel makes additional callers queue here.
		select {
		case st.sem <- struct{}{}:
		case <-ctx.Done():
			return "", provider.Response{}, ctx.Err()
		}
		resp, err := im.Provider.Generate(ctx, req)
		<-st.sem
		r.recordOutcome(st, err)

		if err == nil {
			return im.Name, resp, nil
		}
		lastErr = err

		var pe *provider.Error
		if errors.As(err, &pe) {
			if pe.RateLimit && im.Cooldown > 0 || pe.QuotaExhausted && im.QuotaCooldown > 0 {
				d := im.Cooldown
				if pe.QuotaExhausted && im.QuotaCooldown > 0 {
					d = im.QuotaCooldown
				}
				r.setCooldown(st, d)
			}
			if !pe.Retryable {
				return im.Name, provider.Response{}, err // terminal: stop here
			}
		}
		// retryable or unknown error type: try the next impl
	}
	return "", provider.Response{}, fmt.Errorf("all impls failed: %w", lastErr)
}

func (r *Router) coolingDown(st *implState) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	return r.now().Before(st.cooldownUntil)
}

func (r *Router) setCooldown(st *implState, d time.Duration) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.cooldownUntil = r.now().Add(d)
}

// IsCoolingDown reports whether a named impl is currently on cooldown (for /healthz).
func (r *Router) IsCoolingDown(name string) bool {
	r.mu.Lock()
	st, ok := r.states[name]
	r.mu.Unlock()
	if !ok {
		return false
	}
	return r.coolingDown(st)
}

func (r *Router) recordOutcome(st *implState, err error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if err == nil {
		st.consecFails = 0
		st.lastOKAt = r.now()
		return
	}
	st.consecFails++
	st.lastErr = err.Error()
	st.lastErrAt = r.now()
}

// StatsFor returns the recent-outcome snapshot for a named impl (for /healthz).
func (r *Router) StatsFor(name string) Stats {
	r.mu.Lock()
	st, ok := r.states[name]
	r.mu.Unlock()
	if !ok {
		return Stats{}
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return Stats{
		ConsecutiveFailures: st.consecFails,
		LastError:           st.lastErr,
		LastErrorAt:         st.lastErrAt,
		LastOKAt:            st.lastOKAt,
	}
}
