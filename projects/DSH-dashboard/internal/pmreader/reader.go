package pmreader

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dsh/internal/model"
)

// ScanProjects walks pmPath looking for <project>/PROJECT.md files and parses each one.
func ScanProjects(pmPath string) ([]model.Project, error) {
	entries, err := os.ReadDir(pmPath)
	if err != nil {
		return nil, err
	}

	var projects []model.Project
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mdPath := filepath.Join(pmPath, e.Name(), "PROJECT.md")
		p, err := parseProjectMD(mdPath)
		if err != nil {
			continue // missing or unreadable PROJECT.md — skip silently
		}
		projects = append(projects, p)
	}
	return projects, nil
}

// ProjectDetail returns the drill-down view for the project whose PROJECT.md
// Code field matches code (case-insensitive). The directory is found by
// scanning and matching parsed metadata — the URL-supplied code is never joined
// into a path, so there is no traversal surface.
func ProjectDetail(pmPath, code string) (model.ProjectDetail, error) {
	entries, err := os.ReadDir(pmPath)
	if err != nil {
		return model.ProjectDetail{}, err
	}

	want := strings.ToUpper(strings.TrimSpace(code))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(pmPath, e.Name())
		p, err := parseProjectMD(filepath.Join(dir, "PROJECT.md"))
		if err != nil {
			continue
		}
		if strings.ToUpper(p.Code) != want {
			continue
		}

		d := model.ProjectDetail{Project: p}
		if b, err := os.ReadFile(filepath.Join(dir, "PROJECT.md")); err == nil {
			d.Overview = string(b)
		}
		if b, err := os.ReadFile(filepath.Join(dir, "ASSUMPTIONS.md")); err == nil {
			d.Assumptions = string(b)
		}
		d.Iterations = loadIterations(filepath.Join(dir, "iterations"))
		return d, nil
	}
	return model.ProjectDetail{}, fmt.Errorf("project %q not found", code)
}

// loadIterations reads iterations/*.md, sorted by filename.
func loadIterations(dir string) []model.IterationDoc {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var docs []model.IterationDoc
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		content := string(b)
		docs = append(docs, model.IterationDoc{
			Name:    e.Name(),
			Title:   firstH1(content, e.Name()),
			Content: content,
		})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Name < docs[j].Name })
	return docs
}

// firstH1 returns the text of the first markdown "# " heading, or fallback.
func firstH1(content, fallback string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return fallback
}

// parseProjectMD parses the metadata fields from a PROJECT.md file.
//
// Expected format (markdown list items):
//
//	# Project Name
//	- **Code:** DSH
//	- **Status:** Implementation
//	- **Priority:** Q2 — ...
//	- **Lead:** Developer
//	- **Last updated:** 2026-05-27
func parseProjectMD(path string) (model.Project, error) {
	f, err := os.Open(path)
	if err != nil {
		return model.Project{}, err
	}
	defer f.Close()

	var p model.Project
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		// # Title
		if strings.HasPrefix(line, "# ") && p.Name == "" {
			p.Name = strings.TrimPrefix(line, "# ")
			continue
		}

		// - **Key:** Value
		val, ok := extractField(line, "Code")
		if ok {
			p.Code = val
			continue
		}
		if val, ok = extractField(line, "Status"); ok {
			p.Status = val
			p.CurrentPhase = val
			continue
		}
		if val, ok = extractField(line, "Priority"); ok {
			p.Priority = val
			continue
		}
		if val, ok = extractField(line, "Lead"); ok {
			p.Lead = val
			continue
		}
		if val, ok = extractField(line, "Last updated"); ok {
			p.LastUpdated = val
			continue
		}
	}
	return p, scanner.Err()
}

// extractField parses "- **Key:** Value" and returns (Value, true) on match.
func extractField(line, key string) (string, bool) {
	prefix := "- **" + key + ":** "
	if !strings.HasPrefix(line, prefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, prefix)), true
}
