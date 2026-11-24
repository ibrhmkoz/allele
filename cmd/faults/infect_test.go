package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
	"github.com/ibrhmkoz/go-kit/xtesting"
)

// TestInfections: of the tests not skipped, Five and Zero reach x > 0.
// A test infects a mutant when the mutated condition evaluates otherwise:
// only Zero, at the boundary, tells x > 0 from x >= 0, only Five tells it
// from x < 0, neither from x != 0. Skipped would count for >= 0. Of
// TestCases only "above" runs: Never(5) tells x < 0 from x > 0 and x >= 0
// and x != 0; "at zero" would have told it from x <= 0 and x == 0. No
// test reaches Unused.
func TestInfections(t *testing.T) {
	counts, err := infections(context.Background(), "testdata/infect", []string{"TestSkipped", "TestCases/at_zero"})
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for k, c := range counts {
		lines = append(lines, fmt.Sprintf("%s reached %d infected %d", k, c[0], c[1]))
	}
	slices.Sort(lines)
	xtesting.EqualText(t, strings.Join(lines, "\n")+"\n", `
		sign.go:3:38 ROR "x > 0"→"x != 0" reached 2 infected 0
		sign.go:3:38 ROR "x > 0"→"x < 0" reached 2 infected 1
		sign.go:3:38 ROR "x > 0"→"x <= 0" reached 2 infected 2
		sign.go:3:38 ROR "x > 0"→"x == 0" reached 2 infected 2
		sign.go:3:38 ROR "x > 0"→"x >= 0" reached 2 infected 1
		sign.go:5:35 ROR "x < 0"→"x != 0" reached 1 infected 1
		sign.go:5:35 ROR "x < 0"→"x <= 0" reached 1 infected 0
		sign.go:5:35 ROR "x < 0"→"x == 0" reached 1 infected 0
		sign.go:5:35 ROR "x < 0"→"x > 0" reached 1 infected 1
		sign.go:5:35 ROR "x < 0"→"x >= 0" reached 1 infected 1
		sign.go:7:36 ROR "x == 1"→"x != 1" reached 0 infected 0
		sign.go:7:36 ROR "x == 1"→"x < 1" reached 0 infected 0
		sign.go:7:36 ROR "x == 1"→"x <= 1" reached 0 infected 0
		sign.go:7:36 ROR "x == 1"→"x > 1" reached 0 infected 0
		sign.go:7:36 ROR "x == 1"→"x >= 1" reached 0 infected 0
	`)
}

func TestInfectionsFail(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, c := range map[string]struct {
		ctx context.Context
		dir string
	}{
		"cancelled":  {cancelled, "testdata/infect"},
		"no package": {context.Background(), "testdata/empty"},
		"no build":   {context.Background(), "testdata/broken"},
	} {
		if _, err := infections(c.ctx, c.dir, nil); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestOldSuite(t *testing.T) {
	revealing := []string{"TestNew", "TestA/x_y", "TestA/z", "TestB/deep/er", "TestA/z"}
	var b strings.Builder
	for _, top := range []string{"TestNew", "TestA", "TestB", "TestOld"} {
		run, skip := oldSuite(top, revealing)
		fmt.Fprintf(&b, "%s run %v skip %q\n", top, run, skip)
	}
	xtesting.EqualText(t, b.String(), `
		TestNew run false skip ""
		TestA run true skip "^TestA$/^x_y$|^z$"
		TestB run true skip "^TestB$/^deep$"
		TestOld run true skip ""
	`)
}

func TestCountOf(t *testing.T) {
	mu := killmatrix.Mutant{File: "p.go", Line: 3, Col: 5, Operator: "ROR", Original: "a > b", Mutated: "a >= b"}
	counts := map[string][2]int{mutantKey("p.go", 3, 5, "ROR", "a > b", "a >= b"): {4, 1}}
	other := mu
	other.Mutated = "a < b"
	xtesting.EqualText(t, fmt.Sprint(countOf(counts, mu), countOf(counts, other)), "[4 1] [-1 -1]")
}
