// Package batch has fast tests that, against some mutants, panic, crash
// the process from another goroutine, or hang before a test that would
// fail, so that running them in one process must sort out which test did
// what.
package batch

// At returns xs[i], or -1 when i is out of range.
func At(xs []int, i int) int {
	if i < 0 || i >= len(xs) {
		return -1
	}
	return xs[i]
}

// Async is At on another goroutine: a panic there ends the process with
// no test marked failed.
func Async(xs []int, i int) int {
	ch := make(chan int)
	go func() { ch <- At(xs, i) }()
	return <-ch
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
