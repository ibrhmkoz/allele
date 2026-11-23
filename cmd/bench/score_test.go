package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
	"github.com/ibrhmkoz/go-kit/xtesting"
)

// refMatrix: module m, package p (at the root) and p/sub. T1 kills M1, T2
// kills M2 and M3; M4 survives; M5 times out on T1, M6 does not build,
// M7 and M8 share a key. In the rerun M3 survives, so it
// is not stable.
func refMatrix(rerun bool) *killmatrix.Matrix {
	mu := func(id int, pkg, file string, line int, compiled bool) killmatrix.Mutant {
		return killmatrix.Mutant{ID: id, Pkg: pkg, File: file, Line: line, Col: 1, Operator: "ROR", Original: "a > b", Mutated: "a >= b", Compiled: compiled}
	}
	m := &killmatrix.Matrix{
		Mutants: []killmatrix.Mutant{mu(1, "example.com/m", "p.go", 1, true), mu(2, "example.com/m", "p.go", 2, true), mu(3, "example.com/m/sub", "s.go", 3, true), mu(4, "example.com/m/sub", "s.go", 4, true), mu(5, "example.com/m", "p.go", 5, true), mu(6, "example.com/m", "p.go", 6, false), mu(7, "example.com/m", "p.go", 7, true), mu(8, "example.com/m", "p.go", 7, true)},
		Tests:   []killmatrix.Test{{ID: 1, Name: "T1", State: killmatrix.OK}, {ID: 2, Name: "T2", State: killmatrix.OK}},
	}
	res := func(mid, tid int, s killmatrix.Status) {
		m.Results = append(m.Results, killmatrix.Result{MutantID: mid, TestID: tid, Status: s})
	}
	res(1, 1, killmatrix.Killed)
	res(1, 2, killmatrix.Survived)
	res(2, 2, killmatrix.Killed)
	res(3, 2, killmatrix.Killed)
	res(4, 1, killmatrix.Survived)
	res(5, 1, killmatrix.Timeout)
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
	ref := newReference(refMatrix(false), refMatrix(true))
	grade := func(vs ...verdict) string {
		s := ref.grade("m", "m", vs, 5)
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
	ref := newReference(m, nil)
	forward := ref.grade("m", "m", vs, 3).D
	xtesting.EqualText(t, fmt.Sprintf("%.4f", forward), "0.9198")
	slices.Reverse(vs)
	if backward := ref.grade("m", "m", vs, 3).D; backward != forward {
		t.Errorf("D %v forward, %v backward", forward, backward)
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
	if m := combine("x", []score{{D: 1, Stable: 10}, {D: 0, Stable: 10}}, 1).M; m != 0 {
		t.Errorf("a repository with D 0: M = %v", m)
	}
	if f := combine("x", []score{{D: 1}}, 1).F; f != 0 {
		t.Errorf("no stable mutant reported: F = %v, want 0", f)
	}
	if f := combine("x", []score{{D: 1, Stable: 10, Hidden: 1}}, 1).F; f != 0.5 {
		t.Errorf("one hidden gap in 10 stable mutants: F = %v, want 1 - 5/10", f)
	}
	if m := combine("x", []score{{D: 1, Stable: 10, Hidden: 2}}, 1).M; m != 0 {
		t.Errorf("hidden gaps on every stable mutant: M = %v", m)
	}
}
