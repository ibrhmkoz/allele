// Package load has a test that, once a mutant breaks it, keeps failing
// for a while whatever the code, the way timing tests fail on a loaded
// machine.
package load

// Pick returns n when it is positive, else 0.
func Pick(n int) int {
	if n > 0 {
		return n
	}
	return 0
}
