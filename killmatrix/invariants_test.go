package killmatrix_test

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
)

// checkMatrix fails t where m breaks a rule every matrix keeps, however
// the runner computed it: rows refer to rows that exist, only built mutants
// and tests in the matrix have results, a skipped run never kills, and
// each verdict follows from the results alone.
func checkMatrix(t *testing.T, m *killmatrix.Matrix) {
	t.Helper()
	for _, err := range matrixErrors(m) {
		t.Error(err)
	}
}

func matrixErrors(m *killmatrix.Matrix) []error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	mutants := map[int]killmatrix.Mutant{}
	for i, mu := range m.Mutants {
		if mu.ID != i+1 {
			fail("mutant %d has id %d", i+1, mu.ID)
		}
		if mu.Schema && !mu.Compiled {
			fail("M%d ran in a schema binary but did not build", mu.ID)
		}
		if mu.BuildMS < 0 {
			fail("M%d: build_ms %v", mu.ID, mu.BuildMS)
		}
		mutants[mu.ID] = mu
	}
	tests := map[int]killmatrix.Test{}
	for i, tt := range m.Tests {
		if tt.ID != i+1 {
			fail("test %d has id %d", i+1, tt.ID)
		}
		tests[tt.ID] = tt
	}
	type pair struct{ mutant, test int }
	seen := map[pair]bool{}
	want := map[int]killmatrix.Verdict{}
	for _, mu := range m.Mutants {
		want[mu.ID] = killmatrix.VerdictNotCovered
		if !mu.Compiled {
			want[mu.ID] = killmatrix.VerdictNoBuild
		}
	}
	rank := map[killmatrix.Verdict]int{killmatrix.VerdictNotCovered: 0, killmatrix.VerdictSurvived: 1, killmatrix.VerdictTimeout: 2, killmatrix.VerdictKilled: 3}
	raise := func(id int, v killmatrix.Verdict) {
		if rank[v] > rank[want[id]] {
			want[id] = v
		}
	}
	for _, res := range m.Results {
		mu, okM := mutants[res.MutantID]
		tt, okT := tests[res.TestID]
		// A verdict reused from the cache comes back as one result with no
		// test: the run that found it is not in this matrix.
		if okM && res.TestID == 0 {
			if res.Skipped || seen[pair{mu.ID, 0}] {
				fail("M%d: cached result skipped or repeated", mu.ID)
			}
			seen[pair{mu.ID, 0}] = true
			raise(mu.ID, verdictOf(res.Status))
			continue
		}
		if !okM || !okT {
			fail("result for M%d T%d, which do not exist", res.MutantID, res.TestID)
			continue
		}
		at := fmt.Sprintf("M%d T%d", mu.ID, tt.ID)
		switch {
		case !mu.Compiled:
			fail("%s: result for a mutant that did not build", at)
		case tt.State != killmatrix.OK:
			fail("%s: result for a %s test", at, tt.State)
		case mu.Pkg != tt.Pkg:
			fail("%s: mutant of %s, test of %s", at, mu.Pkg, tt.Pkg)
		case res.Skipped && res.Status != killmatrix.Survived:
			fail("%s: skipped run is %s", at, res.Status)
		case res.MS < 0:
			fail("%s: ms %v", at, res.MS)
		case seen[pair{mu.ID, tt.ID}]:
			fail("%s: two results", at)
		}
		seen[pair{mu.ID, tt.ID}] = true
		if v := verdictOf(res.Status); v == "" {
			fail("%s: status %q", at, res.Status)
		} else if !res.Skipped {
			raise(mu.ID, v)
		}
	}
	got := m.Verdicts()
	if len(got) != len(m.Mutants) {
		fail("%d verdicts for %d mutants", len(got), len(m.Mutants))
	}
	for _, mu := range m.Mutants {
		if !mu.Compiled {
			want[mu.ID] = killmatrix.VerdictNoBuild
		}
		if got[mu.ID] != want[mu.ID] {
			fail("M%d: verdict %s, results say %s", mu.ID, got[mu.ID], want[mu.ID])
		}
	}
	return errs
}

// verdictOf is the verdict one run gives its mutant, "" for an unknown
// status. A flaky kill is no kill.
func verdictOf(s killmatrix.Status) killmatrix.Verdict {
	return map[killmatrix.Status]killmatrix.Verdict{
		killmatrix.Killed:    killmatrix.VerdictKilled,
		killmatrix.Timeout:   killmatrix.VerdictTimeout,
		killmatrix.Survived:  killmatrix.VerdictSurvived,
		killmatrix.FlakyKill: killmatrix.VerdictSurvived,
	}[s]
}

// TestStoredMatrices checks the matrices of real modules kept in docs:
// runs of earlier versions, under every option measured there.
func TestStoredMatrices(t *testing.T) {
	t.Parallel()
	dirs, err := filepath.Glob("../docs/data*/*/mutants.ndjson.gz")
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) < 40 {
		t.Fatalf("found %d stored matrices, want at least 40", len(dirs))
	}
	for _, f := range dirs {
		dir := filepath.Dir(f)
		t.Run(filepath.ToSlash(dir[len("../docs/"):]), func(t *testing.T) {
			t.Parallel()
			var m killmatrix.Matrix
			readRows(t, filepath.Join(dir, "mutants.ndjson.gz"), &m.Mutants)
			readRows(t, filepath.Join(dir, "tests.ndjson.gz"), &m.Tests)
			readRows(t, filepath.Join(dir, "results.ndjson.gz"), &m.Results)
			checkMatrix(t, &m)
		})
	}
}

func readRows[T any](t *testing.T, path string, rows *[]T) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	s := bufio.NewScanner(z)
	s.Buffer(nil, 1<<20)
	for s.Scan() {
		var row T
		if err := json.Unmarshal(s.Bytes(), &row); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		*rows = append(*rows, row)
	}
	if err := s.Err(); err != nil {
		t.Fatal(err)
	}
}

// run computes r's matrix and checks it.
func run(t *testing.T, r killmatrix.Runner, dir string, patterns ...string) *killmatrix.Matrix {
	t.Helper()
	m, err := r.Run(t.Context(), dir, patterns...)
	if err != nil {
		t.Fatal(err)
	}
	checkMatrix(t, m)
	return m
}
