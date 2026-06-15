package todoreader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteItem(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.txt")
	content := `Ideas Backlog
==============

- [ ] first item  #Q2 #2026-06-12
- [ ] multi-line item  #Q1 #2026-06-12
      continuation line one
      continuation line two
- [x] third item  #Q3 #2026-06-12
`
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	items, _ := Load(p)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// Delete the multi-line item (its line index is item[1].ID). Its two
	// indented continuation lines must go with it.
	if err := DeleteItem(p, int(items[1].ID)); err != nil {
		t.Fatalf("delete: %v", err)
	}

	after, _ := Load(p)
	if len(after) != 2 {
		t.Fatalf("expected 2 items after delete, got %d", len(after))
	}
	for _, it := range after {
		if strings.Contains(it.Text, "multi-line") {
			t.Error("deleted item still present")
		}
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "continuation line") {
		t.Error("orphaned continuation line not removed")
	}
	if !strings.Contains(string(raw), "first item") || !strings.Contains(string(raw), "third item") {
		t.Error("delete removed the wrong lines")
	}
}

func TestDeleteItemOutOfRange(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.txt")
	os.WriteFile(p, []byte("- [ ] only  #Q2 #2026-06-12\n"), 0644)
	if err := DeleteItem(p, 99); err == nil {
		t.Error("expected out-of-range error")
	}
}

func TestBulkDelete(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.txt")
	content := `- [ ] one  #Q2 #2026-06-12
- [ ] two  #Q1 #2026-06-12
      continuation of two
- [ ] three  #Q3 #2026-06-12
- [ ] four  #Q4 #2026-06-12
`
	os.WriteFile(p, []byte(content), 0644)
	items, _ := Load(p)
	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(items))
	}

	// Delete "one" (idx items[0]) and "three" (items[2]) at once. "two"'s
	// continuation line must survive; "three" has none.
	if err := BulkDelete(p, []int{int(items[0].ID), int(items[2].ID)}); err != nil {
		t.Fatalf("bulk delete: %v", err)
	}
	after, _ := Load(p)
	if len(after) != 2 {
		t.Fatalf("expected 2 items after bulk delete, got %d", len(after))
	}
	raw, _ := os.ReadFile(p)
	s := string(raw)
	for _, gone := range []string{"] one", "] three"} {
		if strings.Contains(s, gone) {
			t.Errorf("%q should be deleted", gone)
		}
	}
	if !strings.Contains(s, "] two") || !strings.Contains(s, "continuation of two") || !strings.Contains(s, "] four") {
		t.Errorf("bulk delete removed the wrong lines:\n%s", s)
	}
}

func TestBulkSetStatus(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.txt")
	content := `- [ ] one  #Q2 #2026-06-12
- [ ] two  #Q1 #2026-06-12
- [ ] three  #Q3 #2026-06-12
`
	os.WriteFile(p, []byte(content), 0644)
	items, _ := Load(p)

	// Mark one and three done; leave two open.
	if err := BulkSetStatus(p, []int{int(items[0].ID), int(items[2].ID)}, "done"); err != nil {
		t.Fatalf("bulk set status: %v", err)
	}
	after, _ := Load(p)
	got := map[string]string{}
	for _, it := range after {
		got[it.Text] = it.Status
	}
	if got["one"] != "done" || got["three"] != "done" {
		t.Errorf("selected items not marked done: %v", got)
	}
	if got["two"] != "open" {
		t.Errorf("unselected item changed: %v", got)
	}
}
