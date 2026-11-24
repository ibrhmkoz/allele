// Package loop has a mutant that never ends and one that does not compile.
package loop

import "strconv"

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

// code is an error whose value is its message.
type code string

func (c code) Error() string { return string(c) }

// Check returns a concrete error type, so returning nil does not compile.
func Check(n int) code {
	return code(strconv.Itoa(n))
}
