package adequacy_test

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ibrhmkoz/allele/pkg/adequacy"
	"github.com/ibrhmkoz/allele/pkg/xtesting"
)

// discount is the kill matrix of the roadmap's Discount example, with
// T1, T2, T3 as tests 0, 1, 2.
func discount() *adequacy.Matrix {
	return adequacy.New(3, []adequacy.Set{
		adequacy.NewSet(1, 2), // M1  if → true
		adequacy.NewSet(0),    // M2  if → false
		adequacy.NewSet(0, 2), // M3  > → <
		adequacy.NewSet(0, 2), // M4  > → <=
		adequacy.NewSet(),     // M5  > → >=, survives
		adequacy.NewSet(0),    // M6  > → ==
		adequacy.NewSet(2),    // M7  > → !=
		adequacy.NewSet(1, 2), // M8  && → ||
		adequacy.NewSet(0),    // M9  - → +
		adequacy.NewSet(0),    // M10 - → *
		adequacy.NewSet(0),    // M11 - → /
		adequacy.NewSet(0),    // M12 - → %
	})
}

func TestDiscount(t *testing.T) {
	m := discount()
	xtesting.Prints(t, m, `
		12 mutants, 11 killable, 2 dominators
		M2  {0}
		M7  {2}
	`)
	// T1 alone kills 8 of 12 mutants, which reads as 67%, but only one of
	// the two dominators: the redundant AOR mutants inflate the raw score.
	if got := m.Score(adequacy.NewSet(0)); got != 0.5 {
		t.Errorf("Score({T1}) = %v, want 0.5", got)
	}
}

func ExampleMatrix_Score() {
	m := discount()
	for _, tests := range []adequacy.Set{adequacy.NewSet(), adequacy.NewSet(0), adequacy.NewSet(1), adequacy.NewSet(0, 2)} {
		fmt.Printf("%v %.2f\n", tests, m.Score(tests))
	}
	// Output:
	// {} 0.00
	// {0} 0.50
	// {1} 0.00
	// {0 2} 1.00
}

func randomMatrix(r *rand.Rand) (*adequacy.Matrix, []adequacy.Set) {
	tests := 1 + r.IntN(70) // past 64, so sets span more than one word
	kills := make([]adequacy.Set, r.IntN(40))
	for i := range kills {
		kills[i] = adequacy.NewSet()
		for t := range tests {
			if r.IntN(4) == 0 {
				kills[i] = kills[i].With(t)
			}
		}
	}
	return adequacy.New(tests, kills), kills
}

func randomTests(r *rand.Rand, n int) adequacy.Set {
	s := adequacy.NewSet()
	for t := range n {
		if r.IntN(2) == 0 {
			s = s.With(t)
		}
	}
	return s
}

func killsAll(kills []adequacy.Set, tests adequacy.Set) bool {
	for _, k := range kills {
		if !k.Empty() && !k.Intersects(tests) {
			return false
		}
	}
	return true
}

func TestProperties(t *testing.T) {
	for seed := range uint64(500) {
		r := rand.New(rand.NewPCG(seed, 0))
		m, kills := randomMatrix(r)
		n := m.Tests()
		tests := randomTests(r, n)

		// The point of dominators: killing them all is the same as killing
		// every killable mutant.
		if got, want := m.Score(tests) == 1, killsAll(kills, tests); len(m.Dominators()) > 0 && got != want {
			t.Fatalf("seed %d: Score(%v) == 1 is %v, killing all is %v", seed, tests, got, want)
		}

		extra := tests.With(r.IntN(n))
		if m.Score(extra) < m.Score(tests) {
			t.Fatalf("seed %d: adding a test lowered the score", seed)
		}

		if len(kills) > 0 {
			withCopy := adequacy.New(n, append(slices.Clip(kills), kills[r.IntN(len(kills))]))
			if withCopy.Score(tests) != m.Score(tests) || len(withCopy.Dominators()) != len(m.Dominators()) {
				t.Fatalf("seed %d: a copied mutant changed the score or dominators", seed)
			}
		}

		all := make([]int, len(kills))
		for i := range all {
			all[i] = i
		}
		adequate := m.Adequate(all, r)
		if !killsAll(kills, adequate) {
			t.Fatalf("seed %d: Adequate(all) = %v misses a killable mutant", seed, adequate)
		}
		if !adequate.SubsetOf(adequacy.All(n)) {
			t.Fatalf("seed %d: Adequate picked a test past %d", seed, n)
		}
	}
}

// TestSetMatchesModel checks Set against a map, on sets of different word
// counts so the methods must handle one set being longer than the other.
func TestSetMatchesModel(t *testing.T) {
	for seed := range uint64(500) {
		r := rand.New(rand.NewPCG(seed, 1))
		a, b := randomTests(r, r.IntN(140)), randomTests(r, r.IntN(140))
		ma, mb := model(a), model(b)

		intersects, subset := false, true
		for x := range ma {
			intersects = intersects || mb[x]
			subset = subset && mb[x]
		}
		if a.Intersects(b) != intersects || a.SubsetOf(b) != subset || a.Len() != len(ma) || a.Empty() != (len(ma) == 0) {
			t.Fatalf("seed %d: a=%v b=%v: Intersects %v SubsetOf %v Len %d", seed, a, b, a.Intersects(b), a.SubsetOf(b), a.Len())
		}
	}
}

func model(s adequacy.Set) map[int]bool {
	m := map[int]bool{}
	for f := range strings.FieldsSeq(strings.Trim(s.String(), "{}")) {
		n, _ := strconv.Atoi(f)
		m[n] = true
	}
	return m
}

// TestDominatorsDefinition checks Dominators against the definition: every
// killable mutant is subsumed by a dominator, no dominator is strictly
// subsumed by any mutant, and no two dominators share a kill set.
func TestDominatorsDefinition(t *testing.T) {
	for seed := range uint64(500) {
		r := rand.New(rand.NewPCG(seed, 2))
		m, kills := randomMatrix(r)
		ds := m.Dominators()
		for i, k := range kills {
			if k.Empty() {
				continue
			}
			if !slices.ContainsFunc(ds, func(d int) bool { return kills[d].SubsetOf(k) }) {
				t.Fatalf("seed %d: M%d %v is subsumed by no dominator", seed, i+1, k)
			}
			for _, d := range ds {
				if k.SubsetOf(kills[d]) && !kills[d].SubsetOf(k) {
					t.Fatalf("seed %d: dominator M%d %v is strictly subsumed by M%d %v", seed, d+1, kills[d], i+1, k)
				}
			}
		}
		for i, d := range ds {
			for _, e := range ds[i+1:] {
				if kills[d].SubsetOf(kills[e]) && kills[e].SubsetOf(kills[d]) {
					t.Fatalf("seed %d: dominators M%d and M%d share a kill set", seed, d+1, e+1)
				}
			}
		}
	}
}

func TestSimulate(t *testing.T) {
	m := discount()
	// Only the AOR mutants: every adequate test set is {T1}, which kills one
	// dominator of two. A random single test averages a third: T1 and T3
	// score 0.5, T2 scores 0.
	got := m.Simulate([]int{8, 9, 10, 11}, 300, rand.New(rand.NewPCG(1, 2)))
	xtesting.Prints(t, got, `
		mutants 4/12 · tests 1.00 · score 0.50 · random tests 0.35
	`)
}

func ExampleMatrix_Coupled() {
	m := discount()
	// A fix whose new test is T3 (2): only M7 (> → !=) is killed by T3 and
	// by no other test, so the fault is coupled to M7. A fix revealed by
	// T2 (1) alone is coupled to nothing: T3 also kills M1 and M8.
	fmt.Println(m.Coupled(adequacy.NewSet(2)), m.Coupled(adequacy.NewSet(1)), m.Coupled(adequacy.NewSet(1, 2)))
	// Output:
	// [6] [] [0 6 7]
}

func TestCoupledProperties(t *testing.T) {
	for seed := range uint64(300) {
		r := rand.New(rand.NewPCG(seed, 3))
		m, kills := randomMatrix(r)
		frt := randomTests(r, m.Tests())
		for _, i := range m.Coupled(frt) {
			if !kills[i].Intersects(frt) || !kills[i].SubsetOf(frt) {
				t.Fatalf("seed %d: M%d %v is not killed by %v alone", seed, i+1, kills[i], frt)
			}
		}
		all := adequacy.All(m.Tests())
		killable := 0
		for _, k := range kills {
			if !k.Empty() {
				killable++
			}
		}
		if got := len(m.Coupled(all)); got != killable {
			t.Fatalf("seed %d: Coupled(all) = %d mutants, want the %d killable", seed, got, killable)
		}
	}
}

func TestAdequateShuffleAndBounds(t *testing.T) {
	// M0 is killed only by {0} and M1 by {0, 1}. Visiting M0 first always picks
	// {0}; shuffling visits M1 first half the time and sometimes picks {0, 1}.
	m := adequacy.New(2, []adequacy.Set{adequacy.NewSet(0), adequacy.NewSet(0, 1)})
	r := rand.New(rand.NewPCG(1, 2))
	seen := map[string]bool{}
	for range 30 {
		seen[m.Adequate([]int{0, 1}, r).String()] = true
	}
	if !seen["{0}"] || !seen["{0 1}"] {
		t.Fatalf("Adequate produced %v, want both {0} and {0 1}", seen)
	}

	s64, s128 := adequacy.NewSet(64), adequacy.NewSet(128)
	if s64.Len() != 1 || s128.Len() != 1 || s128.SubsetOf(adequacy.NewSet()) || !adequacy.NewSet().SubsetOf(s128) {
		t.Fatalf("sparse sets failed: s64=%v s128=%v", s64, s128)
	}
}
