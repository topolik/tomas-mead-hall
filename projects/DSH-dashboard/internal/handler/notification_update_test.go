package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"dsh/internal/db"
)

func newTestHandler(t *testing.T) *APIHandler {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return &APIHandler{DB: database}
}

func patchNotification(t *testing.T, h *APIHandler, id, body string) int {
	t.Helper()
	req := httptest.NewRequest("PATCH", "/api/v1/notifications/"+id, strings.NewReader(body))
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	h.UpdateNotification(w, req)
	return w.Result().StatusCode
}

func TestUpdateNotification_ActiveUpdatedInPlace(t *testing.T) {
	h := newTestHandler(t)
	res, err := h.DB.Exec(`INSERT INTO notifications(project_code, message, type, priority, link) VALUES('GML','old msg','info','Q4','old-link')`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()

	code := patchNotification(t, h, itoa(id), `{"message":"new msg","link":"new-link","priority":"Q2"}`)
	if code != 200 {
		t.Fatalf("expected 200, got %d", code)
	}

	var msg, link, prio string
	h.DB.QueryRow(`SELECT message, link, COALESCE(priority,'') FROM notifications WHERE id=?`, id).Scan(&msg, &link, &prio)
	if msg != "new msg" || link != "new-link" || prio != "Q2" {
		t.Errorf("row not updated: msg=%q link=%q prio=%q", msg, link, prio)
	}
}

// The dismissed_at IS NULL guard must make an update of a dismissed notification
// a no-op (404) — a dismissed insight stays dismissed; it is never resurrected.
func TestUpdateNotification_DismissedIsGuarded(t *testing.T) {
	h := newTestHandler(t)
	res, err := h.DB.Exec(`INSERT INTO notifications(project_code, message, type, dismissed_at) VALUES('GML','dismissed msg','info',datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()

	code := patchNotification(t, h, itoa(id), `{"message":"hijack","link":"x"}`)
	if code != 404 {
		t.Fatalf("expected 404 for dismissed notification, got %d", code)
	}

	var msg string
	h.DB.QueryRow(`SELECT message FROM notifications WHERE id=?`, id).Scan(&msg)
	if msg != "dismissed msg" {
		t.Errorf("dismissed notification must not change, got %q", msg)
	}
}

func TestUpdateNotification_Validation(t *testing.T) {
	h := newTestHandler(t)
	if code := patchNotification(t, h, "1", `{"message":""}`); code != 400 {
		t.Errorf("empty message: expected 400, got %d", code)
	}
	if code := patchNotification(t, h, "1", `{"message":"ok","priority":"HIGH"}`); code != 400 {
		t.Errorf("bad priority: expected 400, got %d", code)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
