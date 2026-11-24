package load

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestPick fails when Pick is wrong. The first time a mutant breaks it,
// the machine also gets loaded: every run fails until the original code
// runs once, the way timing tests fail on a busy machine. Markers are
// keyed by the process that runs the test binaries; the runner must run
// one mutant at a time.
func TestPick(t *testing.T) {
	dir, parent := os.TempDir(), os.Getppid()
	load := filepath.Join(dir, fmt.Sprintf("killmatrix-load-%d", parent))
	if _, err := os.Stat(load); err == nil {
		if Pick(1) == 1 {
			_ = os.Remove(load) // the original ran: the load is gone
		}
		t.Fatal("loaded")
	}
	if Pick(1) == 1 {
		return
	}
	seen := filepath.Join(dir, fmt.Sprintf("killmatrix-load-%d-%s", parent, os.Getenv("ALLELE")))
	if _, err := os.Stat(seen); err != nil {
		_ = os.WriteFile(seen, nil, 0o600)
		_ = os.WriteFile(load, nil, 0o600)
	}
	t.Fatal("wrong pick")
}
