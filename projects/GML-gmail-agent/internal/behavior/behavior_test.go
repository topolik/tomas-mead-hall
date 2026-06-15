package behavior

import (
	"fmt"
	"math"
	"testing"

	"github.com/topolik/gml-gmail-agent/internal/gws"
)

type mockGmailClient struct {
	messages map[string]*gws.Message
	threads  map[string]*gws.Thread
	refs     []gws.MessageRef
}

func (m *mockGmailClient) ListMessages(query string, maxPages int) ([]gws.MessageRef, error) {
	return m.refs, nil
}

func (m *mockGmailClient) GetMessage(id string) (*gws.Message, error) {
	msg, ok := m.messages[id]
	if !ok {
		return nil, fmt.Errorf("message %s not found", id)
	}
	return msg, nil
}

func (m *mockGmailClient) ListThreads(query string, maxPages int) ([]gws.ThreadRef, error) {
	return nil, nil
}

func (m *mockGmailClient) GetThread(id string) (*gws.Thread, error) {
	t, ok := m.threads[id]
	if !ok {
		return nil, fmt.Errorf("thread %s not found", id)
	}
	return t, nil
}

func makeMsg(id, from string, labels []string) *gws.Message {
	return &gws.Message{
		ID:      id,
		Labels:  labels,
		Payload: gws.Payload{Headers: []gws.Header{{Name: "From", Value: from}}},
	}
}

func TestCollectSenderBehavior_BasicStats(t *testing.T) {
	client := &mockGmailClient{
		refs: []gws.MessageRef{
			{ID: "1", ThreadID: "t1"},
			{ID: "2", ThreadID: "t1"},
			{ID: "3", ThreadID: "t2"},
			{ID: "4", ThreadID: "t3"},
		},
		messages: map[string]*gws.Message{
			"1": makeMsg("1", "Alice <alice@example.com>", []string{"INBOX"}),
			"2": makeMsg("2", "Alice <alice@example.com>", []string{"INBOX"}),
			"3": makeMsg("3", "Alice <alice@example.com>", []string{"INBOX", "UNREAD"}),
			"4": makeMsg("4", "Alice <alice@example.com>", []string{"INBOX"}),
		},
		threads: map[string]*gws.Thread{
			"t1": {ID: "t1", Messages: []gws.Message{
				{ID: "1", Labels: []string{"INBOX"}},
				{ID: "r1", Labels: []string{"SENT"}},
			}},
			"t2": {ID: "t2", Messages: []gws.Message{
				{ID: "3", Labels: []string{"INBOX", "UNREAD"}},
			}},
			"t3": {ID: "t3", Messages: []gws.Message{
				{ID: "4", Labels: []string{"INBOX"}},
				{ID: "r2", Labels: []string{"SENT"}},
			}},
		},
	}

	results, err := CollectSenderBehavior(client, 30, 30, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d senders, want 1", len(results))
	}

	s := results[0]
	if s.Email != "alice@example.com" {
		t.Errorf("email: got %q want %q", s.Email, "alice@example.com")
	}
	if s.TotalEmails != 4 {
		t.Errorf("total: got %d want 4", s.TotalEmails)
	}
	if s.ReadCount != 3 {
		t.Errorf("read count: got %d want 3", s.ReadCount)
	}
	if math.Abs(s.ReadRate-0.75) > 0.01 {
		t.Errorf("read rate: got %.2f want 0.75", s.ReadRate)
	}
	if s.ThreadCount != 3 {
		t.Errorf("thread count: got %d want 3", s.ThreadCount)
	}
	if s.RepliedThreads != 2 {
		t.Errorf("replied threads: got %d want 2", s.RepliedThreads)
	}
	if math.Abs(s.ReplyRate-0.6667) > 0.01 {
		t.Errorf("reply rate: got %.4f want ~0.6667", s.ReplyRate)
	}
}

func TestCollectSenderBehavior_MinEmailsFilter(t *testing.T) {
	client := &mockGmailClient{
		refs: []gws.MessageRef{
			{ID: "1", ThreadID: "t1"},
			{ID: "2", ThreadID: "t2"},
		},
		messages: map[string]*gws.Message{
			"1": makeMsg("1", "bob@example.com", []string{"INBOX"}),
			"2": makeMsg("2", "bob@example.com", []string{"INBOX"}),
		},
		threads: map[string]*gws.Thread{},
	}

	results, err := CollectSenderBehavior(client, 30, 30, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("got %d senders, want 0 (below min threshold)", len(results))
	}
}

func TestCollectSenderBehavior_TopNLimit(t *testing.T) {
	refs := make([]gws.MessageRef, 0)
	messages := make(map[string]*gws.Message)
	threads := make(map[string]*gws.Thread)

	for i := 0; i < 5; i++ {
		email := fmt.Sprintf("sender%d@example.com", i)
		for j := 0; j < 3+i; j++ {
			id := fmt.Sprintf("%d-%d", i, j)
			tid := fmt.Sprintf("t%d-%d", i, j)
			refs = append(refs, gws.MessageRef{ID: id, ThreadID: tid})
			messages[id] = makeMsg(id, email, []string{"INBOX"})
			threads[tid] = &gws.Thread{ID: tid, Messages: []gws.Message{{ID: id, Labels: []string{"INBOX"}}}}
		}
	}

	client := &mockGmailClient{refs: refs, messages: messages, threads: threads}

	results, err := CollectSenderBehavior(client, 30, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("got %d senders, want 2 (topN limit)", len(results))
	}
}

func TestCollectSenderBehavior_EmptyInbox(t *testing.T) {
	client := &mockGmailClient{
		refs:     nil,
		messages: map[string]*gws.Message{},
		threads:  map[string]*gws.Thread{},
	}

	results, err := CollectSenderBehavior(client, 30, 30, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("got %d senders, want 0", len(results))
	}
}

func TestExtractEmail(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"Alice <alice@example.com>", "alice@example.com"},
		{"alice@example.com", "alice@example.com"},
		{"\"Alice Smith\" <Alice@Example.COM>", "alice@example.com"},
		{"", ""},
	}
	for _, tt := range tests {
		got := extractEmail(tt.input)
		if got != tt.want {
			t.Errorf("extractEmail(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
