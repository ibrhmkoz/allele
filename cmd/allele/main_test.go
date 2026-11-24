package main

import (
	"fmt"
	"io"
	"testing"

	"github.com/ibrhmkoz/allele/internal/killmatrix"
	"github.com/ibrhmkoz/allele/pkg/xtesting"
)

func TestRunnerConfirmsKills(t *testing.T) {
	r := runner(options{all: true, noCache: true}, 2, io.Discard)
	if r.CacheDir != "" {
		t.Fatalf("noCache set, got CacheDir=%q", r.CacheDir)
	}
	if withCache := runner(options{}, 2, io.Discard); withCache.CacheDir == "" || withCache.Pick == nil {
		t.Fatal("default runner should set CacheDir and pick one mutant per line")
	}
	if r.Pick != nil {
		t.Fatal("-all picks no mutant per line")
	}
	m, err := r.Run(t.Context(), "../../internal/killmatrix/testdata", "./flaky")
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

func TestRunnerSettings(t *testing.T) {
	r := runner(options{noCache: true}, 8, io.Discard)
	xtesting.EqualText(t, fmt.Sprintf("workers %d, baseline runs %d, timeout 3× baseline + %v, schemata %v, select %v, infect %v, fail fast %v, confirm %v",
		r.Workers, r.BaselineRuns, r.TimeoutPad, r.Schemata, r.Select, r.Infect, r.FailFast, r.Confirm),
		"workers 8, baseline runs 1, timeout 3× baseline + 100ms, schemata true, select true, infect true, fail fast true, confirm true")
}
