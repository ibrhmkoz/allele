package stall

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTwice stalls for a second on its first try against each mutant, and
// never again: a marker file remembers the try, keyed by the mutant and
// the process that runs the test binaries.
func TestTwice(t *testing.T) {
	if id := os.Getenv("GOMUTANT"); id != "" && id != "0" {
		marker := filepath.Join(os.TempDir(), fmt.Sprintf("killmatrix-stall-%s-%d", id, os.Getppid()))
		if _, err := os.Stat(marker); err != nil {
			_ = os.WriteFile(marker, nil, 0o600)
			time.Sleep(time.Second)
		}
	}
	if got := Twice(2); got != 4 {
		t.Fatal(got)
	}
}
