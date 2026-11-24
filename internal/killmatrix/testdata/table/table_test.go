package table

import "testing"

func TestAbs(t *testing.T) {
	// index panics when Abs goes negative, which crashes the test binary
	// before the subtests after it report.
	t.Run("index", func(t *testing.T) {
		if digits := []int{0, 1, 2}; digits[Abs(-2)] != 2 {
			t.Fatal("wrong digit")
		}
	})
	for _, c := range []struct {
		name    string
		in, out int
	}{{"negative", -2, 2}, {"positive", 3, 3}, {"zero", 0, 0}} {
		t.Run(c.name, func(t *testing.T) {
			if got := Abs(c.in); got != c.out {
				t.Fatalf("Abs(%d) = %d, want %d", c.in, got, c.out)
			}
		})
	}
}
