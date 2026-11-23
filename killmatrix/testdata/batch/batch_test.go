package batch

import "testing"

func TestAtInside(t *testing.T) {
	if got := At([]int{7, 8}, 1); got != 8 {
		t.Fatal(got)
	}
}

// TestAtEnd panics, ending the process, where i >= len(xs) becomes i > len(xs).
func TestAtEnd(t *testing.T) {
	if got := At([]int{7, 8}, 2); got != -1 {
		t.Fatal(got)
	}
}

// TestStepsNone hangs where n-- is deleted.
func TestStepsNone(t *testing.T) {
	if got := Steps(3); got != 3 {
		t.Fatal(got)
	}
}

func TestStepsZero(t *testing.T) {
	if got := Steps(0); got != 0 {
		t.Fatal(got)
	}
}

// The parallel tests run at once, so a panic in one leaves both unfinished.
func TestParallelEnd(t *testing.T) {
	t.Parallel()
	if got := At([]int{1}, 1); got != -1 {
		t.Fatal(got)
	}
}

func TestParallelBelow(t *testing.T) {
	t.Parallel()
	if got := At([]int{1}, -1); got != -1 {
		t.Fatal(got)
	}
}
