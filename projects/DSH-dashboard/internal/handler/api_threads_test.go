package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func threadReq(t *testing.T, h *APIHandler, method, path, agent, body string, pathID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if agent != "" {
		req = req.WithContext(withValue(req.Context(), ctxAgent, agent))
	}
	if pathID != "" {
		req.SetPathValue("id", pathID)
	}
	w := httptest.NewRecorder()
	switch {
	case method == "POST" && strings.HasSuffix(path, "/messages"):
		h.PostThreadMessage(w, req)
	case method == "POST":
		h.CreateThread(w, req)
	case method == "PATCH":
		h.UpdateThread(w, req)
	case method == "GET" && pathID != "":
		h.GetThread(w, req)
	default:
		h.ListThreads(w, req)
	}
	return w
}

func TestCreateThread_WithRefAndAuthor(t *testing.T) {
	h := newTestHandler(t)
	res, _ := h.DB.Exec(`INSERT INTO notifications(message, type) VALUES('insight to discuss','info')`)
	nid, _ := res.LastInsertId()

	w := threadReq(t, h, "POST", "/api/v1/threads", "gml",
		`{"subject":"processed: insight","body":"distilled into rules.yaml","ref_type":"notification","ref_id":"`+itoa(nid)+`"}`, "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var subject, refType, refID, createdBy string
	h.DB.QueryRow(`SELECT subject, ref_type, ref_id, created_by FROM threads`).Scan(&subject, &refType, &refID, &createdBy)
	if subject != "processed: insight" || refType != "notification" || refID != itoa(nid) {
		t.Errorf("thread row wrong: %q %q %q", subject, refType, refID)
	}
	// Author comes from the authenticated context, never the payload.
	if createdBy != "gml" {
		t.Errorf("created_by should be the authenticated agent, got %q", createdBy)
	}
	var msgAuthor, msgBody string
	h.DB.QueryRow(`SELECT author, body FROM thread_messages`).Scan(&msgAuthor, &msgBody)
	if msgAuthor != "gml" || msgBody != "distilled into rules.yaml" {
		t.Errorf("first message wrong: %q %q", msgAuthor, msgBody)
	}
}

func TestCreateThread_Validation(t *testing.T) {
	h := newTestHandler(t)
	cases := []struct {
		name, body string
	}{
		{"missing subject", `{"body":"x"}`},
		{"missing body", `{"subject":"x"}`},
		{"bad ref_type", `{"subject":"s","body":"b","ref_type":"todo","ref_id":"1"}`},
		{"dangling ref", `{"subject":"s","body":"b","ref_type":"notification","ref_id":"9999"}`},
		{"ref_id without type", `{"subject":"s","body":"b","ref_id":"1"}`},
		{"oversized subject", `{"subject":"` + strings.Repeat("s", 201) + `","body":"b"}`},
		{"oversized body", `{"subject":"s","body":"` + strings.Repeat("b", 10001) + `"}`},
	}
	for _, c := range cases {
		if w := threadReq(t, h, "POST", "/api/v1/threads", "gml", c.body, ""); w.Code != 400 {
			t.Errorf("%s: expected 400, got %d", c.name, w.Code)
		}
	}
}

// The GML processed-tracking contract: a resolved thread ref'ing a notification
// is findable by ?ref_type=notification&ref_id=N&status=resolved.
func TestListThreads_GMLProcessedContract(t *testing.T) {
	h := newTestHandler(t)
	res, _ := h.DB.Exec(`INSERT INTO notifications(message, type, dismissed_at) VALUES('dismissed insight','info',datetime('now'))`)
	nid, _ := res.LastInsertId()

	// One resolved thread on the insight, one open thread elsewhere.
	id, err := createThread(h.DB, "processed", "folded into rules", "notification", itoa(nid), "gml")
	if err != nil {
		t.Fatal(err)
	}
	h.DB.Exec(`UPDATE threads SET status='resolved' WHERE id=?`, id)
	createThread(h.DB, "unrelated", "other topic", "", "", "mnd")

	w := threadReq(t, h, "GET", "/api/v1/threads?ref_type=notification&ref_id="+itoa(nid)+"&status=resolved", "gml", "", "")
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var threads []map[string]any
	json.Unmarshal(w.Body.Bytes(), &threads)
	if len(threads) != 1 {
		t.Fatalf("GML contract: expected exactly 1 resolved thread for the insight, got %d", len(threads))
	}
	if threads[0]["subject"] != "processed" || threads[0]["message_count"] != float64(1) {
		t.Errorf("wrong thread returned: %v", threads[0])
	}

	// Unprocessed insight (no resolved thread) → empty result.
	w = threadReq(t, h, "GET", "/api/v1/threads?ref_type=notification&ref_id=12345&status=resolved", "gml", "", "")
	threads = nil
	json.Unmarshal(w.Body.Bytes(), &threads)
	if len(threads) != 0 {
		t.Errorf("expected empty for unprocessed insight, got %d", len(threads))
	}
}

func TestGetThread_MessagesInOrder(t *testing.T) {
	h := newTestHandler(t)
	id, _ := createThread(h.DB, "subj", "first", "", "", "gml")
	addThreadMessage(h.DB, itoa(id), "tomas", "second")
	addThreadMessage(h.DB, itoa(id), "gml", "third")

	w := threadReq(t, h, "GET", "/api/v1/threads/"+itoa(id), "", "", itoa(id))
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var thread struct {
		Messages []struct {
			Author string `json:"author"`
			Body   string `json:"body"`
		} `json:"messages"`
	}
	json.Unmarshal(w.Body.Bytes(), &thread)
	if len(thread.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(thread.Messages))
	}
	if thread.Messages[0].Body != "first" || thread.Messages[2].Body != "third" {
		t.Errorf("messages out of order: %v", thread.Messages)
	}
	if thread.Messages[1].Author != "tomas" {
		t.Errorf("author lost: %v", thread.Messages[1])
	}

	if w := threadReq(t, h, "GET", "/api/v1/threads/999", "", "", "999"); w.Code != 404 {
		t.Errorf("unknown thread: expected 404, got %d", w.Code)
	}
}

func TestPostThreadMessage(t *testing.T) {
	h := newTestHandler(t)
	id, _ := createThread(h.DB, "subj", "first", "", "", "gml")

	var before string
	h.DB.QueryRow(`SELECT updated_at FROM threads WHERE id=?`, id).Scan(&before)
	// Force a visible updated_at change regardless of timer resolution.
	h.DB.Exec(`UPDATE threads SET updated_at=datetime('now','-1 hour') WHERE id=?`, id)
	h.DB.QueryRow(`SELECT updated_at FROM threads WHERE id=?`, id).Scan(&before)

	w := threadReq(t, h, "POST", "/api/v1/threads/"+itoa(id)+"/messages", "mnd", `{"body":"reply"}`, itoa(id))
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var count int
	h.DB.QueryRow(`SELECT COUNT(*) FROM thread_messages WHERE thread_id=?`, id).Scan(&count)
	if count != 2 {
		t.Errorf("expected 2 messages, got %d", count)
	}
	var author string
	h.DB.QueryRow(`SELECT author FROM thread_messages WHERE thread_id=? ORDER BY id DESC LIMIT 1`, id).Scan(&author)
	if author != "mnd" {
		t.Errorf("reply author should be authenticated agent, got %q", author)
	}
	var after string
	h.DB.QueryRow(`SELECT updated_at FROM threads WHERE id=?`, id).Scan(&after)
	if after <= before {
		t.Errorf("updated_at not bumped: %q -> %q", before, after)
	}

	if w := threadReq(t, h, "POST", "/api/v1/threads/999/messages", "mnd", `{"body":"x"}`, "999"); w.Code != 404 {
		t.Errorf("unknown thread: expected 404, got %d", w.Code)
	}
	if w := threadReq(t, h, "POST", "/api/v1/threads/"+itoa(id)+"/messages", "mnd", `{"body":""}`, itoa(id)); w.Code != 400 {
		t.Errorf("empty body: expected 400, got %d", w.Code)
	}
}

func TestUpdateThread_Status(t *testing.T) {
	h := newTestHandler(t)
	id, _ := createThread(h.DB, "subj", "first", "", "", "gml")

	if w := threadReq(t, h, "PATCH", "/api/v1/threads/"+itoa(id), "gml", `{"status":"resolved"}`, itoa(id)); w.Code != 200 {
		t.Fatalf("resolve: expected 200, got %d", w.Code)
	}
	var status string
	h.DB.QueryRow(`SELECT status FROM threads WHERE id=?`, id).Scan(&status)
	if status != "resolved" {
		t.Errorf("expected resolved, got %q", status)
	}

	if w := threadReq(t, h, "PATCH", "/api/v1/threads/"+itoa(id), "gml", `{"status":"closed"}`, itoa(id)); w.Code != 400 {
		t.Errorf("invalid status: expected 400, got %d", w.Code)
	}
	if w := threadReq(t, h, "PATCH", "/api/v1/threads/999", "gml", `{"status":"open"}`, "999"); w.Code != 404 {
		t.Errorf("unknown thread: expected 404, got %d", w.Code)
	}
}
