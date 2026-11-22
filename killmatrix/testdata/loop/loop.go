// Package loop has a mutant that never ends and one that does not compile.
package loop

// Count counts up to n one step at a time.
func Count(n int) int {
	c := 0
	for i := 0; ; i++ {
		if i >= n {
			return c
		}
		c++
	}
}

// Last returns the last element of xs, or 0 if xs is empty.
func Last(xs []int) (last int) {
	for _, x := range xs {
		last = x
	}
	return
}
