package exclude

import "testing"

// Shell-side ledger writer (orchestrate.sh) must produce identical hashes.
// Shell: tr lower | tr -s space | sed trim | sha256sum
// Expected value computed by that exact pipeline for this input.
func TestShellHashParity(t *testing.T) {
	got := NormHash("Store the  Execution logs in\nflat files. KISS.")
	want := "3da5f6eaa462c4589b3c565d105e37d8d6d06b7757252e2c5650db3622657231"
	if got != want {
		t.Fatalf("shell/Go hash divergence:\n  go:    %s\n  shell: %s", got, want)
	}
}
