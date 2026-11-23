package killmatrix_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ibrhmkoz/go-kit/mutant"
	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
	"github.com/ibrhmkoz/go-kit/xtesting"
)

func copyDiscount(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/disc\n\ngo 1.21\n")
	write("discount.go", `package disc

func Discount(price int, vip bool) int {
	if price > 100 && vip {
		return price - 20
	}
	return price
}
`)
	write("discount_test.go", `package disc

import "testing"

func TestVIPOver100(t *testing.T) {
	if got := Discount(120, true); got != 100 {
		t.Fatalf("got %d, want 100", got)
	}
}

func TestRegularOver100(t *testing.T) {
	if got := Discount(120, false); got != 120 {
		t.Fatalf("got %d, want 120", got)
	}
}

func TestVIPUnder100(t *testing.T) {
	if got := Discount(80, true); got != 80 {
		t.Fatalf("got %d, want 80", got)
	}
}
`)
	return dir
}

func formatVerdicts(m *killmatrix.Matrix) string {
	v := m.Verdicts()
	var b strings.Builder
	for _, mu := range m.Mutants {
		fmt.Fprintf(&b, "M%d %s\n", mu.ID, v[mu.ID])
	}
	return b.String()
}

// TestCacheReusesAndInvalidates verifies the four incremental-cache
// invariants: exact hit skips builds, adding a test re-runs only survivors,
// weakening an existing test invalidates the mutants it killed, and editing
// production code invalidates the whole package.
func TestCacheReusesAndInvalidates(t *testing.T) {
	t.Parallel()
	dir := copyDiscount(t)
	cacheDir := t.TempDir()
	r := killmatrix.Runner{
		Workers:      min(4, runtime.NumCPU()),
		BaselineRuns: 1,
		TimeoutPad:   500 * time.Millisecond,
		Schemata:     true,
		Select:       true,
		FailFast:     true,
		CacheDir:     cacheDir,
	}

	// 1. Cold run: M5 (price > 100 → price >= 100) survives, all others killed.
	m1 := run(t, r, dir, ".")
	wantInitial := `
		M1 killed
		M2 killed
		M3 killed
		M4 killed
		M5 survived
		M6 killed
		M7 killed
		M8 killed
		M9 killed
		M10 killed
		M11 killed
		M12 killed
	`
	xtesting.EqualText(t, formatVerdicts(m1), wantInitial)

	// 2. Exact hit: nothing changed, so every mutant reuses its cached verdict
	// with zero build time.
	m2 := run(t, r, dir, ".")
	xtesting.EqualText(t, formatVerdicts(m2), wantInitial)
	for _, mu := range m2.Mutants {
		if mu.BuildMS != 0 {
			t.Fatalf("M%d: BuildMS = %v on exact cache hit, want 0", mu.ID, mu.BuildMS)
		}
	}

	// 3. Level-2 hit: append a new test that kills M5. Previously killed
	// mutants stay cached (BuildMS == 0); only M5 is built and killed.
	testPath := filepath.Join(dir, "discount_test.go")
	origTest, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatal(err)
	}
	added := string(origTest) + `
func TestVIPAt100(t *testing.T) {
	if got := Discount(100, true); got != 100 {
		t.Fatalf("got %d, want 100", got)
	}
}
`
	if err := os.WriteFile(testPath, []byte(added), 0o600); err != nil {
		t.Fatal(err)
	}
	m3 := run(t, r, dir, ".")
	for _, mu := range m3.Mutants {
		if !mu.Compiled {
			t.Errorf("M%d: Compiled = false, want true", mu.ID)
		}
		if v := m3.Verdicts()[mu.ID]; v != killmatrix.VerdictKilled {
			t.Errorf("M%d: got %s after adding TestVIPAt100, want killed", mu.ID, v)
		}
		if mu.ID != 5 && mu.BuildMS != 0 {
			t.Errorf("M%d: BuildMS = %v, want 0 (should stay cached)", mu.ID, mu.BuildMS)
		}
		if mu.ID == 5 && mu.BuildMS == 0 {
			t.Errorf("M5: BuildMS = 0, want > 0 (former survivor must be re-tested)")
		}
	}

	// 3b. All-reused Level-2 fast path: adding a no-op test when every mutant
	// is already killed skips building completely and updates the L1 cache.
	withExtra := added + "\nfunc TestExtra(t *testing.T) {}\n"
	if err := os.WriteFile(testPath, []byte(withExtra), 0o600); err != nil {
		t.Fatal(err)
	}
	m3b := run(t, r, dir, ".")
	for _, mu := range m3b.Mutants {
		if v := m3b.Verdicts()[mu.ID]; v != killmatrix.VerdictKilled {
			t.Errorf("M%d: got %s on all-reused L2 hit, want killed", mu.ID, v)
		}
		if mu.BuildMS != 0 {
			t.Errorf("M%d: BuildMS = %v on all-reused L2 hit, want 0", mu.ID, mu.BuildMS)
		}
	}
	// Verify 3b persisted the updated L1 snapshot (containing TestExtra).
	entries, err := os.ReadDir(cacheDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("cacheDir entries = %d, err = %v; want 1", len(entries), err)
	}
	raw, err := os.ReadFile(filepath.Join(cacheDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "TestExtra") {
		t.Fatal("all-reused L2 hit did not update cached snapshot with TestExtra")
	}

	// 4. Weakening an existing test invalidates the mutants it killed, while
	// mutants killed by unchanged tests stay cached (verifies reusedKillers).
	weakened := strings.Replace(withExtra, `if got := Discount(120, true); got != 100`, `if got := Discount(120, true); got != got`, 1)
	if err := os.WriteFile(testPath, []byte(weakened), 0o600); err != nil {
		t.Fatal(err)
	}
	m4 := run(t, r, dir, ".")
	if v := m4.Verdicts()[9]; v != killmatrix.VerdictSurvived {
		t.Fatalf("M9 (price - 20 → price + 20): got %s after weakening TestVIPOver100, want survived", v)
	}
	if m4.Mutants[0].BuildMS != 0 {
		t.Fatalf("M1 (killed by unchanged TestRegularOver100): BuildMS = %v, want 0", m4.Mutants[0].BuildMS)
	}

	// 5. Editing production code invalidates the whole package.
	prodPath := filepath.Join(dir, "discount.go")
	prod, err := os.ReadFile(prodPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prodPath, []byte(strings.Replace(string(prod), "100", "200", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	m5 := run(t, r, dir, ".")
	for _, mu := range m5.Mutants {
		if mu.BuildMS == 0 {
			t.Fatalf("M%d: BuildMS = 0 after production edit, want > 0", mu.ID)
		}
	}

	// 6. Full-matrix mode (FailFast=false) must neither read nor write CacheDir.
	full := r
	full.FailFast = false
	full.CacheDir = t.TempDir()
	run(t, full, dir, ".")
	if entries, _ := os.ReadDir(full.CacheDir); len(entries) != 0 {
		t.Fatalf("FailFast=false wrote %d cache files, want 0", len(entries))
	}
	m6 := run(t, full, dir, ".")
	if len(m6.Tests) == 0 || m6.Mutants[0].BuildMS == 0 {
		t.Fatalf("FailFast=false should bypass cache: tests=%d buildMS=%v", len(m6.Tests), m6.Mutants[0].BuildMS)
	}
}

// TestCacheInvalidatesOnRunner: the cache misses when the runner options
// that change verdicts change, and misses, rather than fails, on an entry
// it cannot read.
// Which edits to a package change its fingerprint is fingerprint's test.
func TestCacheInvalidatesOnRunner(t *testing.T) {
	t.Parallel()
	dir := copyDiscount(t)
	r := killmatrix.Runner{
		Workers:      2,
		BaselineRuns: 1,
		TimeoutPad:   500 * time.Millisecond,
		Schemata:     true,
		Select:       true,
		FailFast:     true,
		CacheDir:     t.TempDir(),
	}
	mustMiss := func(label string, rr killmatrix.Runner) {
		t.Helper()
		m := run(t, rr, dir, ".")
		if m.Mutants[0].BuildMS == 0 {
			t.Fatalf("%s: cache hit", label)
		}
	}
	mustMiss("cold", r)

	confirm := r
	confirm.Confirm = true
	mustMiss("runner option change", confirm)
	infect := confirm
	infect.Infect = true
	mustMiss("infection filter", infect)

	entries, err := os.ReadDir(r.CacheDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("cache entries = %d, err = %v", len(entries), err)
	}
	if err := os.WriteFile(filepath.Join(r.CacheDir, entries[0].Name()), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustMiss("corrupt cache", infect)
}

// TestCacheChoosesMore: mutants chosen after the cache was filled run;
// none of them is reported from an entry that never held it.
func TestCacheChoosesMore(t *testing.T) {
	t.Parallel()
	dir := copyDiscount(t)
	r := killmatrix.Runner{
		Workers:      2,
		BaselineRuns: 1,
		TimeoutPad:   500 * time.Millisecond,
		Schemata:     true,
		Select:       true,
		FailFast:     true,
	}
	fresh := run(t, r, dir, ".")
	r.CacheDir = t.TempDir()
	few := r
	few.Choose = func(_ string, _ []mutant.Mutant) []int { return []int{0} }
	run(t, few, dir, ".")
	all := run(t, r, dir, ".")
	xtesting.EqualText(t, formatVerdicts(all), formatVerdicts(fresh))
}

// TestCacheKeepsVerdicts: an exact hit gives back every verdict the cold
// run gave, timeouts and build errors as well as kills and survivors,
// without building anything.
func TestCacheKeepsVerdicts(t *testing.T) {
	t.Parallel()
	r := killmatrix.Runner{Workers: min(4, runtime.NumCPU()), BaselineRuns: 1, TimeoutPad: 500 * time.Millisecond, Schemata: true, Select: true, FailFast: true, CacheDir: t.TempDir()}
	cold := run(t, r, "testdata", "./discount", "./loop", "./table")
	warm := run(t, r, "testdata", "./discount", "./loop", "./table")
	xtesting.EqualText(t, formatVerdicts(warm), formatVerdicts(cold))
	for _, mu := range warm.Mutants {
		if mu.BuildMS != 0 {
			t.Errorf("M%d built on an exact hit", mu.ID)
		}
	}
}
