package handler

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestThreadsPage_RendersTabsAndList(t *testing.T) {
	h := newTestUIHandler(t)
	id, _ := createThread(h.DB, "discuss the insight", "first message", "", "", "gml")
	_ = id
	createThread(h.DB, "resolved one", "done", "", "", "mnd")
	h.DB.Exec(`UPDATE threads SET status='resolved' WHERE subject='resolved one'`)

	req := httptest.NewRequest("GET", "/threads", nil)
	w := httptest.NewRecorder()
	h.ThreadsPage(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "discuss the insight") {
		t.Error("open thread missing from default view")
	}
	if strings.Contains(body, "resolved one") {
		t.Error("resolved thread shown in open view")
	}
	if !strings.Contains(body, "[Open (1)]") || !strings.Contains(body, "[Resolved (1)]") {
		t.Errorf("tab counts wrong:\n%s", body)
	}

	// Resolved tab shows the resolved thread.
	req = httptest.NewRequest("GET", "/threads?status=resolved", nil)
	w = httptest.NewRecorder()
	h.ThreadsPage(w, req)
	if !strings.Contains(w.Body.String(), "resolved one") {
		t.Error("resolved view missing resolved thread")
	}
}

func TestThreadDetailAndReply(t *testing.T) {
	h := newTestUIHandler(t)
	id, _ := createThread(h.DB, "subj", "opening message", "", "", "gml")

	req := httptest.NewRequest("GET", "/threads/1", nil)
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	h.ThreadDetailPage(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "opening message") {
		t.Error("message body not rendered")
	}

	// Reply via the form.
	form := url.Values{"body": {"a reply from the UI"}}
	req = httptest.NewRequest("POST", "/threads/1/reply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", "1")
	w = httptest.NewRecorder()
	h.ThreadReplyUI(w, req)
	if w.Code != 302 {
		t.Fatalf("reply: expected redirect, got %d", w.Code)
	}
	var count int
	h.DB.QueryRow(`SELECT COUNT(*) FROM thread_messages WHERE thread_id=?`, id).Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 messages after reply, got %d", count)
	}

	// Unknown thread → 404.
	req = httptest.NewRequest("GET", "/threads/999", nil)
	req.SetPathValue("id", "999")
	w = httptest.NewRecorder()
	h.ThreadDetailPage(w, req)
	if w.Code != 404 {
		t.Errorf("unknown thread: expected 404, got %d", w.Code)
	}
}

func TestThreadCreateUI_WithNotificationRef(t *testing.T) {
	h := newTestUIHandler(t)
	res, _ := h.DB.Exec(`INSERT INTO notifications(message, type) VALUES('the insight','info')`)
	nid, _ := res.LastInsertId()

	form := url.Values{
		"subject":  {"re: notification"},
		"body":     {"let's discuss"},
		"ref_type": {"notification"},
		"ref_id":   {itoa(nid)},
	}
	req := httptest.NewRequest("POST", "/threads", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.ThreadCreateUI(w, req)
	if w.Code != 302 {
		t.Fatalf("expected redirect, got %d", w.Code)
	}
	var refType, refID string
	h.DB.QueryRow(`SELECT ref_type, ref_id FROM threads`).Scan(&refType, &refID)
	if refType != "notification" || refID != itoa(nid) {
		t.Errorf("ref not stored: %q %q", refType, refID)
	}
}

func TestNotificationsPage_ThreadBadge(t *testing.T) {
	h := newTestUIHandler(t)
	res, _ := h.DB.Exec(`INSERT INTO notifications(message, type) VALUES('with thread','info')`)
	nid, _ := res.LastInsertId()
	h.DB.Exec(`INSERT INTO notifications(message, type) VALUES('without thread','info')`)
	tid, _ := createThread(h.DB, "discussion", "msg", "notification", itoa(nid), "gml")

	req := httptest.NewRequest("GET", "/notifications", nil)
	w := httptest.NewRecorder()
	h.NotificationsPage(w, req)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "/threads/"+itoa(tid)) {
		t.Error("notification with thread should link to it")
	}
	if !strings.Contains(body, "ref_id="+itoa(nid+1)) && !strings.Contains(body, "[discuss]") {
		t.Error("notification without thread should offer [discuss]")
	}
}
