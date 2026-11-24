package discount

import "testing"

func TestVIPOver100(t *testing.T) {
	if got := Discount(200, true); got != 180 {
		t.Fatalf("got %d, want 180", got)
	}
}

func TestRegularOver100(t *testing.T) {
	if got := Discount(200, false); got != 200 {
		t.Fatalf("got %d, want 200", got)
	}
}

func TestVIPUnder100(t *testing.T) {
	if got := Discount(50, true); got != 50 {
		t.Fatalf("got %d, want 50", got)
	}
}
