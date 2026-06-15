package dedup

import (
	"testing"

	"github.com/topolik/mnd-mind-model/internal/distill"
)

func ins(id, strength string, occ int) distill.Insight {
	return distill.Insight{ID: id, Category: "decision_heuristic", Statement: "stmt " + id, Strength: strength, Occurrences: occ,
		Evidence: []distill.Evidence{{Moment: "m" + id}}}
}

// T58: merge sums occurrences, unions evidence, keeps strongest, drops absorbed.
func TestApply(t *testing.T) {
	insights := []distill.Insight{
		ins("a", "moderate", 1), ins("b", "strong", 1), ins("c", "weak", 1), ins("z", "strong", 4),
	}
	groups := []Group{{Canonical: "a", IDs: []string{"a", "b", "c"}, Statement: "merged directive"}}
	out, merged := Apply(insights, groups)

	if len(out) != 2 { // a (merged) + z (untouched)
		t.Fatalf("want 2 after merge, got %d: %+v", len(out), out)
	}
	if len(merged) != 1 || merged[0].Canonical != "a" || len(merged[0].Absorbed) != 2 {
		t.Fatalf("merge record wrong: %+v", merged)
	}
	var a *distill.Insight
	for i := range out {
		if out[i].ID == "a" {
			a = &out[i]
		}
	}
	if a == nil {
		t.Fatal("canonical 'a' missing")
	}
	if a.Occurrences != 3 {
		t.Fatalf("occurrences should sum to 3, got %d", a.Occurrences)
	}
	if a.Strength != "strong" { // absorbed 'b' was strong
		t.Fatalf("should keep strongest, got %s", a.Strength)
	}
	if a.Statement != "merged directive" || len(a.Evidence) != 3 {
		t.Fatalf("statement/evidence wrong: %q / %d", a.Statement, len(a.Evidence))
	}
}

// T59: idempotent — re-applying a group whose members are gone is a no-op.
func TestApplyIdempotent(t *testing.T) {
	insights := []distill.Insight{ins("a", "strong", 1), ins("b", "weak", 1)}
	g := []Group{{Canonical: "a", IDs: []string{"a", "b"}, Statement: "m"}}
	out, _ := Apply(insights, g)
	out2, merged2 := Apply(out, g) // b already absorbed
	if len(out2) != 1 || len(merged2) != 0 {
		t.Fatalf("second apply must be a no-op, got %d insights, %d merges", len(out2), len(merged2))
	}
}

// T60: parse drops groups with <2 known ids; canonical falls back to first id.
func TestParseGroups(t *testing.T) {
	active := map[string]distill.Insight{"a": {}, "b": {}, "c": {}}
	resp := `{"groups":[
	  {"canonical":"a","ids":["a","b"],"statement":"x"},
	  {"canonical":"ghost","ids":["c","ghost"],"statement":"y"},
	  {"canonical":"a","ids":["a"],"statement":"z"}
	]}`
	groups, dropped, err := ParseGroups(resp, active)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Canonical != "a" || len(groups[0].IDs) != 2 {
		t.Fatalf("only a,b group should survive, got %+v", groups)
	}
	if len(dropped) != 2 {
		t.Fatalf("two groups should drop, got %v", dropped)
	}
}
