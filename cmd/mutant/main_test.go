package main

import (
	"fmt"
	"io"
	"testing"

	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
	"github.com/ibrhmkoz/go-kit/xtesting"
)

// TestRunnerConfirmsKills: the command's runner must not count a test that
// fails against a mutant only once, the way a timing test fails under
// load, as a kill. Such a kill would hide a gap from -strict.
func TestRunnerConfirmsKills(t *testing.T) {
	r := runner(options{all: true, noCache: true}, 2, io.Discard)
	if r.CacheDir != "" {
		t.Fatalf("noCache set, got CacheDir=%q", r.CacheDir)
	}
	if withCache := runner(options{}, 2, io.Discard); withCache.CacheDir == "" {
		t.Fatal("default runner should set CacheDir")
	}
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

func TestStrictOK(t *testing.T) {
	for _, tc := range []struct {
		name     string
		verdicts map[int]killmatrix.Verdict
		want     bool
	}{
		{"empty", nil, true},
		{"killed", map[int]killmatrix.Verdict{1: killmatrix.VerdictKilled}, true},
		{"timeout", map[int]killmatrix.Verdict{1: killmatrix.VerdictTimeout}, true},
		{"build error", map[int]killmatrix.Verdict{1: killmatrix.VerdictNoBuild}, true},
		{"all passing verdicts", map[int]killmatrix.Verdict{1: killmatrix.VerdictKilled, 2: killmatrix.VerdictTimeout, 3: killmatrix.VerdictNoBuild}, true},
		{"survived", map[int]killmatrix.Verdict{1: killmatrix.VerdictKilled, 2: killmatrix.VerdictSurvived}, false},
		{"not covered", map[int]killmatrix.Verdict{1: killmatrix.VerdictKilled, 2: killmatrix.VerdictNotCovered}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := strictOK(tc.verdicts); got != tc.want {
				t.Errorf("strictOK(%v) = %v, want %v", tc.verdicts, got, tc.want)
			}
		})
	}
}

// TestRunnerSettings: what the command runs mutants with. The timeout pad
// is measured in §18: 100 ms halves the corpus' time and hides no gap.
func TestRunnerSettings(t *testing.T) {
	r := runner(options{noCache: true}, 8, io.Discard)
	xtesting.EqualText(t, fmt.Sprintf("workers %d, baseline runs %d, timeout 3× baseline + %v, schemata %v, select %v, fail fast %v, confirm %v",
		r.Workers, r.BaselineRuns, r.TimeoutPad, r.Schemata, r.Select, r.FailFast, r.Confirm),
		"workers 8, baseline runs 1, timeout 3× baseline + 100ms, schemata true, select true, fail fast true, confirm true")
}
