package main

import (
	"cmp"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/ibrhmkoz/allele/killmatrix"
	"github.com/ibrhmkoz/allele/pkg/xtesting"
)

// refMatrix: module m, package p (at the root) and p/sub. T1 kills M1, T2
// kills M2 and M3; M4 survives; M5 times out on T1, M6 does not build,
// M7 and M8 share a key; M9 and M10 sit in two packages whose directories
// end alike. In the rerun M3 survives, so it
// is not stable.
func refMatrix(rerun bool) *killmatrix.Matrix {
	mu := func(id int, pkg, file string, line int, compiled bool) killmatrix.Mutant {
		return killmatrix.Mutant{ID: id, Pkg: pkg, File: file, Line: line, Col: 1, Operator: "ROR", Original: "a > b", Mutated: "a >= b", Compiled: compiled}
	}
	m := &killmatrix.Matrix{
		Mutants: []killmatrix.Mutant{mu(1, "example.com/m", "p.go", 1, true), mu(2, "example.com/m", "p.go", 2, true), mu(3, "example.com/m/sub", "s.go", 3, true), mu(4, "example.com/m/sub", "s.go", 4, true), mu(5, "example.com/m", "p.go", 5, true), mu(6, "example.com/m", "p.go", 6, false), mu(7, "example.com/m", "p.go", 7, true), mu(8, "example.com/m", "p.go", 7, true), mu(9, "example.com/m/a/util", "u.go", 9, true), mu(10, "example.com/m/b/util", "u.go", 9, true)},
		Tests:   []killmatrix.Test{{ID: 1, Name: "T1", State: killmatrix.OK}, {ID: 2, Name: "T2", State: killmatrix.OK}, {ID: 3, Name: "T3", State: killmatrix.OK}},
	}
	res := func(mid, tid int, s killmatrix.Status) {
		m.Results = append(m.Results, killmatrix.Result{MutantID: mid, TestID: tid, Status: s})
	}
	res(1, 1, killmatrix.Killed)
	res(1, 2, killmatrix.Survived)
	res(2, 2, killmatrix.Killed)
	res(3, 2, killmatrix.Killed)
	res(4, 1, killmatrix.Survived)
	res(4, 3, killmatrix.Survived)
	res(5, 1, killmatrix.Timeout)
	res(9, 1, killmatrix.Killed)
	res(10, 1, killmatrix.Survived)
	if rerun {
		m.Results[3].Status = killmatrix.Survived
	}
	return m
}

func reported(line int, file, v string) verdict {
	return verdict{File: file, Line: line, Col: 1, Operator: "ROR", Original: "a > b", Mutated: "a >= b", Verdict: v}
}

// TestGrade: each kind of verdict error lands in its own count, mutants
// the two exhaustive runs disagree on count for none, and a mutant the
// reference lacks counts as unknown.
func TestGrade(t *testing.T) {
	ref := newReference("example.com/m", refMatrix(false), refMatrix(true))
	grade := func(vs ...verdict) string {
		s := ref.grade("m", vs, 5)
		return fmt.Sprintf("reported %d unknown %d stable %d hidden %d false %d timeout %d build %d", s.Reported, s.Unknown, s.Stable, s.Hidden, s.False, s.Timeout, s.Build)
	}
	xtesting.EqualText(t, grade(
		reported(1, "p.go", "killed"),
		reported(2, "p.go", "killed"),
		reported(3, "sub/s.go", "survived"), // unstable: not counted
		reported(4, "sub/s.go", "not covered"),
		reported(5, "p.go", "timeout"),
	), "reported 5 unknown 0 stable 4 hidden 0 false 0 timeout 0 build 0")
	xtesting.EqualText(t, grade(
		reported(1, "p.go", "survived"),       // false gap
		reported(2, "p.go", "timeout"),        // kill read as a timeout
		reported(4, "sub/s.go", "killed"),     // hidden gap
		reported(5, "p.go", "build error"),    // build regression
		reported(9, "p.go", "killed"),         // not in the reference
		reported(4, "other/s.go", "survived"), // another package
		reported(6, "p.go", "build error"),    // did not build in the reference either
		reported(7, "p.go", "killed"),         // two reference mutants share its key
	), "reported 8 unknown 4 stable 4 hidden 1 false 1 timeout 1 build 1")
	// Packages are told apart by their whole directory; a mutant reported
	// twice counts once; a verdict mutant does not define hides a gap.
	xtesting.EqualText(t, grade(
		reported(9, "a/util/u.go", "killed"),
		reported(9, "b/util/u.go", "survived"),
		reported(9, "b/util/u.go", "survived"),
		reported(1, "p.go", "dead"),
	), "reported 4 unknown 1 stable 3 hidden 1 false 0 timeout 0 build 0")
	// A kill where the reference timed out is no error.
	xtesting.EqualText(t, grade(reported(5, "p.go", "killed")), "reported 1 unknown 0 stable 1 hidden 0 false 0 timeout 0 build 0")
}

// TestGradeOrder: the order a build reports its mutants in does not move
// D, on a matrix of 40 mutants with random kill sets over 8 tests.
func TestGradeOrder(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	m := &killmatrix.Matrix{}
	for id := 1; id <= 8; id++ {
		m.Tests = append(m.Tests, killmatrix.Test{ID: id, Name: fmt.Sprint("T", id), State: killmatrix.OK})
	}
	var vs []verdict
	for id := 1; id <= 40; id++ {
		m.Mutants = append(m.Mutants, killmatrix.Mutant{ID: id, Pkg: "example.com/m", File: "p.go", Line: id, Col: 1, Operator: "ROR", Original: "a > b", Mutated: "a >= b", Compiled: true})
		for test := 1; test <= 8; test++ {
			if r.IntN(4) == 0 {
				m.Results = append(m.Results, killmatrix.Result{MutantID: id, TestID: test, Status: killmatrix.Killed})
			}
		}
		if id%2 == 0 {
			vs = append(vs, reported(id, "p.go", "killed"))
		}
	}
	ref := newReference("example.com/m", m, nil)
	forward := ref.grade("m", vs, 3).D
	xtesting.EqualText(t, fmt.Sprintf("%.4f", forward), "0.9198")
	slices.Reverse(vs)
	if backward := ref.grade("m", vs, 3).D; backward != forward {
		t.Errorf("D %v forward, %v backward", forward, backward)
	}
}

func TestWorse(t *testing.T) {
	a := score{D: 0.9, False: 1}
	for _, c := range []struct {
		b    score
		want bool
	}{
		{score{D: 0.9, False: 2}, true},   // more errors
		{score{D: 0.8, False: 1}, true},   // as many, lower D
		{score{D: 0.9, False: 1}, false},  // the same
		{score{D: 0.95, False: 1}, false}, // as many, higher D
		{score{D: 0.1, False: 0}, false},  // fewer errors, however low D
	} {
		if got := c.b.worse(a); got != c.want {
			t.Errorf("%+v worse than %+v: %v", c.b, a, got)
		}
	}
}

// TestCombine: the exhaustive anchor scores 1; twice as fast makes up for
// 1 - 2^-Gamma less quality and no more; giving up a repository, or every
// verdict, gives up everything.
func TestCombine(t *testing.T) {
	perfect := []score{{D: 1, Stable: 10}, {D: 1, Stable: 10}}
	if m := combine("x", perfect, T0).M; math.Abs(m-1) > 1e-12 {
		t.Errorf("anchor M = %v", m)
	}
	slow := combine("x", []score{{D: 0.9, Stable: 10}}, 100)
	q := 0.9 * math.Pow(2, -Gamma)
	fast := combine("x", []score{{D: q, Stable: 10}}, (100+Floor)/2-Floor)
	if math.Abs(fast.M-slow.M) > 1e-9 {
		t.Errorf("break-even: slow %v, fast %v", slow.M, fast.M)
	}
	if d := combine("x", []score{{D: 0.25, Stable: 1}, {D: 1, Stable: 1}}, 1).D; math.Abs(d-0.5) > 1e-12 {
		t.Errorf("D of 0.25 and 1: %v, want their geometric mean 0.5", d)
	}
	if m := combine("x", nil, 1); m.D != 0 || m.M != 0 {
		t.Errorf("no repository: %+v", m)
	}
	if m := combine("x", []score{{D: 1, Stable: 10}, {D: 0, Stable: 10}}, 1).M; m != 0 {
		t.Errorf("a repository with D 0: M = %v", m)
	}
	if f := combine("x", []score{{D: 1}}, 1).F; f != 0 {
		t.Errorf("no stable mutant reported: F = %v, want 0", f)
	}
	if f := combine("x", []score{{D: 1, Stable: 100, False: 10}}, 1).F; f != 0.8 {
		t.Errorf("10 false gaps in 100 stable mutants: F = %v, want 1 - 2·10/100", f)
	}
	if m := combine("x", []score{{D: 1, Stable: 200, Hidden: 3}}, 1).M; m != 0 {
		t.Errorf("1.5%% of stable mutants hide a gap: M = %v, want 0", m)
	}
	if f := combine("x", []score{{D: 1, Stable: 200, Hidden: 2}}, 1).F; f != 1-10.0/200 {
		t.Errorf("1%% of stable mutants hide a gap, the most allowed: F = %v", f)
	}
}

// TestCounter prints which count each wrong verdict adds to: rows are what
// the reference says, columns what the build says.
func TestCounter(t *testing.T) {
	names := map[kind]string{unknown: "unknown", noBuild: "build", dead: "killed", hung: "timeout", gap: "gap"}
	var s score
	counts := map[*int]string{&s.Hidden: "hidden", &s.False: "false", &s.Timeout: "timeout", &s.Build: "build"}
	var b strings.Builder
	fmt.Fprintf(&b, "%-8s", "")
	for _, got := range []kind{unknown, noBuild, dead, hung, gap} {
		fmt.Fprintf(&b, "%-8s", names[got])
	}
	b.WriteString("\n")
	for _, want := range []kind{dead, hung, gap} {
		fmt.Fprintf(&b, "%-8s", names[want])
		for _, got := range []kind{unknown, noBuild, dead, hung, gap} {
			fmt.Fprintf(&b, "%-8s", cmp.Or(counts[s.counter(want, got)], "-"))
		}
		b.WriteString("\n")
	}
	xtesting.EqualText(t, b.String(), `
		        unknown build   killed  timeout gap
		killed  hidden  build   -       timeout false
		timeout hidden  build   -       -       false
		gap     hidden  build   hidden  hidden  -
	`)
}
