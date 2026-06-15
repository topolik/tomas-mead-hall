package todoreader

import (
	"path/filepath"
	"testing"
)

func TestAddDedup(t *testing.T) {
	p := filepath.Join(t.TempDir(), "todo.txt")

	if added, err := Add(p, "[GML] read google alerts", "Q2"); err != nil || !added {
		t.Fatalf("first add: added=%v err=%v", added, err)
	}
	// exact repeat
	if added, _ := Add(p, "[GML] read google alerts", "Q2"); added {
		t.Error("exact duplicate should be skipped")
	}
	// case + whitespace variant → still duplicate
	if added, _ := Add(p, "[gml]   READ   google alerts", "Q3"); added {
		t.Error("normalized duplicate (case/space) should be skipped")
	}
	// genuinely different text → added
	if added, _ := Add(p, "[GML] read AWS alerts", "Q2"); !added {
		t.Error("distinct todo should be added")
	}

	items, _ := Load(p)
	if len(items) != 2 {
		t.Fatalf("expected 2 items after dedup, got %d", len(items))
	}
}
