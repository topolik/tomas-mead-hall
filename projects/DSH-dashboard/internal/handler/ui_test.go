package handler

import (
	"html/template"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dsh/internal/db"
)

func newTestUIHandler(t *testing.T) *UIHandler {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	tmpls, err := template.ParseGlob("../../cmd/dsh/web/templates/*.html")
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}
	return &UIHandler{DB: database, Tmpls: tmpls}
}

func TestTodoDelete(t *testing.T) {
	h := newTestUIHandler(t)
	todo := filepath.Join(t.TempDir(), "todo.txt")
	os.WriteFile(todo, []byte("- [ ] keep me  #Q2 #2026-06-12\n- [ ] delete me  #Q2 #2026-06-12\n"), 0644)
	h.TodoPath = todo

	req := httptest.NewRequest("POST", "/todo/1/delete", nil)
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	h.TodoDelete(w, req)

	if w.Result().StatusCode != 302 {
		t.Fatalf("expected redirect, got %d", w.Result().StatusCode)
	}
	raw, _ := os.ReadFile(todo)
	if strings.Contains(string(raw), "delete me") {
		t.Error("item not deleted")
	}
	if !strings.Contains(string(raw), "keep me") {
		t.Error("wrong item deleted")
	}
}

func writeTodo(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "todo.txt")
	os.WriteFile(p, []byte(
		"- [ ] alpha security item  #Q1 #2026-06-10\n"+
			"- [~] beta in progress  #Q2 #2026-06-11\n"+
			"- [x] gamma done item  #Q3 #2026-06-12\n"+
			"- [ ] delta security thing  #Q2 #2026-06-09\n"), 0644)
	return p
}

func TestTodoPage_FilterAndCounts(t *testing.T) {
	h := newTestUIHandler(t)
	h.TodoPath = writeTodo(t)

	// Filter to text "security" — should show alpha + delta only.
	req := httptest.NewRequest("GET", "/todo?text=security", nil)
	w := httptest.NewRecorder()
	h.TodoPage(w, req)
	if w.Result().StatusCode != 200 {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
	body := w.Body.String()
	if !strings.Contains(body, "alpha security item") || !strings.Contains(body, "delta security thing") {
		t.Error("text filter dropped matching items")
	}
	if strings.Contains(body, "beta in progress") || strings.Contains(body, "gamma done item") {
		t.Error("text filter kept non-matching items")
	}
	// Status count badges render the full (unfiltered) picture: 2 open, 1 in_progress, 1 done.
	if !strings.Contains(body, "(2)") || !strings.Contains(body, "Showing 2 of 4") {
		t.Errorf("status counts / showing line wrong:\n%s", body)
	}
}

func TestTodoPage_StatusFilter(t *testing.T) {
	h := newTestUIHandler(t)
	h.TodoPath = writeTodo(t)

	req := httptest.NewRequest("GET", "/todo?status=done", nil)
	w := httptest.NewRecorder()
	h.TodoPage(w, req)
	body := w.Body.String()
	if !strings.Contains(body, "gamma done item") {
		t.Error("status filter dropped the done item")
	}
	if strings.Contains(body, "alpha security item") {
		t.Error("status=done kept a non-done item")
	}
}

func TestTodoBulk_Delete(t *testing.T) {
	h := newTestUIHandler(t)
	h.TodoPath = writeTodo(t)

	form := url.Values{}
	form.Set("action", "delete")
	form.Add("ids", "0") // line 0 = alpha
	form.Add("ids", "2") // line 2 = gamma
	req := httptest.NewRequest("POST", "/todo/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.TodoBulk(w, req)
	if w.Result().StatusCode != 302 {
		t.Fatalf("expected redirect, got %d", w.Result().StatusCode)
	}
	raw, _ := os.ReadFile(h.TodoPath)
	if strings.Contains(string(raw), "alpha") || strings.Contains(string(raw), "gamma") {
		t.Error("bulk delete did not remove selected items")
	}
	if !strings.Contains(string(raw), "beta") || !strings.Contains(string(raw), "delta") {
		t.Error("bulk delete removed unselected items")
	}
}

func TestTodoBulk_MarkDone(t *testing.T) {
	h := newTestUIHandler(t)
	h.TodoPath = writeTodo(t)

	form := url.Values{}
	form.Set("action", "done")
	form.Add("ids", "0")
	form.Add("ids", "1")
	req := httptest.NewRequest("POST", "/todo/bulk", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.TodoBulk(w, req)

	raw, _ := os.ReadFile(h.TodoPath)
	lines := strings.Split(string(raw), "\n")
	if !strings.HasPrefix(lines[0], "- [x] alpha") || !strings.HasPrefix(lines[1], "- [x] beta") {
		t.Errorf("bulk mark-done did not set markers:\n%s", string(raw))
	}
}

func TestProjectDetailPage(t *testing.T) {
	h := newTestUIHandler(t)
	pm := t.TempDir()
	dir := filepath.Join(pm, "DSH-dashboard")
	os.MkdirAll(filepath.Join(dir, "iterations"), 0755)
	os.WriteFile(filepath.Join(dir, "PROJECT.md"),
		[]byte("# Dashboard\n- **Code:** DSH\n- **Status:** Implementation\n"), 0644)
	os.WriteFile(filepath.Join(dir, "iterations", "001-ideation.md"),
		[]byte("# Ideation\nbrainstorm\n"), 0644)
	h.PMPath = pm

	req := httptest.NewRequest("GET", "/projects/DSH", nil)
	req.SetPathValue("code", "DSH")
	w := httptest.NewRecorder()
	h.ProjectDetailPage(w, req)

	if w.Result().StatusCode != 200 {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Dashboard") || !strings.Contains(body, "Ideation") {
		t.Errorf("project detail did not render expected content:\n%s", body)
	}
}

func TestProjectDetailPage_NotFound(t *testing.T) {
	h := newTestUIHandler(t)
	h.PMPath = t.TempDir()
	req := httptest.NewRequest("GET", "/projects/NOPE", nil)
	req.SetPathValue("code", "NOPE")
	w := httptest.NewRecorder()
	h.ProjectDetailPage(w, req)
	if w.Result().StatusCode != 404 {
		t.Fatalf("expected 404, got %d", w.Result().StatusCode)
	}
}
