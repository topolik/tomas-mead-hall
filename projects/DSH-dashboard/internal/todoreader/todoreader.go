package todoreader

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"dsh/internal/model"
)

var (
	itemRe     = regexp.MustCompile(`^- \[(.)\] (.+)$`)
	suffixRe   = regexp.MustCompile(`\s{2}#(Q[1-4]) #(\d{4}-\d{2}-\d{2})\s*$`)
	priorityRe = regexp.MustCompile(`Priority:\s*(Q[1-4])`)
	addedRe    = regexp.MustCompile(`Added:\s*(\d{4}-\d{2}-\d{2})`)
)

func statusStr(c byte) string {
	switch c {
	case '~':
		return "in_progress"
	case 'x':
		return "done"
	case '-':
		return "parked"
	default:
		return "open"
	}
}

func statusChar(s string) byte {
	switch s {
	case "in_progress":
		return '~'
	case "done":
		return 'x'
	case "parked":
		return '-'
	default:
		return ' '
	}
}

// Load reads all todo items from path. Non-item lines are silently skipped.
// Items with no inline suffix fall back to scanning continuation lines for
// Priority and Added fields (supports the legacy multi-line format).
func Load(path string) ([]model.BacklogItem, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	var items []model.BacklogItem
	for i, line := range lines {
		m := itemRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		status := statusStr(m[1][0])
		text := m[2]
		priority := "Q2"
		addedDate := ""

		if sm := suffixRe.FindStringSubmatchIndex(text); sm != nil {
			priority = text[sm[2]:sm[3]]
			addedDate = text[sm[4]:sm[5]]
			text = strings.TrimRight(text[:sm[0]], " ")
		} else {
			// Legacy multi-line format: scan indented continuation lines.
			for j := i + 1; j < len(lines) && isContinuation(lines[j]); j++ {
				if pm := priorityRe.FindStringSubmatch(lines[j]); pm != nil {
					priority = pm[1]
				}
				if am := addedRe.FindStringSubmatch(lines[j]); am != nil {
					addedDate = am[1]
				}
			}
		}

		items = append(items, model.BacklogItem{
			ID:        int64(i),
			Text:      text,
			Status:    status,
			Priority:  priority,
			AddedDate: addedDate,
		})
	}
	return items, nil
}

func isContinuation(line string) bool {
	return len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
}

// Add appends a new item using the single-line format, unless an item with the
// same normalized text already exists. Callers such as the GML distill step
// re-post the same action items every cycle (the distilling LLM re-derives them
// from the same dismissed insights), so deduping at this single write boundary
// keeps todo.txt clean regardless of caller. Returns whether a line was written.
func Add(path, text, priority string) (bool, error) {
	existing, err := Load(path)
	if err != nil {
		return false, err
	}
	key := normalizeTodoText(text)
	for _, it := range existing {
		if normalizeTodoText(it.Text) == key {
			return false, nil // duplicate — skip
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	date := time.Now().Format("2006-01-02")
	if _, err := fmt.Fprintf(f, "- [ ] %s  #%s #%s\n", text, priority, date); err != nil {
		return false, err
	}
	return true, nil
}

// normalizeTodoText canonicalizes a todo's text for duplicate detection:
// case-insensitive with collapsed whitespace. Compared against Load()'s Text
// (which has already stripped the trailing "#priority #date" suffix).
func normalizeTodoText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// UpdateStatus changes the status marker on the item line at lineIdx.
func UpdateStatus(path string, lineIdx int, newStatus string) error {
	return updateLine(path, lineIdx, func(line string) string {
		m := itemRe.FindStringSubmatch(line)
		if m == nil {
			return line
		}
		return fmt.Sprintf("- [%c] %s", statusChar(newStatus), m[2])
	})
}

// UpdateItem rewrites both the text and priority of the item at lineIdx,
// preserving the original added date.
func UpdateItem(path string, lineIdx int, text, priority, addedDate string) error {
	if addedDate == "" {
		addedDate = time.Now().Format("2006-01-02")
	}
	return updateLine(path, lineIdx, func(line string) string {
		m := itemRe.FindStringSubmatch(line)
		if m == nil {
			return line
		}
		return fmt.Sprintf("- [%s] %s  #%s #%s", m[1], text, priority, addedDate)
	})
}

// UpdatePriority updates the priority in the inline suffix of the item at lineIdx.
// For legacy items without an inline suffix, it appends one.
func UpdatePriority(path string, lineIdx int, priority string) error {
	return updateLine(path, lineIdx, func(line string) string {
		m := itemRe.FindStringSubmatch(line)
		if m == nil {
			return line
		}
		text := m[2]
		date := time.Now().Format("2006-01-02")
		if sm := suffixRe.FindStringSubmatchIndex(text); sm != nil {
			date = text[sm[4]:sm[5]]
			text = strings.TrimRight(text[:sm[0]], " ")
		}
		return fmt.Sprintf("- [%s] %s  #%s #%s", m[1], text, priority, date)
	})
}

// DeleteItem removes the item line at lineIdx and any indented continuation lines.
func DeleteItem(path string, lineIdx int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	if lineIdx < 0 || lineIdx >= len(lines) {
		return fmt.Errorf("line %d out of range", lineIdx)
	}
	end := lineIdx + 1
	for end < len(lines) && isContinuation(lines[end]) {
		end++
	}
	lines = append(lines[:lineIdx], lines[end:]...)
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

// BulkDelete removes the item lines at the given indices and their indented
// continuation lines, in a single read-modify-write pass. Indices that shift as
// earlier lines are removed are a non-issue here because the whole file is
// rewritten once from a deletion set rather than mutated index-by-index.
func BulkDelete(path string, lineIdxs []int) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	del := make(map[int]bool)
	for _, idx := range lineIdxs {
		if idx < 0 || idx >= len(lines) {
			continue
		}
		del[idx] = true
		for j := idx + 1; j < len(lines) && isContinuation(lines[j]); j++ {
			del[j] = true
		}
	}
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if del[i] {
			continue
		}
		out = append(out, line)
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0644)
}

// BulkSetStatus sets the status marker on the item lines at the given indices,
// in a single read-modify-write pass. Non-item lines (and out-of-range indices)
// are skipped. Setting status does not change line count, so indices stay valid.
func BulkSetStatus(path string, lineIdxs []int, newStatus string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	set := make(map[int]bool)
	for _, idx := range lineIdxs {
		set[idx] = true
	}
	for i := range lines {
		if !set[i] {
			continue
		}
		m := itemRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		lines[i] = fmt.Sprintf("- [%c] %s", statusChar(newStatus), m[2])
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}

func updateLine(path string, lineIdx int, fn func(string) string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	if lineIdx < 0 || lineIdx >= len(lines) {
		return fmt.Errorf("line %d out of range", lineIdx)
	}
	lines[lineIdx] = fn(lines[lineIdx])
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}
