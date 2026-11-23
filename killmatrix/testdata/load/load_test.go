package load

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestPick fails when Pick is wrong, and leaves a marker that makes every
// run fail until one runs the right Pick: the machine stays loaded until
// the original code runs. The marker is keyed by the process that runs
// the test binaries; the runner must run one mutant at a time.
func TestPick(t *testing.T) {
	marker := filepath.Join(os.TempDir(), fmt.Sprintf("killmatrix-load-%d", os.Getppid()))
	if _, err := os.Stat(marker); err == nil {
		if Pick(1) == 1 {
			_ = os.Remove(marker)
		}
		t.Fatal("loaded")
	}
	if Pick(1) != 1 {
		_ = os.WriteFile(marker, nil, 0o600)
		t.Fatal("wrong pick")
	}
}
