// Package slowbatch has fast tests that together take longer than any one
// of them: a batch of them must not time out against a mutant they all
// pass.
package slowbatch

// Double returns 2n.
func Double(n int) int {
	return n * 2
}
