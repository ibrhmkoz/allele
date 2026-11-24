package batch

import (
	"testing"
	"time"
)

func TestAtInside(t *testing.T) {
	if got := At([]int{7, 8}, 1); got != 8 {
		t.Fatal(got)
	}
}

// TestAsyncEnd crashes the process from Async's goroutine, before any
// other test starts, where i >= len(xs) becomes i > len(xs).
func TestAsyncEnd(t *testing.T) {
	if got := Async([]int{7, 8}, 2); got != -1 {
		t.Fatal(got)
	}
}

// TestAtEnd panics in the test, which marks it failed, where i >= len(xs)
// becomes i > len(xs).
func TestAtEnd(t *testing.T) {
	if got := At([]int{7, 8}, 2); got != -1 {
		t.Fatal(got)
	}
}

// The parallel tests run at once, so a crash in one leaves both
// unfinished and neither marked failed.
func TestParallelEnd(t *testing.T) {
	t.Parallel()
	// Still running when TestParallelBelow crashes.
	time.Sleep(20 * time.Millisecond)
	if got := Async([]int{1}, 1); got != -1 {
		t.Fatal(got)
	}
}

func TestParallelBelow(t *testing.T) {
	t.Parallel()
	if got := Async([]int{1}, -1); got != -1 {
		t.Fatal(got)
	}
}

// TestStepsZero comes first: where n != 0 becomes n <= 0 it hangs, and the
// test after it, which would fail, never runs in the same process.
func TestStepsZero(t *testing.T) {
	if got := Steps(0); got != 0 {
		t.Fatal(got)
	}
}

func TestStepsThree(t *testing.T) {
	if got := Steps(3); got != 3 {
		t.Fatal(got)
	}
}
