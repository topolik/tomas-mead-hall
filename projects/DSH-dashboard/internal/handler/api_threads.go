package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// Threads: durable M:N discussions attachable to notifications/plans/projects.
// Authorship always comes from the authenticated identity (OAuth client name on
// the API, session username in the UI) — never from the payload.

const (
	maxThreadSubject = 200
	maxThreadBody    = 10000
)

var validRefTypes = map[string]bool{"notification": true, "plan": true, "project": true}
var validThreadStatuses = map[string]bool{"open": true, "resolved": true}

// validateThreadRef checks that an optional polymorphic ref points at an
// existing row. Empty ref_type means "no ref" and is valid.
func validateThreadRef(db *sql.DB, refType, refID string) error {
	if refType == "" {
		if refID != "" {
			return fmt.Errorf("ref_id without ref_type")
		}
		return nil
	}
	if !validRefTypes[refType] {
		return fmt.Errorf("ref_type must be notification, plan, or project")
	}
	if refID == "" {
		return fmt.Errorf("ref_id is required with ref_type")
	}
	var count int
	switch refType {
	case "notification":
		db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE id=?`, refID).Scan(&count)
	case "plan":
		db.QueryRow(`SELECT COUNT(*) FROM plans WHERE id=?`, refID).Scan(&count)
	case "project":
		db.QueryRow(`SELECT COUNT(*) FROM projects WHERE code=?`, refID).Scan(&count)
	}
	if count == 0 {
		return fmt.Errorf("%s %q not found", refType, refID)
	}
	return nil
}

// createThread inserts a thread and its first message atomically.
func createThread(db *sql.DB, subject, body, refType, refID, author string) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var refTypeVal, refIDVal any
	if refType != "" {
		refTypeVal, refIDVal = refType, refID
	}
	res, err := tx.Exec(
		`INSERT INTO threads(subject, ref_type, ref_id, created_by) VALUES(?,?,?,?)`,
		subject, refTypeVal, refIDVal, author,
	)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.Exec(
		`INSERT INTO thread_messages(thread_id, author, body) VALUES(?,?,?)`,
		id, author, body,
	); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

// addThreadMessage appends a message and bumps the thread's updated_at.
// Returns sql.ErrNoRows if the thread doesn't exist.
func addThreadMessage(db *sql.DB, threadID string, author, body string) error {
	var exists int
	db.QueryRow(`SELECT COUNT(*) FROM threads WHERE id=?`, threadID).Scan(&exists)
	if exists == 0 {
		return sql.ErrNoRows
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(
		`INSERT INTO thread_messages(thread_id, author, body) VALUES(?,?,?)`,
		threadID, author, body,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE threads SET updated_at=datetime('now') WHERE id=?`, threadID,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateThread handles POST /api/v1/threads.
func (h *APIHandler) CreateThread(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
		RefType string `json:"ref_type"`
		RefID   string `json:"ref_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Subject == "" || body.Body == "" {
		jsonError(w, http.StatusBadRequest, "subject and body are required")
		return
	}
	if len(body.Subject) > maxThreadSubject {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("subject too long (max %d)", maxThreadSubject))
		return
	}
	if len(body.Body) > maxThreadBody {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("body too long (max %d)", maxThreadBody))
		return
	}
	if err := validateThreadRef(h.DB, body.RefType, body.RefID); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}

	author := agentFromRequest(r)
	if author == "" {
		author = "agent"
	}
	id, err := createThread(h.DB, body.Subject, body.Body, body.RefType, body.RefID, author)
	if err != nil {
		log.Printf("create thread: %v", err)
		jsonError(w, 500, "internal error")
		return
	}

	go SendWebPush(h.DB, h.VAPIDPublicKey, h.VAPIDPrivateKey, h.VAPIDContact,
		"[DSH] thread: "+body.Subject, body.Body, "/threads/"+fmt.Sprint(id))

	jsonOK(w, map[string]any{"ok": true, "id": id})
}

// ListThreads handles GET /api/v1/threads.
// The GML processed-tracking contract: ?ref_type=notification&ref_id=N&status=resolved
// returns non-empty iff insight N has a resolved thread.
func (h *APIHandler) ListThreads(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := `SELECT t.id, t.subject, COALESCE(t.ref_type,''), COALESCE(t.ref_id,''), t.status,
	                 t.created_by, t.created_at, t.updated_at,
	                 (SELECT COUNT(*) FROM thread_messages m WHERE m.thread_id=t.id)
	          FROM threads t WHERE 1=1`
	var args []any

	if s := q.Get("status"); s != "" {
		if !validThreadStatuses[s] {
			jsonError(w, http.StatusBadRequest, "status must be open or resolved")
			return
		}
		query += ` AND t.status=?`
		args = append(args, s)
	}
	if rt := q.Get("ref_type"); rt != "" {
		query += ` AND t.ref_type=?`
		args = append(args, rt)
	}
	if ri := q.Get("ref_id"); ri != "" {
		query += ` AND t.ref_id=?`
		args = append(args, ri)
	}
	query += ` ORDER BY t.updated_at DESC`

	limit := 100
	if l := q.Get("limit"); l != "" {
		var n int
		if _, err := fmt.Sscan(l, &n); err == nil && n > 0 {
			if n > 500 {
				n = 500
			}
			limit = n
		}
	}
	query += fmt.Sprintf(` LIMIT %d`, limit)

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		log.Printf("list threads: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	defer rows.Close()

	type thread struct {
		ID           int64  `json:"id"`
		Subject      string `json:"subject"`
		RefType      string `json:"ref_type"`
		RefID        string `json:"ref_id"`
		Status       string `json:"status"`
		CreatedBy    string `json:"created_by"`
		CreatedAt    string `json:"created_at"`
		UpdatedAt    string `json:"updated_at"`
		MessageCount int    `json:"message_count"`
	}
	var threads []thread
	for rows.Next() {
		var t thread
		if err := rows.Scan(&t.ID, &t.Subject, &t.RefType, &t.RefID, &t.Status,
			&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.MessageCount); err != nil {
			continue
		}
		threads = append(threads, t)
	}
	if threads == nil {
		threads = []thread{}
	}
	jsonOK(w, threads)
}

// GetThread handles GET /api/v1/threads/{id} — thread plus messages in order.
func (h *APIHandler) GetThread(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	type message struct {
		ID        int64  `json:"id"`
		Author    string `json:"author"`
		Body      string `json:"body"`
		CreatedAt string `json:"created_at"`
	}
	var t struct {
		ID        int64     `json:"id"`
		Subject   string    `json:"subject"`
		RefType   string    `json:"ref_type"`
		RefID     string    `json:"ref_id"`
		Status    string    `json:"status"`
		CreatedBy string    `json:"created_by"`
		CreatedAt string    `json:"created_at"`
		UpdatedAt string    `json:"updated_at"`
		Messages  []message `json:"messages"`
	}
	err := h.DB.QueryRow(
		`SELECT id, subject, COALESCE(ref_type,''), COALESCE(ref_id,''), status, created_by, created_at, updated_at
		 FROM threads WHERE id=?`, id,
	).Scan(&t.ID, &t.Subject, &t.RefType, &t.RefID, &t.Status, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		jsonError(w, http.StatusNotFound, "thread not found")
		return
	}

	rows, err := h.DB.Query(
		`SELECT id, author, body, created_at FROM thread_messages WHERE thread_id=? ORDER BY id`, id,
	)
	if err != nil {
		log.Printf("get thread messages: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var m message
		if rows.Scan(&m.ID, &m.Author, &m.Body, &m.CreatedAt) == nil {
			t.Messages = append(t.Messages, m)
		}
	}
	if t.Messages == nil {
		t.Messages = []message{}
	}
	jsonOK(w, t)
}

// PostThreadMessage handles POST /api/v1/threads/{id}/messages.
func (h *APIHandler) PostThreadMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Body == "" {
		jsonError(w, http.StatusBadRequest, "body is required")
		return
	}
	if len(body.Body) > maxThreadBody {
		jsonError(w, http.StatusBadRequest, fmt.Sprintf("body too long (max %d)", maxThreadBody))
		return
	}

	author := agentFromRequest(r)
	if author == "" {
		author = "agent"
	}
	if err := addThreadMessage(h.DB, id, author, body.Body); err != nil {
		if err == sql.ErrNoRows {
			jsonError(w, http.StatusNotFound, "thread not found")
			return
		}
		log.Printf("post thread message: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

// UpdateThread handles PATCH /api/v1/threads/{id} — status open|resolved.
func (h *APIHandler) UpdateThread(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if !validThreadStatuses[body.Status] {
		jsonError(w, http.StatusBadRequest, "status must be open or resolved")
		return
	}
	res, err := h.DB.Exec(
		`UPDATE threads SET status=?, updated_at=datetime('now') WHERE id=?`,
		body.Status, id,
	)
	if err != nil {
		log.Printf("update thread: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, http.StatusNotFound, "thread not found")
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}
