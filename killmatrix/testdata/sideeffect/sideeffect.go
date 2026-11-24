// Package sideeffect returns an error from a call that also does work: the
// mutant that returns nil instead skips the work, not just the error.
package sideeffect

// T holds the length of what it was loaded from.
type T struct{ n int }

func (t *T) fill(s string) error {
	t.n = len(s)
	return nil
}

// Load returns a T filled from s.
func Load(s string) (*T, error) {
	t := &T{}
	return t, t.fill(s)
}
