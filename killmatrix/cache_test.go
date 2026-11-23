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
	m1, err := r.Run(t.Context(), dir, ".")
	if err != nil {
		t.Fatal(err)
	}
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
	m2, err := r.Run(t.Context(), dir, ".")
	if err != nil {
		t.Fatal(err)
	}
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
	m3, err := r.Run(t.Context(), dir, ".")
	if err != nil {
		t.Fatal(err)
	}
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
	m3b, err := r.Run(t.Context(), dir, ".")
	if err != nil {
		t.Fatal(err)
	}
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
	m4, err := r.Run(t.Context(), dir, ".")
	if err != nil {
		t.Fatal(err)
	}
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
	m5, err := r.Run(t.Context(), dir, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, mu := range m5.Mutants {
		if mu.BuildMS == 0 {
			t.Fatalf("M%d: BuildMS = 0 after production edit, want > 0", mu.ID)
		}
	}

	// 6. Full-matrix mode (FailFast=false) must neither read nor write CacheDir.
	full := r
	full.FailFast = false
	full.CacheDir = t.TempDir()
	if _, err := full.Run(t.Context(), dir, "."); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(full.CacheDir); len(entries) != 0 {
		t.Fatalf("FailFast=false wrote %d cache files, want 0", len(entries))
	}
	m6, err := full.Run(t.Context(), dir, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(m6.Tests) == 0 || m6.Mutants[0].BuildMS == 0 {
		t.Fatalf("FailFast=false should bypass cache: tests=%d buildMS=%v", len(m6.Tests), m6.Mutants[0].BuildMS)
	}
}

// TestCacheInvalidatesOnDepEdit checks that editing a local dependency
// invalidates the dependent package's cached verdicts.
func TestCacheInvalidatesOnDepEdit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/depcache\n\ngo 1.21\n")
	write("dep/dep.go", "package dep\nfunc Threshold() int { return 10 }\n")
	write("app/app.go", "package app\nimport \"example.com/depcache/dep\"\nfunc Big(n int) bool { return n > dep.Threshold() }\n")
	write("app/app_test.go", "package app\nimport \"testing\"\nfunc TestBig(t *testing.T) { if !Big(15) { t.Fatal() } }\n")

	r := killmatrix.Runner{
		Workers:      2,
		BaselineRuns: 1,
		TimeoutPad:   500 * time.Millisecond,
		Schemata:     true,
		Select:       true,
		FailFast:     true,
		CacheDir:     t.TempDir(),
	}
	if _, err := r.Run(t.Context(), dir, "./app"); err != nil {
		t.Fatal(err)
	}
	// Changing dep.Threshold() from 10 to 12 and back to 10 keeps TestBig
	// passing while covering both hash orderings (old < new and old > new).
	for _, v := range []string{"12", "10"} {
		write("dep/dep.go", "package dep\nfunc Threshold() int { return "+v+" }\n")
		m, err := r.Run(t.Context(), dir, "./app")
		if err != nil {
			t.Fatal(err)
		}
		if m.Mutants[0].BuildMS == 0 {
			t.Fatalf("cache hit after dependency edit to %s; want cache miss", v)
		}
	}
}

// TestCacheInvalidatesOnTestDepEdit checks that editing a test-only
// dependency (like xtesting in this repo) invalidates cached verdicts of
// packages whose tests import it.
func TestCacheInvalidatesOnTestDepEdit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/testdep\n\ngo 1.21\n")
	write("assert/assert.go", "package assert\nimport \"testing\"\nfunc True(t *testing.T, ok bool) { if !ok { t.Fatal(\"want true\") } }\n")
	write("app/app.go", "package app\nfunc Pos(n int) bool { return n > 0 }\n")
	write("app/app_test.go", "package app\nimport (\n\t\"testing\"\n\t\"example.com/testdep/assert\"\n)\nfunc TestPos(t *testing.T) { assert.True(t, Pos(1)) }\n")

	r := killmatrix.Runner{
		Workers:      2,
		BaselineRuns: 1,
		TimeoutPad:   500 * time.Millisecond,
		Schemata:     true,
		Select:       true,
		FailFast:     true,
		CacheDir:     t.TempDir(),
	}
	m1, err := r.Run(t.Context(), dir, "./app")
	if err != nil {
		t.Fatal(err)
	}
	if m1.Verdicts()[1] != killmatrix.VerdictKilled {
		t.Fatalf("cold run: M1 = %s, want killed", m1.Verdicts()[1])
	}
	// Break the test-only assertion helper so nothing is checked anymore.
	write("assert/assert.go", "package assert\nimport \"testing\"\nfunc True(t *testing.T, ok bool) {}\n")
	m2, err := r.Run(t.Context(), dir, "./app")
	if err != nil {
		t.Fatal(err)
	}
	if m2.Mutants[0].BuildMS == 0 {
		t.Fatal("cache hit after test-only dependency edit; want cache miss")
	}
	if m2.Verdicts()[1] != killmatrix.VerdictSurvived {
		t.Fatalf("after breaking assert.True: M1 = %s, want survived", m2.Verdicts()[1])
	}
}

// TestCacheInvalidatesOnInputs checks that edits to testdata/, //go:embed
// files, shared test helpers, runner options, and Choose subsets all
// invalidate the cache.
func TestCacheInvalidatesOnInputs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/inputs\n\ngo 1.21\n")
	write("msg.txt", "ok")
	write("testdata/want.txt", "ok")
	write("p.go", `package p
import _ "embed"
//go:embed msg.txt
var msg string
func Check(n int) string {
	if n > 0 {
		return msg
	}
	return ""
}
`)
	write("p_test.go", `package p
import (
	"os"
	"testing"
)
func want(t *testing.T) string {
	b, err := os.ReadFile("testdata/want.txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func TestCheck(t *testing.T) {
	if got := Check(1); got != want(t) {
		t.Fatalf("got %q, want %q", got, want(t))
	}
}
`)

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
		m, err := rr.Run(t.Context(), dir, ".")
		if err != nil {
			t.Fatal(err)
		}
		if m.Mutants[0].BuildMS == 0 {
			t.Fatalf("%s: unexpected cache hit", label)
		}
	}
	mustMiss("cold", r)

	// 1. //go:embed file edit alone
	write("msg.txt", "ok!")
	mustMiss("embed edit", r)

	// 2. testdata/ file edit alone (warm cache first on current msg.txt)
	write("testdata/extra.txt", "x")
	mustMiss("testdata edit", r)

	// 3. Shared helper edit in *_test.go
	write("p_test.go", `package p
import (
	"os"
	"testing"
)
func want(t *testing.T) string {
	b, err := os.ReadFile("testdata/want.txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + ""
}
func TestCheck(t *testing.T) {
	if got := Check(1); got != want(t) {
		t.Fatalf("got %q, want %q", got, want(t))
	}
}
`)
	mustMiss("test helper edit", r)

	// 4. Runner option change (Confirm)
	rConfirm := r
	rConfirm.Confirm = true
	mustMiss("runner option change", rConfirm)

	// 5. Choose subset change (same runner options as rConfirm)
	rSub := rConfirm
	rSub.Choose = func(_ string, _ []mutant.Mutant) []int { return []int{0} }
	mustMiss("Choose subset change", rSub)

	// 6. Corrupt cache JSON is treated as a cache miss rather than an error.
	entries, err := os.ReadDir(rSub.CacheDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("cache entries = %d, err = %v", len(entries), err)
	}
	if err := os.WriteFile(filepath.Join(rSub.CacheDir, entries[0].Name()), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustMiss("corrupt cache", rSub)
}
