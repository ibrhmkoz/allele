package loop

import "testing"

func TestCount(t *testing.T) {
	if got := Count(3); got != 3 {
		t.Fatalf("got %d, want 3", got)
	}
}

func TestLast(t *testing.T) {
	if got := Last([]int{1, 2}); got != 2 {
		t.Fatalf("got %d, want 2", got)
	}
}

// TestBroken fails on the unmutated package, so it stays out of the matrix.
func TestBroken(t *testing.T) {
	t.Fatal("broken")
}
