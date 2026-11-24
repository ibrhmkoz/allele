// Package adequacy measures how well a set of tests does against a kill
// matrix, counting each fault the mutants stand for once.
//
// A raw mutation score counts every mutant, so ten copies of an easy mutant
// weigh ten times as much as one hard mutant. Score counts dominator mutants
// instead (Kurtz, Ammann et al., FSE 2016): mutant A subsumes B when every
// test that kills A also kills B, and the dominators are the mutants no
// other mutant strictly subsumes, one per kill set.
package adequacy

import (
	"fmt"
	"math/bits"
	"math/rand/v2"
	"slices"
	"strings"
)

// Set is an immutable set of tests, numbered from 0.
type Set struct{ words []uint64 }

// NewSet returns the set of the given tests.
func NewSet(tests ...int) Set {
	var s Set
	for _, t := range tests {
		s = s.With(t)
	}
	return s
}

// All returns the set of tests 0 to n-1.
func All(n int) Set {
	s := Set{words: make([]uint64, (n+63)/64)}
	for t := range n {
		s.words[t/64] |= 1 << (t % 64)
	}
	return s
}

// With returns s plus test t.
func (s Set) With(t int) Set {
	words := make([]uint64, max(len(s.words), t/64+1))
	copy(words, s.words)
	words[t/64] |= 1 << (t % 64)
	return Set{words: words}
}

func (s Set) Len() int {
	n := 0
	for _, w := range s.words {
		n += bits.OnesCount64(w)
	}
	return n
}

func (s Set) Empty() bool { return s.Len() == 0 }

func (s Set) Intersects(o Set) bool {
	for i := range min(len(s.words), len(o.words)) {
		if s.words[i]&o.words[i] != 0 {
			return true
		}
	}
	return false
}

func (s Set) SubsetOf(o Set) bool {
	for i, w := range s.words {
		var ow uint64
		if i < len(o.words) {
			ow = o.words[i]
		}
		if w&^ow != 0 {
			return false
		}
	}
	return true
}

func (s Set) equal(o Set) bool { return s.SubsetOf(o) && o.SubsetOf(s) }

func (s Set) tests() []int {
	var ts []int
	for i, w := range s.words {
		for w != 0 {
			ts = append(ts, i*64+bits.TrailingZeros64(w))
			w &= w - 1
		}
	}
	return ts
}

func (s Set) String() string { return "{" + strings.Trim(fmt.Sprint(s.tests()), "[]") + "}" }

// Matrix is a kill matrix: for each mutant, the set of tests that kill it.
type Matrix struct {
	tests      int
	kills      []Set
	dominators []int
}

// New returns the matrix of mutants whose kill sets are kills, over tests
// numbered 0 to tests-1. A mutant no test kills has an empty kill set.
func New(tests int, kills []Set) *Matrix {
	m := &Matrix{tests: tests, kills: slices.Clone(kills)}
	m.dominators = m.findDominators()
	return m
}

func (m *Matrix) Tests() int { return m.tests }

// Dominators returns one mutant per minimal kill set, the first one in
// mutant order.
func (m *Matrix) Dominators() []int { return slices.Clone(m.dominators) }

// Mutants sharing a kill set form a class; a class is minimal when no other
// class's kill set is a strict subset of its own. Quadratic in classes,
// which stay in the low thousands for one repository.
func (m *Matrix) findDominators() []int {
	var classes []int // first mutant of each distinct, non-empty kill set
	for i, k := range m.kills {
		if !k.Empty() && !slices.ContainsFunc(classes, func(c int) bool { return m.kills[c].equal(k) }) {
			classes = append(classes, i)
		}
	}
	var dominators []int
	for _, c := range classes {
		strictlySubsumed := slices.ContainsFunc(classes, func(o int) bool {
			return o != c && m.kills[o].SubsetOf(m.kills[c])
		})
		if !strictlySubsumed {
			dominators = append(dominators, c)
		}
	}
	return dominators
}

// Score returns the share of dominators tests kill, 0 when there are none.
func (m *Matrix) Score(tests Set) float64 {
	if len(m.dominators) == 0 {
		return 0
	}
	killed := 0
	for _, d := range m.dominators {
		if m.kills[d].Intersects(tests) {
			killed++
		}
	}
	return float64(killed) / float64(len(m.dominators))
}

// Adequate returns a random test set that kills every killable mutant
// among mutants: it visits them in random order and, for each one not yet
// killed, adds a random test that kills it. This models a developer who
// writes tests until the given mutants are dead.
func (m *Matrix) Adequate(mutants []int, r *rand.Rand) Set {
	order := slices.Clone(mutants)
	r.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	var tests Set
	for _, i := range order {
		k := m.kills[i]
		if k.Empty() || k.Intersects(tests) {
			continue
		}
		ts := k.tests()
		tests = tests.With(ts[r.IntN(len(ts))])
	}
	return tests
}

// Coupled returns, in order, the mutants that tests in frt kill and no
// other test does. With frt the tests a fix added, which fail on the code
// before it, these are the mutants that stood for the fault: the suite
// before the fix could not kill them either (Just et al., FSE 2014).
func (m *Matrix) Coupled(frt Set) []int {
	coupled := []int{}
	for i, k := range m.kills {
		if k.Intersects(frt) && k.SubsetOf(frt) {
			coupled = append(coupled, i)
		}
	}
	return coupled
}

// Outcome is how well tests written against a subset of mutants do against
// all of them.
type Outcome struct {
	Mutants, Total int
	// Tests is the mean size of the adequate test sets.
	Tests float64
	// Score is their mean Score; Random is the mean Score of random test
	// sets of the same sizes, which controls for test set size.
	Score, Random float64
}

func (o Outcome) String() string {
	return fmt.Sprintf("mutants %d/%d · tests %.2f · score %.2f · random tests %.2f", o.Mutants, o.Total, o.Tests, o.Score, o.Random)
}

// Simulate runs the selective mutation protocol runs times: build a test set
// adequate for mutants, score it on all mutants, and score a random test set
// of the same size beside it.
func (m *Matrix) Simulate(mutants []int, runs int, r *rand.Rand) Outcome {
	o := Outcome{Mutants: len(mutants), Total: len(m.kills)}
	for range runs {
		tests := m.Adequate(mutants, r)
		o.Tests += float64(tests.Len())
		o.Score += m.Score(tests)
		o.Random += m.Score(NewSet(r.Perm(m.tests)[:tests.Len()]...))
	}
	o.Tests /= float64(runs)
	o.Score /= float64(runs)
	o.Random /= float64(runs)
	return o
}

func (m *Matrix) String() string {
	killable := 0
	for _, k := range m.kills {
		if !k.Empty() {
			killable++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d mutants, %d killable, %d dominators\n", len(m.kills), killable, len(m.dominators))
	for _, d := range m.dominators {
		fmt.Fprintf(&b, "%-4s%v\n", fmt.Sprint("M", d+1), m.kills[d])
	}
	return b.String()
}
