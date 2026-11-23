package flaky

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestPick fails when Pick is wrong, but only on the first try for each
// mutant: a marker file remembers the try, keyed by the mutant and the
// process that runs the test binaries.
func TestPick(t *testing.T) {
	if Pick(1) == 1 {
		return
	}
	marker := filepath.Join(os.TempDir(), fmt.Sprintf("killmatrix-flaky-%s-%d", os.Getenv("GOMUTANT"), os.Getppid()))
	if _, err := os.Stat(marker); err == nil {
		_ = os.Remove(marker)
		return
	}
	_ = os.WriteFile(marker, nil, 0o600)
	t.Fatal("wrong pick")
}
