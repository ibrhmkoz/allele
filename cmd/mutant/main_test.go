package main

import (
	"io"
	"testing"

	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
)

// TestRunnerConfirmsKills: the command's runner must not count a test that
// fails against a mutant only once, the way a timing test fails under
// load, as a kill. Such a kill would hide a gap from -strict.
func TestRunnerConfirmsKills(t *testing.T) {
	r := runner(options{all: true}, 2, io.Discard)
	m, err := r.Run(t.Context(), "../../killmatrix/testdata", "./flaky")
	if err != nil {
		t.Fatal(err)
	}
	for _, mu := range m.Mutants {
		if v := m.Verdicts()[mu.ID]; v == killmatrix.VerdictKilled {
			t.Errorf("%s:%d %s → %s: %s, but TestPick fails against it only once", mu.File, mu.Line, mu.Original, mu.Mutated, v)
		}
	}
}
