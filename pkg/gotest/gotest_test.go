package gotest_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ibrhmkoz/allele/pkg/gotest"
	"github.com/ibrhmkoz/allele/pkg/xtesting"
)

// fixture builds a test binary whose tests pass, fail, skip, panic or
// hang, as the environment asks.
func fixture(t *testing.T) (dir, bin string) {
	t.Helper()
	dir = t.TempDir()
	for name, src := range map[string]string{
		"go.mod": "module fixture\n\ngo 1.21\n",
		"fixture_test.go": `package fixture

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain needs the package's directory, as fixtures do, even to list.
func TestMain(m *testing.M) {
	if _, err := os.Stat("fixture_test.go"); err != nil {
		os.Exit(3)
	}
	os.Exit(m.Run())
}

// TestLong prints a line longer than a default bufio.Scanner reads, then
// a result that must still count.
func TestLong(t *testing.T) {
	t.Log(strings.Repeat("x", 100_000))
	t.Run("after", func(t *testing.T) {})
}

func TestOne(t *testing.T) {
	t.Run("passes", func(t *testing.T) {})
	t.Run("fails", func(t *testing.T) { t.Fatal("no") })
	t.Run("skips", func(t *testing.T) { t.Skip("later") })
}

func TestEnv(t *testing.T) {
	if os.Getenv("FIXTURE") != "on" {
		t.Fatal("FIXTURE unset")
	}
	if _, err := os.Stat("fixture_test.go"); err != nil {
		t.Fatal("not run in the package's directory")
	}
}

func TestPanics(t *testing.T) {
	t.Run("first", func(t *testing.T) { panic("boom") })
	t.Run("never", func(t *testing.T) {})
}

func TestHangs(t *testing.T) { time.Sleep(time.Minute) }

func BenchmarkNothing(b *testing.B) {}
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bin = filepath.Join(dir, "fixture.test")
	if out, err := gotest.Build(t.Context(), dir, bin, ""); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return dir, bin
}

// show prints a report: the run, then each leaf it was asked about.
func show(r gotest.Report, leaves ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ok %v, timed out %v, reported %v\n", r.OK, r.TimedOut, r.Reported())
	for _, l := range leaves {
		passed, reported := r.Passed(l)
		fmt.Fprintf(&b, "  %-18s %-7s started %-5v reported %-5v passed %v\n", l, r.Outcome(l), r.Started(l), reported, passed)
	}
	return b.String()
}

func TestRun(t *testing.T) {
	t.Parallel()
	dir, bin := fixture(t)
	ctx := t.Context()

	names, err := gotest.List(ctx, dir, bin)
	if err != nil {
		t.Fatal(err)
	}
	xtesting.EqualText(t, fmt.Sprint(names), "[TestLong TestOne TestEnv TestPanics TestHangs]")

	// A long line does not stop the reading.
	r := gotest.Run(ctx, bin, gotest.Options{Dir: dir, Tests: []string{"TestLong"}})
	xtesting.EqualText(t, show(r, "TestLong/after"), `
		ok true, timed out false, reported [TestLong/after TestLong]
		  TestLong/after     pass    started true  reported true  passed true
	`)

	// A skipped subtest passed: it did not fail.
	r = gotest.Run(ctx, bin, gotest.Options{Dir: dir, Tests: []string{"TestOne"}})
	xtesting.EqualText(t, show(r, "TestOne/passes", "TestOne/fails", "TestOne/skips"), `
		ok false, timed out false, reported [TestOne/passes TestOne/fails TestOne/skips TestOne]
		  TestOne/passes     pass    started true  reported true  passed true
		  TestOne/fails      fail    started true  reported true  passed false
		  TestOne/skips      pass    started true  reported true  passed true
	`)

	// Skip leaves the failing subtest out.
	r = gotest.Run(ctx, bin, gotest.Options{Dir: dir, Tests: []string{"TestOne"}, Skip: "TestOne/fails"})
	xtesting.EqualText(t, show(r), "ok true, timed out false, reported [TestOne/passes TestOne/skips TestOne]\n")

	// The environment and the directory reach the test.
	r = gotest.Run(ctx, bin, gotest.Options{Dir: dir, Tests: []string{"TestEnv"}, Env: []string{"FIXTURE=on"}})
	xtesting.EqualText(t, show(r, "TestEnv"), `
		ok true, timed out false, reported [TestEnv]
		  TestEnv            pass    started true  reported true  passed true
	`)

	// A panic ends the binary once the panicking subtest has reported:
	// its sibling never starts, and unreported, takes the run's outcome.
	r = gotest.Run(ctx, bin, gotest.Options{Dir: dir, Tests: []string{"TestPanics"}})
	xtesting.EqualText(t, show(r, "TestPanics/first", "TestPanics/never"), `
		ok false, timed out false, reported [TestPanics/first TestPanics]
		  TestPanics/first   fail    started true  reported true  passed false
		  TestPanics/never   fail    started false reported false passed false
	`)

	// Tests run in source order, whatever order Tests gives. FailFast
	// stops at the first failure, TestOne/fails: TestOne/skips and TestEnv
	// never start. Without it, every test runs.
	both := []string{"TestEnv", "TestOne"}
	r = gotest.Run(ctx, bin, gotest.Options{Dir: dir, Tests: both, FailFast: true})
	xtesting.EqualText(t, show(r, "TestEnv"), `
		ok false, timed out false, reported [TestOne/passes TestOne/fails TestOne]
		  TestEnv            fail    started false reported false passed false
	`)
	r = gotest.Run(ctx, bin, gotest.Options{Dir: dir, Tests: both})
	xtesting.EqualText(t, show(r, "TestEnv"), `
		ok false, timed out false, reported [TestOne/passes TestOne/fails TestOne/skips TestOne TestEnv]
		  TestEnv            fail    started true  reported true  passed false
	`)

	// A hanging test is stopped at the limit.
	start := time.Now()
	r = gotest.Run(ctx, bin, gotest.Options{Dir: dir, Tests: []string{"TestHangs"}, Limit: 300 * time.Millisecond})
	xtesting.EqualText(t, show(r, "TestHangs"), `
		ok false, timed out true, reported []
		  TestHangs          timeout started true  reported false passed false
	`)
	if d := time.Since(start); d > 5*time.Second || r.Elapsed < 300*time.Millisecond {
		t.Errorf("ran %v, reported %v; want stopped near 300ms", d, r.Elapsed)
	}

	// A deadline the caller's context brought is not Run's timeout.
	short, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	r = gotest.Run(short, bin, gotest.Options{Dir: dir, Tests: []string{"TestHangs"}})
	xtesting.EqualText(t, show(r), "ok false, timed out false, reported []\n")
}

// TestBuildOverlay: an overlay replaces a file for the build only.
func TestBuildOverlay(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module over\n\ngo 1.21\n")
	write("v.go", "package over\n\nfunc V() int { return 1 }\n")
	write("v_test.go", "package over\n\nimport \"testing\"\n\nfunc TestV(t *testing.T) {\n\tif V() != 2 {\n\t\tt.Fatal(V())\n\t}\n}\n")
	elsewhere := t.TempDir()
	two := filepath.Join(elsewhere, "two.go")
	if err := os.WriteFile(two, []byte("package over\n\nfunc V() int { return 2 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	overlay := filepath.Join(elsewhere, "overlay.json")
	if err := os.WriteFile(overlay, []byte(`{"Replace": {"`+filepath.Join(dir, "v.go")+`": "`+two+`"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ok := func(overlay string) bool {
		t.Helper()
		bin := filepath.Join(t.TempDir(), "over.test")
		if out, err := gotest.Build(t.Context(), dir, bin, overlay); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		return gotest.Run(t.Context(), bin, gotest.Options{Dir: dir, Tests: []string{"TestV"}}).OK
	}
	if ok("") || !ok(overlay) {
		t.Error("the overlay did not replace v.go")
	}
}

func TestNames(t *testing.T) {
	names := []string{"TestA", "TestA/x", "TestA/x/deep", "TestA/y", "TestB"}
	xtesting.EqualText(t, fmt.Sprint(gotest.Leaves(names), " ", gotest.Top("TestA/x/deep"), " ", gotest.Top("TestB")), "[TestA/x/deep TestA/y TestB] TestA TestB")
}

func TestBuildFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module broken\n\ngo 1.21\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b_test.go"), []byte("package broken\n\nfunc x() { undefined() }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := gotest.Build(t.Context(), dir, filepath.Join(dir, "b.test"), ""); err == nil || !strings.Contains(string(out), "undefined") {
		t.Errorf("a broken package built: %v %s", err, out)
	}
	if _, err := gotest.List(t.Context(), dir, filepath.Join(dir, "missing.test")); err == nil {
		t.Error("listing a missing binary gave no error")
	}
}
