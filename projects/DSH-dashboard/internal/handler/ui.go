package handler

import (
	"database/sql"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"dsh/internal/auth"
	"dsh/internal/model"
	"dsh/internal/pmreader"
	"dsh/internal/todoreader"
)

type UIHandler struct {
	DB        *sql.DB
	Tmpls     *template.Template
	PMPath    string
	TodoPath  string
	LLPURL    string // LLM-proxy data API base URL ("" => not configured)
	LLPSocket string // LLP control socket path (handshake)
}

const conflictDetailCond = `(detail LIKE '%merge_conflict:[%' OR detail LIKE '%merge_conflict_unresolved%')`

type conflictGroup struct {
	Resolution model.Plan
	Originals  []model.Plan
	Unresolved bool
}

func (h *UIHandler) csrfToken(r *http.Request) string {
	_, data, err := auth.SessionFromRequest(r, h.DB)
	if err != nil {
		return ""
	}
	return data.CSRFToken
}

func (h *UIHandler) username(r *http.Request) string {
	sess, _, err := auth.SessionFromRequest(r, h.DB)
	if err != nil {
		return ""
	}
	var uname string
	h.DB.QueryRow(`SELECT username FROM users WHERE id=?`, sess.UserID).Scan(&uname)
	return uname
}

// --- Todo ---

var todoStatuses = []string{"open", "in_progress", "done", "parked"}

// priorityRank / statusRank order items for the sortable flat list.
var priorityRank = map[string]int{"Q1": 0, "Q2": 1, "Q3": 2, "Q4": 3}
var statusRank = map[string]int{"open": 0, "in_progress": 1, "parked": 2, "done": 3}

func (h *UIHandler) TodoPage(w http.ResponseWriter, r *http.Request) {
	items, err := todoreader.Load(h.TodoPath)
	if err != nil {
		log.Printf("todo load: %v", err)
		http.Error(w, "internal error", 500)
		return
	}

	q := r.URL.Query()
	validStatus := map[string]bool{"open": true, "in_progress": true, "done": true, "parked": true}
	validPriority := map[string]bool{"Q1": true, "Q2": true, "Q3": true, "Q4": true}

	fStatus := parseFilter(q.Get("status"), validStatus)
	fPriority := parseFilter(q.Get("priority"), validPriority)
	fText := parseFilter(q.Get("text"), nil)
	sortBy := q.Get("sort")
	filterOpen := q.Get("fo") == "1"

	// Status counts across all items (drives the filter badges) — computed
	// before filtering so the badges always show the full picture.
	statusCounts := map[string]int{}
	for _, it := range items {
		statusCounts[it.Status]++
	}

	var filtered []model.BacklogItem
	for _, it := range items {
		if !fStatus.Accept(it.Status) {
			continue
		}
		if !fPriority.Accept(it.Priority) {
			continue
		}
		if !fText.AcceptLike(it.Text) {
			continue
		}
		filtered = append(filtered, it)
	}

	sortTodos(filtered, sortBy)
	if sortBy == "" {
		sortBy = "priority"
	}

	filterQuery := buildTodoFilterQuery(fStatus, fPriority, fText, sortBy)

	notifBadge, planBadge, threadBadge := navBadges(h.DB)
	h.Tmpls.ExecuteTemplate(w, "todo.html", map[string]any{
		"Items":          filtered,
		"Total":          len(items),
		"Shown":          len(filtered),
		"StatusCounts":   statusCounts,
		"Statuses":       todoStatuses,
		"FilterStatus":   fStatus,
		"FilterPriority": fPriority,
		"FilterText":     fText,
		"SortBy":         sortBy,
		"FilterOpen":     filterOpen,
		"FilterQuery":    filterQuery,
		"CSRFToken":      h.csrfToken(r),
		"Username":       h.username(r),
		"NotifBadge":     notifBadge,
		"PlanBadge":      planBadge,
		"ThreadBadge":    threadBadge,
	})
}

func sortTodos(items []model.BacklogItem, sortBy string) {
	switch sortBy {
	case "date":
		// Newest added first; blank dates sort last.
		sort.SliceStable(items, func(i, j int) bool {
			a, b := items[i].AddedDate, items[j].AddedDate
			if a == b {
				return items[i].ID < items[j].ID
			}
			if a == "" {
				return false
			}
			if b == "" {
				return true
			}
			return a > b
		})
	case "status":
		sort.SliceStable(items, func(i, j int) bool {
			if statusRank[items[i].Status] != statusRank[items[j].Status] {
				return statusRank[items[i].Status] < statusRank[items[j].Status]
			}
			return priorityRank[items[i].Priority] < priorityRank[items[j].Priority]
		})
	default: // priority
		sort.SliceStable(items, func(i, j int) bool {
			if priorityRank[items[i].Priority] != priorityRank[items[j].Priority] {
				return priorityRank[items[i].Priority] < priorityRank[items[j].Priority]
			}
			return statusRank[items[i].Status] < statusRank[items[j].Status]
		})
	}
}

func buildTodoFilterQuery(fStatus, fPriority, fText filterSpec, sortBy string) string {
	var parts []string
	if r := fStatus.Raw(); r != "" {
		parts = append(parts, "status="+r)
	}
	if r := fPriority.Raw(); r != "" {
		parts = append(parts, "priority="+r)
	}
	if r := fText.Raw(); r != "" {
		parts = append(parts, "text="+url.QueryEscape(r))
	}
	if sortBy != "" && sortBy != "priority" {
		parts = append(parts, "sort="+sortBy)
	}
	if len(parts) == 0 {
		return ""
	}
	return "?" + strings.Join(parts, "&")
}

// TodoBulk applies one action to all selected items in a single pass.
func (h *UIHandler) TodoBulk(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	action := r.FormValue("action")
	var idxs []int
	for _, raw := range r.Form["ids"] {
		if n, err := strconv.Atoi(raw); err == nil {
			idxs = append(idxs, n)
		}
	}
	if len(idxs) > 0 {
		switch action {
		case "delete":
			if err := todoreader.BulkDelete(h.TodoPath, idxs); err != nil {
				log.Printf("todo bulk delete: %v", err)
			}
		case "done", "in_progress", "parked", "open":
			if err := todoreader.BulkSetStatus(h.TodoPath, idxs, action); err != nil {
				log.Printf("todo bulk status: %v", err)
			}
		}
	}
	redirectTodo(w, r)
}

// redirectTodo sends the user back to the filtered list they came from,
// falling back to /todo. Only same-page returns are honored.
func redirectTodo(w http.ResponseWriter, r *http.Request) {
	returnTo := r.FormValue("_return")
	if returnTo == "" || !strings.HasPrefix(returnTo, "/todo") {
		returnTo = "/todo"
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (h *UIHandler) TodoAdd(w http.ResponseWriter, r *http.Request) {
	text := r.FormValue("text")
	priority := r.FormValue("priority")
	if text == "" || priority == "" {
		redirectTodo(w, r)
		return
	}
	if _, err := todoreader.Add(h.TodoPath, text, priority); err != nil {
		log.Printf("todo add: %v", err)
	}
	redirectTodo(w, r)
}

func (h *UIHandler) TodoEditPage(w http.ResponseWriter, r *http.Request) {
	lineIdx, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	items, err := todoreader.Load(h.TodoPath)
	if err != nil {
		http.Error(w, "internal error", 500)
		return
	}
	var found *model.BacklogItem
	for i := range items {
		if items[i].ID == int64(lineIdx) {
			found = &items[i]
			break
		}
	}
	if found == nil {
		http.NotFound(w, r)
		return
	}
	notifBadge, planBadge, threadBadge := navBadges(h.DB)
	h.Tmpls.ExecuteTemplate(w, "todo_edit.html", map[string]any{
		"Item":        found,
		"CSRFToken":   h.csrfToken(r),
		"Username":    h.username(r),
		"NotifBadge":  notifBadge,
		"PlanBadge":   planBadge,
		"ThreadBadge": threadBadge,
	})
}

func (h *UIHandler) TodoEditSubmit(w http.ResponseWriter, r *http.Request) {
	lineIdx, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.Redirect(w, r, "/todo", http.StatusFound)
		return
	}
	text := r.FormValue("text")
	priority := r.FormValue("priority")
	addedDate := r.FormValue("added_date")
	if text == "" || priority == "" {
		http.Redirect(w, r, "/todo", http.StatusFound)
		return
	}
	if err := todoreader.UpdateItem(h.TodoPath, lineIdx, text, priority, addedDate); err != nil {
		log.Printf("todo edit: %v", err)
	}
	http.Redirect(w, r, "/todo", http.StatusFound)
}

func (h *UIHandler) TodoDelete(w http.ResponseWriter, r *http.Request) {
	lineIdx, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		redirectTodo(w, r)
		return
	}
	if err := todoreader.DeleteItem(h.TodoPath, lineIdx); err != nil {
		log.Printf("todo delete: %v", err)
	}
	redirectTodo(w, r)
}

func (h *UIHandler) TodoUpdate(w http.ResponseWriter, r *http.Request) {
	lineStr := r.PathValue("id")
	lineIdx, err := strconv.Atoi(lineStr)
	if err != nil {
		redirectTodo(w, r)
		return
	}
	field := r.FormValue("field")
	value := r.FormValue("value")

	switch field {
	case "status":
		if err := todoreader.UpdateStatus(h.TodoPath, lineIdx, value); err != nil {
			log.Printf("todo update status: %v", err)
		}
	case "priority":
		if err := todoreader.UpdatePriority(h.TodoPath, lineIdx, value); err != nil {
			log.Printf("todo update priority: %v", err)
		}
	}
	redirectTodo(w, r)
}

// --- Projects ---

func (h *UIHandler) ProjectsPage(w http.ResponseWriter, r *http.Request) {
	var projects []model.Project
	if h.PMPath != "" {
		var err error
		projects, err = pmreader.ScanProjects(h.PMPath)
		if err != nil {
			log.Printf("projects scan: %v", err)
		}
	}

	notifBadge, planBadge, threadBadge := navBadges(h.DB)
	h.Tmpls.ExecuteTemplate(w, "projects.html", map[string]any{
		"Projects":    projects,
		"Username":    h.username(r),
		"NotifBadge":  notifBadge,
		"PlanBadge":   planBadge,
		"ThreadBadge": threadBadge,
	})
}

func (h *UIHandler) ProjectDetailPage(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if h.PMPath == "" {
		http.NotFound(w, r)
		return
	}
	detail, err := pmreader.ProjectDetail(h.PMPath, code)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	notifBadge, planBadge, threadBadge := navBadges(h.DB)
	h.Tmpls.ExecuteTemplate(w, "project_detail.html", map[string]any{
		"Detail":      detail,
		"Username":    h.username(r),
		"NotifBadge":  notifBadge,
		"PlanBadge":   planBadge,
		"ThreadBadge": threadBadge,
	})
}

// --- Notifications ---

type filterSpec struct {
	Include []string
	Exclude []string
}

func parseFilter(raw string, allowed map[string]bool) filterSpec {
	var f filterSpec
	if raw == "" {
		return f
	}
	for _, v := range strings.Split(raw, ",") {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		neg := strings.HasPrefix(v, "!")
		if neg {
			v = v[1:]
		}
		if allowed != nil && !allowed[v] {
			continue
		}
		if neg {
			f.Exclude = append(f.Exclude, v)
		} else {
			f.Include = append(f.Include, v)
		}
	}
	return f
}

func (f filterSpec) applySQL(col string, query *string, args *[]any) {
	if len(f.Include) > 0 {
		placeholders := strings.Repeat(",?", len(f.Include))[1:]
		*query += ` AND ` + col + ` IN (` + placeholders + `)`
		for _, v := range f.Include {
			*args = append(*args, v)
		}
	}
	if len(f.Exclude) > 0 {
		placeholders := strings.Repeat(",?", len(f.Exclude))[1:]
		*query += ` AND (` + col + ` NOT IN (` + placeholders + `) OR ` + col + ` IS NULL)`
		for _, v := range f.Exclude {
			*args = append(*args, v)
		}
	}
}

func (f filterSpec) applyLikeSQL(col string, query *string, args *[]any) {
	if len(f.Include) > 0 {
		*query += ` AND (`
		for i, v := range f.Include {
			if i > 0 {
				*query += ` OR `
			}
			*query += col + ` LIKE '%' || ? || '%'`
			*args = append(*args, v)
		}
		*query += `)`
	}
	for _, v := range f.Exclude {
		*query += ` AND ` + col + ` NOT LIKE '%' || ? || '%'`
		*args = append(*args, v)
	}
}

func (f filterSpec) Raw() string {
	var parts []string
	for _, v := range f.Include {
		parts = append(parts, v)
	}
	for _, v := range f.Exclude {
		parts = append(parts, "!"+v)
	}
	return strings.Join(parts, ",")
}

func (f filterSpec) Empty() bool {
	return len(f.Include) == 0 && len(f.Exclude) == 0
}

func (f filterSpec) Has(v string) bool {
	for _, s := range f.Include {
		if s == v {
			return true
		}
	}
	for _, s := range f.Exclude {
		if s == v {
			return true
		}
	}
	return false
}

func (f filterSpec) IsNegated() bool {
	return len(f.Exclude) > 0
}

func (f filterSpec) HasExclude(v string) bool {
	for _, s := range f.Exclude {
		if s == v {
			return true
		}
	}
	return false
}

// Accept reports whether val passes this filter using exact-match
// include/exclude semantics — the in-memory mirror of applySQL, for data
// loaded from a file rather than queried from SQL.
func (f filterSpec) Accept(val string) bool {
	if len(f.Include) > 0 {
		found := false
		for _, v := range f.Include {
			if v == val {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, v := range f.Exclude {
		if v == val {
			return false
		}
	}
	return true
}

// AcceptLike reports whether val passes using case-insensitive substring
// include (OR) / exclude semantics — the in-memory mirror of applyLikeSQL.
func (f filterSpec) AcceptLike(val string) bool {
	lv := strings.ToLower(val)
	if len(f.Include) > 0 {
		any := false
		for _, v := range f.Include {
			if strings.Contains(lv, strings.ToLower(v)) {
				any = true
				break
			}
		}
		if !any {
			return false
		}
	}
	for _, v := range f.Exclude {
		if strings.Contains(lv, strings.ToLower(v)) {
			return false
		}
	}
	return true
}

func (h *UIHandler) NotificationsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	validTypes := map[string]bool{"action_needed": true, "info": true}
	validPriorities := map[string]bool{"Q1": true, "Q2": true, "Q3": true, "Q4": true}

	fType := parseFilter(q.Get("type"), validTypes)
	fProject := parseFilter(q.Get("project"), nil)
	fPriority := parseFilter(q.Get("priority"), validPriorities)
	fMessage := parseFilter(q.Get("message"), nil)
	sortBy := q.Get("sort")
	filterOpen := q.Get("fo") == "1"
	showDismissed := q.Get("show") == "dismissed"

	var query string
	if showDismissed {
		query = `SELECT id, COALESCE(project_code,''), message, type, COALESCE(priority,''), COALESCE(link,''), comment, created_at FROM notifications WHERE dismissed_at IS NOT NULL`
	} else {
		query = `SELECT id, COALESCE(project_code,''), message, type, COALESCE(priority,''), COALESCE(link,''), comment, created_at FROM notifications WHERE dismissed_at IS NULL`
	}
	var args []any

	fType.applySQL("type", &query, &args)
	fProject.applySQL("project_code", &query, &args)
	fPriority.applySQL("priority", &query, &args)
	fMessage.applyLikeSQL("message", &query, &args)

	switch sortBy {
	case "priority":
		query += ` ORDER BY priority IS NULL, priority, created_at DESC`
	default:
		sortBy = "date"
		query += ` ORDER BY created_at DESC`
	}
	query += ` LIMIT 500`

	rows, err := h.DB.Query(query, args...)
	if err != nil {
		log.Printf("notifications query: %v", err)
		http.Error(w, "internal error", 500)
		return
	}
	defer rows.Close()

	var notifs []model.Notification
	for rows.Next() {
		var n model.Notification
		if err := rows.Scan(&n.ID, &n.ProjectCode, &n.Message, &n.Type, &n.Priority, &n.Link, &n.Comment, &n.CreatedAt); err != nil {
			continue
		}
		notifs = append(notifs, n)
	}

	var projectQuery string
	if showDismissed {
		projectQuery = `SELECT DISTINCT project_code FROM notifications WHERE dismissed_at IS NOT NULL AND project_code IS NOT NULL ORDER BY project_code`
	} else {
		projectQuery = `SELECT DISTINCT project_code FROM notifications WHERE dismissed_at IS NULL AND project_code IS NOT NULL ORDER BY project_code`
	}
	projectRows, _ := h.DB.Query(projectQuery)
	var projects []string
	if projectRows != nil {
		defer projectRows.Close()
		for projectRows.Next() {
			var p string
			projectRows.Scan(&p)
			projects = append(projects, p)
		}
	}

	filterQuery := buildFilterQuery(fType, fProject, fPriority, fMessage, sortBy)

	notifBadge, planBadge, threadBadge := navBadges(h.DB)
	h.Tmpls.ExecuteTemplate(w, "notifications.html", map[string]any{
		"Notifications":  notifs,
		"ThreadRefs":     threadRefsForNotifications(h.DB),
		"Projects":       projects,
		"FilterType":     fType,
		"FilterProject":  fProject,
		"FilterPriority": fPriority,
		"FilterMessage":  fMessage,
		"SortBy":         sortBy,
		"FilterOpen":     filterOpen,
		"FilterQuery":    filterQuery,
		"ShowDismissed":  showDismissed,
		"CSRFToken":      h.csrfToken(r),
		"Username":       h.username(r),
		"NotifBadge":     notifBadge,
		"PlanBadge":      planBadge,
		"ThreadBadge":    threadBadge,
	})
}

func buildFilterQuery(fType, fProject, fPriority, fMessage filterSpec, sortBy string) string {
	var parts []string
	if r := fType.Raw(); r != "" {
		parts = append(parts, "type="+r)
	}
	if r := fProject.Raw(); r != "" {
		parts = append(parts, "project="+r)
	}
	if r := fPriority.Raw(); r != "" {
		parts = append(parts, "priority="+r)
	}
	if r := fMessage.Raw(); r != "" {
		parts = append(parts, "message="+url.QueryEscape(r))
	}
	if sortBy != "" && sortBy != "date" {
		parts = append(parts, "sort="+sortBy)
	}
	if len(parts) == 0 {
		return ""
	}
	return "?" + strings.Join(parts, "&")
}

func (h *UIHandler) NotificationDismiss(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, _ = h.DB.Exec(
		`UPDATE notifications SET dismissed_at=datetime('now') WHERE id=?`, id,
	)
	returnTo := r.FormValue("_return")
	if returnTo == "" || !strings.HasPrefix(returnTo, "/notifications") {
		returnTo = "/notifications"
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (h *UIHandler) NotificationBulkDismiss(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	ids := r.Form["ids"]
	if len(ids) > 0 {
		placeholders := make([]string, 0, len(ids))
		args := make([]any, 0, len(ids))
		for _, raw := range ids {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				continue
			}
			placeholders = append(placeholders, "?")
			args = append(args, id)
		}
		if len(placeholders) > 0 {
			h.DB.Exec(
				`UPDATE notifications SET dismissed_at=datetime('now') WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
				args...,
			)
		}
	}
	returnTo := r.FormValue("_return")
	if returnTo == "" || !strings.HasPrefix(returnTo, "/notifications") {
		returnTo = "/notifications"
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (h *UIHandler) NotificationDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	h.DB.Exec(`DELETE FROM notifications WHERE id=?`, id)
	returnTo := r.FormValue("_return")
	if returnTo == "" || !strings.HasPrefix(returnTo, "/notifications") {
		returnTo = "/notifications?show=dismissed"
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (h *UIHandler) NotificationRestore(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, _ = h.DB.Exec(
		`UPDATE notifications SET dismissed_at=NULL WHERE id=?`, id,
	)
	returnTo := r.FormValue("_return")
	if returnTo == "" || !strings.HasPrefix(returnTo, "/notifications") {
		returnTo = "/notifications?show=dismissed"
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (h *UIHandler) NotificationComment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	comment := r.FormValue("comment")
	if len(comment) > 2000 {
		http.Error(w, "comment too long (max 2000 characters)", http.StatusBadRequest)
		return
	}
	_, _ = h.DB.Exec(`UPDATE notifications SET comment=? WHERE id=?`, comment, id)
	returnTo := r.FormValue("_return")
	if returnTo == "" || !strings.HasPrefix(returnTo, "/notifications") {
		returnTo = "/notifications"
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

// --- Plans ---

func (h *UIHandler) PlansPage(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")
	if statusFilter == "" {
		statusFilter = "pending"
	}

	var counts struct{ Pending, Approved, Rejected, Conflicts int }
	h.DB.QueryRow(`SELECT COUNT(*) FROM plans WHERE status='pending' AND NOT ` + conflictDetailCond).Scan(&counts.Pending)
	h.DB.QueryRow(`SELECT COUNT(*) FROM plans WHERE status='pending' AND ` + conflictDetailCond).Scan(&counts.Conflicts)
	h.DB.QueryRow(`SELECT COUNT(*) FROM plans WHERE status='approved'`).Scan(&counts.Approved)
	h.DB.QueryRow(`SELECT COUNT(*) FROM plans WHERE status='rejected'`).Scan(&counts.Rejected)

	notifBadge, planBadge, threadBadge := navBadges(h.DB)
	data := map[string]any{
		"StatusFilter": statusFilter,
		"Counts":       counts,
		"CSRFToken":    h.csrfToken(r),
		"Username":     h.username(r),
		"NotifBadge":   notifBadge,
		"PlanBadge":    planBadge,
		"ThreadBadge":  threadBadge,
	}

	if statusFilter == "conflicts" {
		data["ConflictGroups"] = h.loadConflictGroups()
	} else {
		query := `SELECT id, COALESCE(project_code,''), title, detail, status, comment, created_at, decided_at FROM plans`
		var args []any
		switch statusFilter {
		case "pending":
			query += ` WHERE status='pending' AND NOT ` + conflictDetailCond
		case "all":
			// no filter
		default:
			query += ` WHERE status=?`
			args = append(args, statusFilter)
		}
		query += ` ORDER BY id DESC LIMIT 200`

		rows, err := h.DB.Query(query, args...)
		if err != nil {
			log.Printf("plans query: %v", err)
			http.Error(w, "internal error", 500)
			return
		}
		defer rows.Close()

		var plans []model.Plan
		for rows.Next() {
			var p model.Plan
			var decidedAt sql.NullTime
			if err := rows.Scan(&p.ID, &p.ProjectCode, &p.Title, &p.Detail, &p.Status, &p.Comment, &p.CreatedAt, &decidedAt); err != nil {
				continue
			}
			if decidedAt.Valid {
				p.DecidedAt = &decidedAt.Time
			}
			plans = append(plans, p)
		}
		data["Plans"] = plans
	}

	h.Tmpls.ExecuteTemplate(w, "plans.html", data)
}

func (h *UIHandler) loadConflictGroups() []conflictGroup {
	rows, err := h.DB.Query(
		`SELECT id, COALESCE(project_code,''), title, detail, status, comment, created_at, decided_at
		 FROM plans WHERE status='pending' AND ` + conflictDetailCond + ` ORDER BY id DESC LIMIT 200`,
	)
	if err != nil {
		log.Printf("conflict plans query: %v", err)
		return nil
	}
	defer rows.Close()

	var conflicts []model.Plan
	for rows.Next() {
		var p model.Plan
		var decidedAt sql.NullTime
		if err := rows.Scan(&p.ID, &p.ProjectCode, &p.Title, &p.Detail, &p.Status, &p.Comment, &p.CreatedAt, &decidedAt); err != nil {
			continue
		}
		if decidedAt.Valid {
			p.DecidedAt = &decidedAt.Time
		}
		conflicts = append(conflicts, p)
	}

	allIDs := make(map[int64]bool)
	groupIDs := make([][]int64, len(conflicts))
	for i, c := range conflicts {
		ids := parseConflictOriginalIDs(c.Detail)
		groupIDs[i] = ids
		for _, id := range ids {
			allIDs[id] = true
		}
	}

	origMap := make(map[int64]model.Plan)
	if len(allIDs) > 0 {
		idSlice := make([]int64, 0, len(allIDs))
		for id := range allIDs {
			idSlice = append(idSlice, id)
		}
		placeholders := strings.Repeat("?,", len(idSlice))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]any, len(idSlice))
		for i, id := range idSlice {
			args[i] = id
		}
		orows, err := h.DB.Query(
			`SELECT id, COALESCE(project_code,''), title, detail, status, comment, created_at, decided_at
			 FROM plans WHERE id IN (`+placeholders+`)`, args...,
		)
		if err == nil {
			defer orows.Close()
			for orows.Next() {
				var p model.Plan
				var decidedAt sql.NullTime
				if orows.Scan(&p.ID, &p.ProjectCode, &p.Title, &p.Detail, &p.Status, &p.Comment, &p.CreatedAt, &decidedAt) == nil {
					if decidedAt.Valid {
						p.DecidedAt = &decidedAt.Time
					}
					origMap[p.ID] = p
				}
			}
		}
	}

	var groups []conflictGroup
	for i, c := range conflicts {
		g := conflictGroup{Resolution: c}
		g.Unresolved = strings.Contains(c.Detail, `"merge_conflict_unresolved"`)
		for _, id := range groupIDs[i] {
			if p, ok := origMap[id]; ok {
				g.Originals = append(g.Originals, p)
			}
		}
		groups = append(groups, g)
	}
	return groups
}

func parseConflictOriginalIDs(detail string) []int64 {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(detail), &obj); err != nil {
		return nil
	}

	if raw, ok := obj["knowledge_ref"]; ok {
		var ref string
		if json.Unmarshal(raw, &ref) == nil {
			if ids := parseMergeConflictRef(ref); len(ids) > 0 {
				return ids
			}
		}
	}

	if raw, ok := obj["source_plan_ids"]; ok {
		var ids []int64
		if json.Unmarshal(raw, &ids) == nil {
			return ids
		}
	}

	return nil
}

func parseMergeConflictRef(ref string) []int64 {
	const prefix = "merge_conflict:["
	if !strings.HasPrefix(ref, prefix) || !strings.HasSuffix(ref, "]") {
		return nil
	}
	inner := ref[len(prefix) : len(ref)-1]
	parts := strings.Fields(inner)
	var ids []int64
	for _, p := range parts {
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil
		}
		ids = append(ids, id)
	}
	return ids
}

func (h *UIHandler) PlanDecide(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	status := r.FormValue("status")
	comment := r.FormValue("comment")

	validStatuses := map[string]bool{"approved": true, "rejected": true, "pending": true}
	if !validStatuses[status] {
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}

	if status == "pending" {
		h.DB.Exec(`UPDATE plans SET status='pending', comment=?, decided_at=NULL WHERE id=?`, comment, id)
	} else {
		h.DB.Exec(`UPDATE plans SET status=?, comment=?, decided_at=datetime('now') WHERE id=?`, status, comment, id)
	}

	returnTo := r.FormValue("_return")
	if returnTo == "" || !strings.HasPrefix(returnTo, "/plans") {
		returnTo = "/plans"
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (h *UIHandler) PlanDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	h.DB.Exec(`DELETE FROM plans WHERE id=?`, id)
	returnTo := r.FormValue("_return")
	if returnTo == "" || !strings.HasPrefix(returnTo, "/plans") {
		returnTo = "/plans"
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

type planEditForm struct {
	RuleName     string
	RuleType     string
	Patterns     string
	Filter       string
	RequireReply bool
	Constraint   string
}

func parsePlanDetail(detail string) (planEditForm, map[string]any) {
	var raw map[string]any
	var form planEditForm
	if json.Unmarshal([]byte(detail), &raw) != nil {
		return form, raw
	}
	if c, ok := raw["constraint"].(string); ok {
		form.Constraint = c
	}
	if rule, ok := raw["proposed_rule"].(map[string]any); ok {
		if n, ok := rule["name"].(string); ok {
			form.RuleName = n
		}
		if t, ok := rule["type"].(string); ok {
			form.RuleType = t
		}
		if params, ok := rule["params"].(map[string]any); ok {
			if patterns, ok := params["patterns"].([]any); ok {
				var lines []string
				for _, p := range patterns {
					if s, ok := p.(string); ok {
						lines = append(lines, s)
					}
				}
				form.Patterns = strings.Join(lines, "\n")
			}
			if f, ok := params["filter"].(string); ok {
				form.Filter = f
			}
			if rr, ok := params["require_reply"].(bool); ok {
				form.RequireReply = rr
			}
		}
	}
	return form, raw
}

func (h *UIHandler) PlanEditPage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var p model.Plan
	var decidedAt sql.NullTime
	err := h.DB.QueryRow(
		`SELECT id, COALESCE(project_code,''), title, detail, status, comment, created_at, decided_at FROM plans WHERE id=?`, id,
	).Scan(&p.ID, &p.ProjectCode, &p.Title, &p.Detail, &p.Status, &p.Comment, &p.CreatedAt, &decidedAt)
	if err != nil {
		http.Error(w, "plan not found", 404)
		return
	}
	if decidedAt.Valid {
		p.DecidedAt = &decidedAt.Time
	}

	form, _ := parsePlanDetail(p.Detail)
	returnTo := r.URL.Query().Get("return")
	if returnTo == "" {
		returnTo = "/plans"
	}

	notifBadge, planBadge, threadBadge := navBadges(h.DB)
	h.Tmpls.ExecuteTemplate(w, "plan_edit.html", map[string]any{
		"Plan":        p,
		"Form":        form,
		"ReturnTo":    returnTo,
		"CSRFToken":   h.csrfToken(r),
		"Username":    h.username(r),
		"NotifBadge":  notifBadge,
		"PlanBadge":   planBadge,
		"ThreadBadge": threadBadge,
	})
}

func (h *UIHandler) PlanEditSave(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var detail string
	h.DB.QueryRow(`SELECT detail FROM plans WHERE id=?`, id).Scan(&detail)

	var raw map[string]any
	if json.Unmarshal([]byte(detail), &raw) != nil {
		raw = make(map[string]any)
	}

	patterns := strings.Split(strings.TrimSpace(r.FormValue("patterns")), "\n")
	var cleaned []string
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}

	rule := map[string]any{
		"name": strings.TrimSpace(r.FormValue("rule_name")),
		"type": strings.TrimSpace(r.FormValue("rule_type")),
		"params": map[string]any{
			"patterns": cleaned,
		},
	}
	params := rule["params"].(map[string]any)
	if f := strings.TrimSpace(r.FormValue("filter")); f != "" {
		params["filter"] = f
	}
	if r.FormValue("require_reply") == "on" {
		params["require_reply"] = true
	}

	raw["proposed_rule"] = rule
	constraint := strings.TrimSpace(r.FormValue("constraint"))
	if constraint != "" {
		raw["constraint"] = constraint
	} else {
		delete(raw, "constraint")
	}

	newDetail, err := json.Marshal(raw)
	if err != nil {
		http.Error(w, "failed to encode detail", 500)
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	h.DB.Exec(`UPDATE plans SET title=?, detail=? WHERE id=?`, title, string(newDetail), id)

	returnTo := r.FormValue("_return")
	if returnTo == "" || !strings.HasPrefix(returnTo, "/plans") {
		returnTo = "/plans"
	}
	http.Redirect(w, r, returnTo, http.StatusFound)
}

// --- Admin: OAuth2 clients ---

func (h *UIHandler) AdminClientsPage(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(
		`SELECT client_id, name, created_at, revoked_at, last_used_at, last_used_ip FROM oauth2_clients ORDER BY created_at DESC`,
	)
	if err != nil {
		log.Printf("clients query: %v", err)
		http.Error(w, "internal error", 500)
		return
	}
	defer rows.Close()

	type clientRow struct {
		ClientID   string
		Name       string
		CreatedAt  string
		Revoked    bool
		LastUsedAt string
		LastUsedIP string
	}
	var clients []clientRow
	for rows.Next() {
		var cr clientRow
		var revokedAt, lastUsedAt, lastUsedIP sql.NullString
		if err := rows.Scan(&cr.ClientID, &cr.Name, &cr.CreatedAt, &revokedAt, &lastUsedAt, &lastUsedIP); err != nil {
			continue
		}
		cr.Revoked = revokedAt.Valid
		if lastUsedAt.Valid {
			cr.LastUsedAt = lastUsedAt.String
		}
		if lastUsedIP.Valid {
			cr.LastUsedIP = lastUsedIP.String
		}
		clients = append(clients, cr)
	}

	notifBadge, planBadge, threadBadge := navBadges(h.DB)
	h.Tmpls.ExecuteTemplate(w, "admin_clients.html", map[string]any{
		"Clients":     clients,
		"CSRFToken":   h.csrfToken(r),
		"Username":    h.username(r),
		"NewSecret":   r.URL.Query().Get("secret"),
		"NewClient":   r.URL.Query().Get("client_id"),
		"NotifBadge":  notifBadge,
		"PlanBadge":   planBadge,
		"ThreadBadge": threadBadge,
	})
}

func (h *UIHandler) AdminAuditPage(w http.ResponseWriter, r *http.Request) {
	rows, err := h.DB.Query(
		`SELECT id, event, actor, remote_ip, detail, created_at FROM audit_log ORDER BY id DESC LIMIT 200`,
	)
	if err != nil {
		log.Printf("audit query: %v", err)
		http.Error(w, "internal error", 500)
		return
	}
	defer rows.Close()

	var entries []model.AuditEntry
	for rows.Next() {
		var e model.AuditEntry
		if err := rows.Scan(&e.ID, &e.Event, &e.Actor, &e.RemoteIP, &e.Detail, &e.CreatedAt); err != nil {
			continue
		}
		entries = append(entries, e)
	}

	notifBadge, planBadge, threadBadge := navBadges(h.DB)
	h.Tmpls.ExecuteTemplate(w, "admin_audit.html", map[string]any{
		"Entries":     entries,
		"Username":    h.username(r),
		"NotifBadge":  notifBadge,
		"PlanBadge":   planBadge,
		"ThreadBadge": threadBadge,
	})
}

func (h *UIHandler) AdminClientCreate(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	if name == "" {
		http.Redirect(w, r, "/admin/clients", http.StatusFound)
		return
	}
	clientID, secret, err := auth.CreateOAuth2Client(h.DB, name)
	if err != nil {
		log.Printf("create client: %v", err)
		http.Error(w, "internal error", 500)
		return
	}
	auth.WriteAudit(h.DB, "oauth_client_created", h.username(r), realIP(r), name)
	http.Redirect(w, r, "/admin/clients?client_id="+clientID+"&secret="+secret, http.StatusFound)
}

func (h *UIHandler) AdminClientRevoke(w http.ResponseWriter, r *http.Request) {
	clientID := r.PathValue("id")
	_ = auth.RevokeOAuth2Client(h.DB, clientID)
	auth.WriteAudit(h.DB, "oauth_client_revoked", h.username(r), realIP(r), clientID)
	http.Redirect(w, r, "/admin/clients", http.StatusFound)
}

// --- Root redirect ---

func (h *UIHandler) Root(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/todo", http.StatusFound)
}
