package handler

import (
	"database/sql"
	"log"
	"net/http"
	"strconv"
	"strings"

	"dsh/internal/model"
)

// --- Threads UI ---

func (h *UIHandler) ThreadsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	statusFilter := q.Get("status")
	if statusFilter == "" {
		statusFilter = "open"
	}

	var counts struct{ Open, Resolved int }
	h.DB.QueryRow(`SELECT COUNT(*) FROM threads WHERE status='open'`).Scan(&counts.Open)
	h.DB.QueryRow(`SELECT COUNT(*) FROM threads WHERE status='resolved'`).Scan(&counts.Resolved)

	query := `SELECT t.id, t.subject, COALESCE(t.ref_type,''), COALESCE(t.ref_id,''), t.status,
	                 t.created_by, t.created_at, t.updated_at,
	                 (SELECT COUNT(*) FROM thread_messages m WHERE m.thread_id=t.id)
	          FROM threads t`
	var args []any
	if statusFilter != "all" {
		query += ` WHERE t.status=?`
		args = append(args, statusFilter)
	}
	query += ` ORDER BY t.updated_at DESC LIMIT 200`

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		log.Printf("threads query: %v", err)
		http.Error(w, "internal error", 500)
		return
	}
	defer rows.Close()

	var threads []model.Thread
	for rows.Next() {
		var t model.Thread
		if err := rows.Scan(&t.ID, &t.Subject, &t.RefType, &t.RefID, &t.Status,
			&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.MessageCount); err != nil {
			continue
		}
		threads = append(threads, t)
	}

	notifBadge, planBadge, threadBadge := navBadges(h.DB)
	h.Tmpls.ExecuteTemplate(w, "threads.html", map[string]any{
		"Threads":      threads,
		"StatusFilter": statusFilter,
		"Counts":       counts,
		// New-thread form prefill (e.g. [discuss] from a notification row).
		"NewOpen":     q.Get("new") == "1",
		"PrefRefType": q.Get("ref_type"),
		"PrefRefID":   q.Get("ref_id"),
		"PrefSubject": q.Get("subject"),
		"CSRFToken":   h.csrfToken(r),
		"Username":    h.username(r),
		"NotifBadge":  notifBadge,
		"PlanBadge":   planBadge,
		"ThreadBadge": threadBadge,
	})
}

func (h *UIHandler) ThreadDetailPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var t model.Thread
	err := h.DB.QueryRow(
		`SELECT id, subject, COALESCE(ref_type,''), COALESCE(ref_id,''), status, created_by, created_at, updated_at
		 FROM threads WHERE id=?`, id,
	).Scan(&t.ID, &t.Subject, &t.RefType, &t.RefID, &t.Status, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	rows, err := h.DB.Query(
		`SELECT id, thread_id, author, body, created_at FROM thread_messages WHERE thread_id=? ORDER BY id`, id,
	)
	if err != nil {
		log.Printf("thread messages query: %v", err)
		http.Error(w, "internal error", 500)
		return
	}
	defer rows.Close()

	var msgs []model.ThreadMessage
	for rows.Next() {
		var m model.ThreadMessage
		if rows.Scan(&m.ID, &m.ThreadID, &m.Author, &m.Body, &m.CreatedAt) == nil {
			msgs = append(msgs, m)
		}
	}

	notifBadge, planBadge, threadBadge := navBadges(h.DB)
	h.Tmpls.ExecuteTemplate(w, "thread_detail.html", map[string]any{
		"Thread":      t,
		"Messages":    msgs,
		"CSRFToken":   h.csrfToken(r),
		"Username":    h.username(r),
		"NotifBadge":  notifBadge,
		"PlanBadge":   planBadge,
		"ThreadBadge": threadBadge,
	})
}

// ThreadCreateUI handles the new-thread form POST. Author = session username.
func (h *UIHandler) ThreadCreateUI(w http.ResponseWriter, r *http.Request) {
	subject := strings.TrimSpace(r.FormValue("subject"))
	body := strings.TrimSpace(r.FormValue("body"))
	refType := r.FormValue("ref_type")
	refID := strings.TrimSpace(r.FormValue("ref_id"))

	if subject == "" || body == "" || len(subject) > maxThreadSubject || len(body) > maxThreadBody {
		http.Redirect(w, r, "/threads?new=1", http.StatusFound)
		return
	}
	if err := validateThreadRef(h.DB, refType, refID); err != nil {
		log.Printf("thread create ref: %v", err)
		http.Redirect(w, r, "/threads?new=1", http.StatusFound)
		return
	}

	author := h.username(r)
	if author == "" {
		author = "tomas"
	}
	id, err := createThread(h.DB, subject, body, refType, refID, author)
	if err != nil {
		log.Printf("thread create: %v", err)
		http.Error(w, "internal error", 500)
		return
	}
	http.Redirect(w, r, "/threads/"+strconv.FormatInt(id, 10), http.StatusFound)
}

// ThreadReplyUI appends a message from the reply form.
func (h *UIHandler) ThreadReplyUI(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	body := strings.TrimSpace(r.FormValue("body"))
	if body == "" || len(body) > maxThreadBody {
		http.Redirect(w, r, "/threads/"+id, http.StatusFound)
		return
	}
	author := h.username(r)
	if author == "" {
		author = "tomas"
	}
	if err := addThreadMessage(h.DB, id, author, body); err != nil {
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
			return
		}
		log.Printf("thread reply: %v", err)
		http.Error(w, "internal error", 500)
		return
	}
	http.Redirect(w, r, "/threads/"+id, http.StatusFound)
}

// ThreadStatusUI toggles open/resolved from the detail page.
func (h *UIHandler) ThreadStatusUI(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	status := r.FormValue("status")
	if !validThreadStatuses[status] {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}
	h.DB.Exec(`UPDATE threads SET status=?, updated_at=datetime('now') WHERE id=?`, status, id)
	returnTo := r.FormValue("_return")
	if returnTo == "" || !strings.HasPrefix(returnTo, "/threads") {
		returnTo = "/threads/" + id
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

// threadRefsForNotifications returns notificationID -> (threadID, status) for
// the notifications page badge/discuss links.
func threadRefsForNotifications(db *sql.DB) map[string]model.Thread {
	out := map[string]model.Thread{}
	rows, err := db.Query(
		`SELECT id, COALESCE(ref_id,''), status FROM threads WHERE ref_type='notification'`,
	)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var t model.Thread
		var refID string
		if rows.Scan(&t.ID, &refID, &t.Status) == nil && refID != "" {
			out[refID] = t
		}
	}
	return out
}
