package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"dsh/internal/todoreader"
)

type APIHandler struct {
	DB              *sql.DB
	Version         string
	TodoPath        string
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDContact    string
}

func (h *APIHandler) Health(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]string{"status": "ok", "version": h.Version})
}

// UpsertProject creates or updates a project record.
func (h *APIHandler) UpsertProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code         string `json:"code"`
		Name         string `json:"name"`
		Status       string `json:"status"`
		Priority     string `json:"priority"`
		Lead         string `json:"lead"`
		CurrentPhase string `json:"phase"`
		LastUpdated  string `json:"last_updated"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Code == "" || body.Name == "" {
		jsonError(w, http.StatusBadRequest, "code and name are required")
		return
	}
	if body.LastUpdated == "" {
		body.LastUpdated = "date('now')"
	}

	_, err := h.DB.Exec(`
		INSERT INTO projects(code, name, status, priority, lead, current_phase, last_updated)
		VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(code) DO UPDATE SET
		  name=excluded.name,
		  status=excluded.status,
		  priority=excluded.priority,
		  lead=excluded.lead,
		  current_phase=excluded.current_phase,
		  last_updated=excluded.last_updated`,
		body.Code, body.Name,
		coalesce(body.Status, "Ideation"),
		coalesce(body.Priority, "Q2"),
		body.Lead, body.CurrentPhase, body.LastUpdated,
	)
	if err != nil {
		log.Printf("upsert project: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

var validPriorities = map[string]bool{
	"Q1": true, "Q2": true, "Q3": true, "Q4": true,
}

// CreateNotification adds a notification.
func (h *APIHandler) CreateNotification(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProjectCode string `json:"project_code"`
		Message     string `json:"message"`
		Type        string `json:"type"`
		Priority    string `json:"priority"`
		Link        string `json:"link"`
		Comment     string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Message == "" {
		jsonError(w, http.StatusBadRequest, "message is required")
		return
	}
	if body.Priority != "" && !validPriorities[body.Priority] {
		jsonError(w, http.StatusBadRequest, "priority must be Q1, Q2, Q3, or Q4")
		return
	}
	if len(body.Comment) > 2000 {
		jsonError(w, http.StatusBadRequest, "comment too long (max 2000 characters)")
		return
	}
	notifType := body.Type
	if notifType == "" {
		notifType = "info"
	}

	var projectCode interface{}
	if body.ProjectCode != "" {
		projectCode = body.ProjectCode
	}
	var priority interface{}
	if body.Priority != "" {
		priority = body.Priority
	}

	res, err := h.DB.Exec(
		`INSERT INTO notifications(project_code, message, type, priority, link, comment) VALUES(?,?,?,?,?,?)`,
		projectCode, body.Message, notifType, priority, body.Link, body.Comment,
	)
	if err != nil {
		log.Printf("create notification: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	id, _ := res.LastInsertId()

	title := "[DSH]"
	if body.ProjectCode != "" {
		title = "[" + body.ProjectCode + "]"
	}
	title += " " + notifType
	url := "/notifications"
	if body.Link != "" {
		url = body.Link
	}
	go SendWebPush(h.DB, h.VAPIDPublicKey, h.VAPIDPrivateKey, h.VAPIDContact, title, body.Message, url)

	jsonOK(w, map[string]any{"ok": true, "id": id})
}

func (h *APIHandler) ListNotifications(w http.ResponseWriter, r *http.Request) {
	includeDismissed := r.URL.Query().Get("include_dismissed") == "true"
	hasComment := r.URL.Query().Get("has_comment") == "true"

	selectCols := `id, COALESCE(project_code,''), message, type, COALESCE(priority,''), COALESCE(link,''), comment, created_at`
	if includeDismissed {
		selectCols += `, dismissed_at`
	}
	query := `SELECT ` + selectCols + ` FROM notifications WHERE 1=1`
	var args []any

	if !includeDismissed {
		query += ` AND dismissed_at IS NULL`
	}
	if hasComment {
		query += ` AND comment != ''`
	}

	validTypes := map[string]bool{"action_needed": true, "info": true}
	parseFilter(r.URL.Query().Get("project_code"), nil).applySQL("project_code", &query, &args)
	parseFilter(r.URL.Query().Get("priority"), validPriorities).applySQL("priority", &query, &args)
	parseFilter(r.URL.Query().Get("type"), validTypes).applySQL("type", &query, &args)
	parseFilter(r.URL.Query().Get("message"), nil).applyLikeSQL("message", &query, &args)
	query += ` ORDER BY created_at DESC`

	limit := 20
	const maxLimit = 200
	if l := r.URL.Query().Get("limit"); l != "" {
		var n int
		if _, err := fmt.Sscan(l, &n); err == nil && n > 0 {
			// Clamp to the max rather than silently falling back to the default
			// (20) when out of range — callers that ask for 200 were getting 20.
			if n > maxLimit {
				n = maxLimit
			}
			limit = n
		}
	}
	query += fmt.Sprintf(` LIMIT %d`, limit)

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		log.Printf("list notifications: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	defer rows.Close()

	type notif struct {
		ID          int64   `json:"id"`
		ProjectCode string  `json:"project_code"`
		Message     string  `json:"message"`
		Type        string  `json:"type"`
		Priority    string  `json:"priority"`
		Link        string  `json:"link"`
		Comment     string  `json:"comment"`
		CreatedAt   string  `json:"created_at"`
		DismissedAt *string `json:"dismissed_at,omitempty"`
	}
	var notifs []notif
	for rows.Next() {
		var n notif
		if includeDismissed {
			var dismissedAt sql.NullString
			rows.Scan(&n.ID, &n.ProjectCode, &n.Message, &n.Type, &n.Priority, &n.Link, &n.Comment, &n.CreatedAt, &dismissedAt)
			if dismissedAt.Valid {
				n.DismissedAt = &dismissedAt.String
			}
		} else {
			rows.Scan(&n.ID, &n.ProjectCode, &n.Message, &n.Type, &n.Priority, &n.Link, &n.Comment, &n.CreatedAt)
		}
		notifs = append(notifs, n)
	}
	if notifs == nil {
		notifs = []notif{}
	}
	jsonOK(w, notifs)
}

func coalesce(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}

// ListProjects returns all projects as JSON.
func (h *APIHandler) ListProjects(w http.ResponseWriter, _ *http.Request) {
	rows, err := h.DB.Query(
		`SELECT code, name, status, priority, lead, current_phase, last_updated FROM projects ORDER BY priority, code LIMIT 500`,
	)
	if err != nil {
		jsonError(w, 500, "internal error")
		return
	}
	defer rows.Close()

	type proj struct {
		Code         string `json:"code"`
		Name         string `json:"name"`
		Status       string `json:"status"`
		Priority     string `json:"priority"`
		Lead         string `json:"lead"`
		CurrentPhase string `json:"phase"`
		LastUpdated  string `json:"last_updated"`
	}
	var projects []proj
	for rows.Next() {
		var p proj
		rows.Scan(&p.Code, &p.Name, &p.Status, &p.Priority, &p.Lead, &p.CurrentPhase, &p.LastUpdated)
		projects = append(projects, p)
	}
	if projects == nil {
		projects = []proj{}
	}
	jsonOK(w, projects)
}

// CreateTodo adds a todo item via the file-based todo.txt system.
func (h *APIHandler) CreateTodo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text        string `json:"text"`
		Priority    string `json:"priority"`
		ProjectCode string `json:"project_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Text == "" {
		jsonError(w, http.StatusBadRequest, "text is required")
		return
	}
	priority := body.Priority
	if priority == "" {
		priority = "Q2"
	}
	if !validPriorities[priority] {
		jsonError(w, http.StatusBadRequest, "priority must be Q1, Q2, Q3, or Q4")
		return
	}
	text := body.Text
	if body.ProjectCode != "" {
		text = "[" + body.ProjectCode + "] " + text
	}
	added, err := todoreader.Add(h.TodoPath, text, priority)
	if err != nil {
		log.Printf("create todo: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	if !added {
		log.Printf("create todo: skipped duplicate %q", text)
	}
	jsonOK(w, map[string]bool{"ok": true, "added": added})
}

// ListTodos returns all todo items as JSON.
func (h *APIHandler) ListTodos(w http.ResponseWriter, r *http.Request) {
	items, err := todoreader.Load(h.TodoPath)
	if err != nil {
		log.Printf("list todos: %v", err)
		jsonError(w, 500, "internal error")
		return
	}

	statusFilter := r.URL.Query().Get("status")
	priorityFilter := r.URL.Query().Get("priority")

	type todo struct {
		ID        int64  `json:"id"`
		Text      string `json:"text"`
		Priority  string `json:"priority"`
		Status    string `json:"status"`
		AddedDate string `json:"added_date"`
	}
	var todos []todo
	for _, item := range items {
		if statusFilter != "" && item.Status != statusFilter {
			continue
		}
		if priorityFilter != "" && item.Priority != priorityFilter {
			continue
		}
		todos = append(todos, todo{
			ID:        item.ID,
			Text:      item.Text,
			Priority:  item.Priority,
			Status:    item.Status,
			AddedDate: item.AddedDate,
		})
	}
	if todos == nil {
		todos = []todo{}
	}
	jsonOK(w, todos)
}

// --- Plans ---

func (h *APIHandler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProjectCode string `json:"project_code"`
		Title       string `json:"title"`
		Detail      string `json:"detail"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Title == "" || body.Detail == "" {
		jsonError(w, http.StatusBadRequest, "title and detail are required")
		return
	}

	var projectCode interface{}
	if body.ProjectCode != "" {
		projectCode = body.ProjectCode
	}

	res, err := h.DB.Exec(
		`INSERT INTO plans(project_code, title, detail) VALUES(?,?,?)`,
		projectCode, body.Title, body.Detail,
	)
	if err != nil {
		log.Printf("create plan: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	id, _ := res.LastInsertId()

	title := "[DSH]"
	if body.ProjectCode != "" {
		title = "[" + body.ProjectCode + "]"
	}
	title += " plan"
	go SendWebPush(h.DB, h.VAPIDPublicKey, h.VAPIDPrivateKey, h.VAPIDContact, title, body.Title, "/plans")

	jsonOK(w, map[string]any{"ok": true, "id": id})
}

func (h *APIHandler) ListPlans(w http.ResponseWriter, r *http.Request) {
	query := `SELECT id, COALESCE(project_code,''), title, detail, status, comment, created_at, decided_at FROM plans WHERE 1=1`
	var args []any

	if s := r.URL.Query().Get("status"); s != "" {
		query += ` AND status=?`
		args = append(args, s)
	}
	if p := r.URL.Query().Get("project_code"); p != "" {
		query += ` AND project_code=?`
		args = append(args, p)
	}
	query += ` ORDER BY created_at DESC`

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		var n int
		if _, err := fmt.Sscan(l, &n); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	query += fmt.Sprintf(` LIMIT %d`, limit)

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		log.Printf("list plans: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	defer rows.Close()

	type plan struct {
		ID          int64   `json:"id"`
		ProjectCode string  `json:"project_code"`
		Title       string  `json:"title"`
		Detail      string  `json:"detail"`
		Status      string  `json:"status"`
		Comment     string  `json:"comment"`
		CreatedAt   string  `json:"created_at"`
		DecidedAt   *string `json:"decided_at,omitempty"`
	}
	var plans []plan
	for rows.Next() {
		var p plan
		var decidedAt sql.NullString
		if err := rows.Scan(&p.ID, &p.ProjectCode, &p.Title, &p.Detail, &p.Status, &p.Comment, &p.CreatedAt, &decidedAt); err != nil {
			continue
		}
		if decidedAt.Valid {
			p.DecidedAt = &decidedAt.String
		}
		plans = append(plans, p)
	}
	if plans == nil {
		plans = []plan{}
	}
	jsonOK(w, plans)
}

func (h *APIHandler) UpdatePlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Status  string `json:"status"`
		Comment string `json:"comment"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	validStatuses := map[string]bool{"approved": true, "rejected": true, "pending": true}
	if body.Status != "" && !validStatuses[body.Status] {
		jsonError(w, http.StatusBadRequest, "status must be pending, approved, or rejected")
		return
	}

	if body.Status != "" && body.Status != "pending" {
		_, err := h.DB.Exec(
			`UPDATE plans SET status=?, comment=?, decided_at=datetime('now') WHERE id=?`,
			body.Status, body.Comment, id,
		)
		if err != nil {
			log.Printf("update plan: %v", err)
			jsonError(w, 500, "internal error")
			return
		}
	} else if body.Status == "pending" {
		_, err := h.DB.Exec(
			`UPDATE plans SET status='pending', comment=?, decided_at=NULL WHERE id=?`,
			body.Comment, id,
		)
		if err != nil {
			log.Printf("update plan: %v", err)
			jsonError(w, 500, "internal error")
			return
		}
	} else {
		_, err := h.DB.Exec(`UPDATE plans SET comment=? WHERE id=?`, body.Comment, id)
		if err != nil {
			log.Printf("update plan comment: %v", err)
			jsonError(w, 500, "internal error")
			return
		}
	}

	jsonOK(w, map[string]bool{"ok": true})
}

func (h *APIHandler) DeletePlan(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	res, err := h.DB.Exec(`DELETE FROM plans WHERE id=?`, id)
	if err != nil {
		log.Printf("delete plan: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, http.StatusNotFound, "plan not found")
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

// UpdateNotification updates an ACTIVE notification's message/link/priority in
// place. The `dismissed_at IS NULL` guard guarantees an update can never
// resurrect a dismissed notification — the GML insight pipeline relies on this
// to refresh a live insight when learn re-derives it with new detail, while a
// dismissed insight stays dismissed (a new clarifying insight is posted instead).
func (h *APIHandler) UpdateNotification(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Message  string `json:"message"`
		Link     string `json:"link"`
		Priority string `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Message == "" {
		jsonError(w, http.StatusBadRequest, "message is required")
		return
	}
	if body.Priority != "" && !validPriorities[body.Priority] {
		jsonError(w, http.StatusBadRequest, "priority must be Q1, Q2, Q3, or Q4")
		return
	}
	var priority interface{}
	if body.Priority != "" {
		priority = body.Priority
	}

	res, err := h.DB.Exec(
		`UPDATE notifications SET message=?, link=?, priority=? WHERE id=? AND dismissed_at IS NULL`,
		body.Message, body.Link, priority, id,
	)
	if err != nil {
		log.Printf("update notification: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		jsonError(w, http.StatusNotFound, "active notification not found")
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
}

// GetDB exposes the DB for bootstrap use.
func (h *APIHandler) GetDB() *sql.DB { return h.DB }
