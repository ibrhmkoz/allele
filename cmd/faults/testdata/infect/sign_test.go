package sign

import (
	"os"
	"testing"
)

// TestMain needs the package's directory, as fixtures do, even to list tests.
func TestMain(m *testing.M) {
	if _, err := os.Stat("sign.go"); err != nil {
		os.Exit(3)
	}
	os.Exit(m.Run())
}

func TestFive(t *testing.T) { _ = Positive(5) }

// TestZero runs in the package's directory or not at all.
func TestZero(t *testing.T) {
	if _, err := os.Stat("sign.go"); err == nil {
		_ = Positive(0)
	}
}

func TestNothing(t *testing.T) {}

func TestSkipped(t *testing.T) { _ = Positive(0) }

// TestCases' subtest "at zero" is revealing; "above" stays in the old suite.
func TestCases(t *testing.T) {
	t.Run("above", func(t *testing.T) { _ = Never(5) })
	t.Run("at zero", func(t *testing.T) { _ = Never(0) })
}
