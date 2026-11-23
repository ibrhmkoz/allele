// Package stall has a test that stalls against a mutant on the first try
// only, the way a busy machine stalls a fast test now and then.
package stall

// Twice returns 2n.
func Twice(n int) int {
	return n * 2
}
