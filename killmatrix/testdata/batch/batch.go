// Package batch has fast tests that, against some mutants, panic, hang or
// fail together, so that running them in one process must sort out which
// test did what.
package batch

// At returns xs[i], or -1 when i is out of range.
func At(xs []int, i int) int {
	if i < 0 || i >= len(xs) {
		return -1
	}
	return xs[i]
}

// Steps counts n down to zero.
func Steps(n int) int {
	steps := 0
	for n != 0 {
		n--
		steps++
	}
	return steps
}
