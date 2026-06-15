package pmreader

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectDetail(t *testing.T) {
	pm := t.TempDir()
	writeFile(t, filepath.Join(pm, "DSH-dashboard", "PROJECT.md"),
		"# Dashboard\n- **Code:** DSH\n- **Status:** Implementation\n- **Priority:** Q2\n")
	writeFile(t, filepath.Join(pm, "DSH-dashboard", "ASSUMPTIONS.md"), "# Assumptions\nDSH-001: foo\n")
	writeFile(t, filepath.Join(pm, "DSH-dashboard", "iterations", "002-planning.md"), "# Planning\nplan body\n")
	writeFile(t, filepath.Join(pm, "DSH-dashboard", "iterations", "001-ideation.md"), "# Ideation\nidea body\n")
	// A second project that must not be returned.
	writeFile(t, filepath.Join(pm, "GML-agent", "PROJECT.md"), "# Gmail\n- **Code:** GML\n")

	d, err := ProjectDetail(pm, "dsh") // case-insensitive
	if err != nil {
		t.Fatalf("ProjectDetail: %v", err)
	}
	if d.Project.Code != "DSH" || d.Project.Name != "Dashboard" {
		t.Errorf("wrong project: %+v", d.Project)
	}
	if d.Assumptions == "" {
		t.Error("assumptions not loaded")
	}
	if len(d.Iterations) != 2 {
		t.Fatalf("expected 2 iterations, got %d", len(d.Iterations))
	}
	// Sorted by filename: 001 before 002.
	if d.Iterations[0].Name != "001-ideation.md" || d.Iterations[1].Name != "002-planning.md" {
		t.Errorf("iterations not sorted: %v, %v", d.Iterations[0].Name, d.Iterations[1].Name)
	}
	if d.Iterations[0].Title != "Ideation" {
		t.Errorf("H1 title not parsed: %q", d.Iterations[0].Title)
	}
}

func TestProjectDetailNotFound(t *testing.T) {
	pm := t.TempDir()
	writeFile(t, filepath.Join(pm, "DSH-dashboard", "PROJECT.md"), "# Dashboard\n- **Code:** DSH\n")
	if _, err := ProjectDetail(pm, "NOPE"); err == nil {
		t.Error("expected not-found error")
	}
}
