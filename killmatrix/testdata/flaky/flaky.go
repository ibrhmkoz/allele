// Package flaky has a test that fails against a mutant on the first try
// only, the way a timing-sensitive test fails under load.
package flaky

// Pick returns n when it is positive, else 0.
func Pick(n int) int {
	if n > 0 {
		return n
	}
	return 0
}
