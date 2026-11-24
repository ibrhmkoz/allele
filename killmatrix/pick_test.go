package killmatrix_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
	"github.com/ibrhmkoz/go-kit/xtesting"
)

// TestPick: after the baseline, Pick sees each line's candidates with how
// many top-level tests reached and infected them, and only the mutant it
// keeps runs and lands in the matrix, on a cache hit too. Discount's three
// tests (200 VIP, 200, 50 VIP) all reach line 4; none sits at the
// boundary, so none infects price >= 100. Only the VIP over 100 reaches
// line 5.
func TestPick(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var seen []string
	r := killmatrix.Runner{Workers: min(4, runtime.NumCPU()), BaselineRuns: 1, TimeoutPad: 500 * time.Millisecond, Schemata: true, Select: true, FailFast: true, Infect: true, CacheDir: t.TempDir()}
	r.Pick = func(cs []killmatrix.Candidate) int {
		var b strings.Builder
		for _, c := range cs {
			fmt.Fprintf(&b, "  %d %s %q→%q reached %d infected %d\n", c.Line, c.Operator, c.Original, c.Mutated, c.Reached, c.Infected)
		}
		mu.Lock()
		seen = append(seen, b.String())
		mu.Unlock()
		return len(cs) - 1
	}
	dir := copyDiscount(t)
	m := run(t, r, dir, ".")
	slices.Sort(seen)
	xtesting.EqualText(t, strings.Join(seen, "--\n"), `
		  4 COR "price > 100 && vip"→"true" reached 3 infected 2
		  4 COR "price > 100 && vip"→"false" reached 3 infected 1
		  4 ROR "price > 100"→"price < 100" reached 3 infected 3
		  4 ROR "price > 100"→"price <= 100" reached 3 infected 3
		  4 ROR "price > 100"→"price >= 100" reached 3 infected 0
		  4 ROR "price > 100"→"price == 100" reached 3 infected 2
		  4 ROR "price > 100"→"price != 100" reached 3 infected 1
		  4 COR "price > 100 && vip"→"price > 100 || vip" reached 3 infected 2
		--
		  5 AOR "price - 20"→"price + 20" reached 1 infected 1
		  5 AOR "price - 20"→"price * 20" reached 1 infected 1
		  5 AOR "price - 20"→"price / 20" reached 1 infected 1
		  5 AOR "price - 20"→"price % 20" reached 1 infected 1
	`)
	xtesting.EqualText(t, listMutants(m), `
		4 COR "price > 100 && vip"→"price > 100 || vip" killed
		5 AOR "price - 20"→"price % 20" killed
	`)
	seen = nil
	again := run(t, r, dir, ".")
	xtesting.EqualText(t, listMutants(again), listMutants(m))
	if len(seen) != 0 {
		t.Errorf("a cache hit picked again: %v", seen)
	}
	// A new test reruns the baseline and Pick; the kills of unchanged
	// tests are reused, so neither kept mutant runs again.
	testPath := filepath.Join(dir, "discount_test.go")
	orig, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(testPath, append(orig, "\nfunc TestMore(t *testing.T) { _ = Discount(1, false) }\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	seen = nil
	third := run(t, r, dir, ".")
	if strings.Contains(strings.Join(seen, ""), "reached -1") {
		t.Errorf("a reused kill left a candidate uncounted:\n%s", strings.Join(seen, "--\n"))
	}
	xtesting.EqualText(t, listMutants(third), listMutants(m))
	for _, res := range third.Results {
		if res.MS > 0 {
			t.Errorf("M%d ran again against test %d", res.MutantID, res.TestID)
		}
	}

	// -all shares the cache directory and gets every mutant, not the
	// ones Pick kept.
	all := r
	all.Pick = nil
	if n := len(run(t, all, dir, ".").Mutants); n != 12 {
		t.Errorf("-all after Pick: %d mutants, want 12", n)
	}

	// Without a schema the baseline records no reach.
	seen = nil
	own := r
	own.Schemata, own.CacheDir = false, ""
	run(t, own, copyDiscount(t), ".")
	for line := range strings.Lines(strings.Join(seen, "")) {
		if !strings.HasSuffix(line, "reached -1 infected -1\n") {
			t.Errorf("counts without a schema: %s", line)
		}
	}
	if len(seen) != 2 {
		t.Errorf("Pick ran on %d lines without a schema, want 2", len(seen))
	}
}

func listMutants(m *killmatrix.Matrix) string {
	v := m.Verdicts()
	var b strings.Builder
	for _, mu := range m.Mutants {
		fmt.Fprintf(&b, "%d %s %q→%q %s\n", mu.Line, mu.Operator, mu.Original, mu.Mutated, v[mu.ID])
	}
	return b.String()
}

// TestPickPackages: ids stay unique across packages when Pick leaves
// candidates out; a reused id would mix two mutants' results.
func TestPickPackages(t *testing.T) {
	t.Parallel()
	r := killmatrix.Runner{Workers: min(4, runtime.NumCPU()), BaselineRuns: 1, TimeoutPad: 500 * time.Millisecond, Schemata: true, Select: true, FailFast: true, Infect: true}
	r.Pick = func(cs []killmatrix.Candidate) int { return 0 }
	all := r
	all.Pick = nil
	patterns := []string{"./discount", "./loop", "./table"}
	got, every := run(t, r, "testdata", patterns...), run(t, all, "testdata", patterns...)
	// Each kept mutant's verdict is the one it gets when every mutant runs.
	want := map[string]killmatrix.Verdict{}
	v := every.Verdicts()
	for _, mu := range every.Mutants {
		want[fmt.Sprintf("%s:%d:%d %s", mu.Pkg, mu.Line, mu.Col, mu.Mutated)] = v[mu.ID]
	}
	v = got.Verdicts()
	for _, mu := range got.Mutants {
		if k := fmt.Sprintf("%s:%d:%d %s", mu.Pkg, mu.Line, mu.Col, mu.Mutated); v[mu.ID] != want[k] {
			t.Errorf("%s: %s, alone %s", k, v[mu.ID], want[k])
		}
	}
}

// TestPickCountsPassingTests: a failing test never runs against mutants,
// so what it reaches and infects must not steer Pick.
func TestPickCountsPassingTests(t *testing.T) {
	t.Parallel()
	dir := copyDiscount(t)
	testPath := filepath.Join(dir, "discount_test.go")
	orig, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatal(err)
	}
	broken := "\nfunc TestBroken(t *testing.T) { _ = Discount(100, true); t.Fatal(\"broken\") }\n"
	if err := os.WriteFile(testPath, append(orig, broken...), 0o600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var seen []string
	r := killmatrix.Runner{Workers: 2, BaselineRuns: 1, TimeoutPad: 500 * time.Millisecond, Schemata: true, Select: true, FailFast: true, Infect: true}
	r.Pick = func(cs []killmatrix.Candidate) int {
		mu.Lock()
		defer mu.Unlock()
		for _, c := range cs {
			seen = append(seen, fmt.Sprintf("%s reached %d infected %d", c.Mutated, c.Reached, c.Infected))
		}
		return 0
	}
	run(t, r, dir, ".")
	if !slices.Contains(seen, "price >= 100 reached 3 infected 0") {
		t.Errorf("the failing test at the boundary counted:\n%s", strings.Join(seen, "\n"))
	}
}
