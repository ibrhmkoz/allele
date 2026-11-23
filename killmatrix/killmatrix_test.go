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

// TestRun computes the fixture's matrix with a build per mutant and with
// schemata. Both must print the same: a schema binary that switches a
// mutant on by its id must behave as the mutant built on its own.
func TestRun(t *testing.T) {
	for _, mode := range []struct{ schemata, sel bool }{{false, false}, {true, false}, {true, true}} {
		schemata := mode.schemata
		t.Run(fmt.Sprintf("schemata=%v,select=%v", mode.schemata, mode.sel), func(t *testing.T) {
			t.Parallel()
			// A full second of pad: under ci load, a fast test's process can
			// take hundreds of milliseconds to start, and a run that times
			// out reads T instead of K or a dot.
			r := killmatrix.Runner{Workers: runtime.NumCPU(), TimeoutPad: time.Second, Schemata: schemata, Select: mode.sel}
			m, err := r.Run(t.Context(), "testdata", "./discount", "./loop", "./table")
			if err != nil {
				t.Fatal(err)
			}
			xtesting.Prints(t, m, fixtureMatrix)
			if !schemata {
				return
			}
			// M22 returns nil as a concrete type; _mutErr returns error.
			var own []string
			for _, mu := range m.Mutants {
				if !mu.Schema {
					own = append(own, fmt.Sprint("M", mu.ID))
				}
			}
			xtesting.EqualText(t, strings.Join(own, " "), "M22")
			if !mode.sel {
				return
			}
			// TestCount never calls Last and TestLast never calls Count.
			// Every discount test calls Discount, so all run against its
			// mutants, even where a branch such as price - 20 does not run.
			skipped := map[string]int{}
			for _, res := range m.Results {
				if res.Skipped {
					skipped[m.Tests[res.TestID-1].Name]++
				}
			}
			xtesting.EqualText(t, fmt.Sprint(skipped), "map[TestCount:1 TestLast:8]")
		})
	}
}

// M5 is the boundary test the suite lacks (price == 100), M18 is
// equivalent for n >= 0 and M14 loops forever. M21 would leave x unused,
// so it keeps it used with _ = x; M22 returns nil as a concrete type.
// Each subtest of TestAbs is a column; where index panics, the subtests
// it hides count as killed too (M24, M26-M28).
const fixtureMatrix = `
		fixture/discount
		  T1 TestVIPOver100
		  T2 TestRegularOver100
		  T3 TestVIPUnder100

		      T1 T2 T3
		  M1  .  K  K   discount.go:5:5 COR price > 100 && vip → true
		  M2  K  .  .   discount.go:5:5 COR price > 100 && vip → false
		  M3  K  .  K   discount.go:5:11 ROR price > 100 → price < 100
		  M4  K  .  K   discount.go:5:11 ROR price > 100 → price <= 100
		  M5  .  .  .   discount.go:5:11 ROR price > 100 → price >= 100
		  M6  K  .  .   discount.go:5:11 ROR price > 100 → price == 100
		  M7  .  .  K   discount.go:5:11 ROR price > 100 → price != 100
		  M8  .  K  K   discount.go:5:17 COR price > 100 && vip → price > 100 || vip
		  M9  K  .  .   discount.go:6:16 AOR price - 20 → price + 20
		  M10 K  .  .   discount.go:6:16 AOR price - 20 → price * 20
		  M11 K  .  .   discount.go:6:16 AOR price - 20 → price / 20
		  M12 K  .  .   discount.go:6:16 AOR price - 20 → price % 20

		fixture/loop
		  T4 TestCount
		  T5 TestLast
		  T6 TestBroken (failing)

		      T4 T5
		  M13 K  .   loop.go:10:6 COR i >= n → true
		  M14 T  .   loop.go:10:6 COR i >= n → false
		  M15 K  .   loop.go:10:8 ROR i >= n → i < n
		  M16 K  .   loop.go:10:8 ROR i >= n → i <= n
		  M17 K  .   loop.go:10:8 ROR i >= n → i > n
		  M18 .  .   loop.go:10:8 ROR i >= n → i == n
		  M19 K  .   loop.go:10:8 ROR i >= n → i != n
		  M20 K  .   loop.go:13:3 SDL c++ → (deleted)
		  M21 .  K   loop.go:20:3 SDL last = x → _ = x
		  M22 B  B   loop.go:32:9 ERR return code(strconv.Itoa(n)) → return func() error { _ = strconv.Itoa; return nil }()

		fixture/table
		  T7 TestAbs/index
		  T8 TestAbs/negative
		  T9 TestAbs/positive
		  T10 TestAbs/zero

		      T7 T8 T9 T10
		  M23 .  .  K  .    table.go:6:5 COR x < 0 → true
		  M24 K  K  K  K    table.go:6:5 COR x < 0 → false
		  M25 .  .  .  .    table.go:6:7 ROR x < 0 → x <= 0
		  M26 K  K  K  K    table.go:6:7 ROR x < 0 → x > 0
		  M27 K  K  K  K    table.go:6:7 ROR x < 0 → x >= 0
		  M28 K  K  K  K    table.go:6:7 ROR x < 0 → x == 0
		  M29 .  .  K  .    table.go:6:7 ROR x < 0 → x != 0
`

// TestUnstableNames checks that a leaf whose name changes between runs is
// flaky, gets no results, and that no parent test becomes a column.
func TestUnstableNames(t *testing.T) {
	t.Parallel()
	r := killmatrix.Runner{Workers: runtime.NumCPU(), TimeoutPad: time.Second}
	m, err := r.Run(t.Context(), "testdata", "./unstable")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	ok := map[int]bool{}
	for _, tt := range m.Tests {
		name := tt.Name
		if strings.HasPrefix(name, "TestClockName/") {
			name = "TestClockName/<clock>"
		}
		got = append(got, fmt.Sprintf("%s %s", name, tt.State))
		ok[tt.ID] = tt.State == killmatrix.OK
	}
	xtesting.EqualText(t, strings.Join(got, "\n"), `
		TestClockName/<clock> flaky
		TestClockName/<clock> flaky
		TestClockName/<clock> flaky
		TestDouble ok
	`)
	for _, res := range m.Results {
		if !ok[res.TestID] {
			t.Errorf("result for flaky test T%d", res.TestID)
		}
	}
}

// TestFailFast runs the fixture the way a developer would: schemata, test
// selection, failing fast, and one mutant per line. Each mutant kept must
// get the verdict the full matrix gives it.
func TestFailFast(t *testing.T) {
	t.Parallel()
	firstPerLine := func(_ string, ms []mutant.Mutant) []int {
		var keep []int
		for i, m := range ms {
			if i == 0 || m.Pos.Line != ms[i-1].Pos.Line {
				keep = append(keep, i)
			}
		}
		return keep
	}
	r := killmatrix.Runner{Workers: runtime.NumCPU(), TimeoutPad: time.Second, Schemata: true, Select: true, FailFast: true, Choose: firstPerLine}
	m, err := r.Run(t.Context(), "testdata", "./discount", "./loop", "./table")
	if err != nil {
		t.Fatal(err)
	}
	verdicts := m.Verdicts()
	var b strings.Builder
	for _, mu := range m.Mutants {
		fmt.Fprintf(&b, "%s:%d %s %s → %s: %s\n", mu.File, mu.Line, mu.Operator, mu.Original, mu.Mutated, verdicts[mu.ID])
	}
	// One mutant per line: the if condition's true, the first COR on each
	// line, the first statement deleted. Verdicts match fixtureMatrix's
	// M1, M9, M13, M20, M21, M22 and M23.
	xtesting.EqualText(t, b.String(), `
		discount.go:5 COR price > 100 && vip → true: killed
		discount.go:6 AOR price - 20 → price + 20: killed
		loop.go:10 COR i >= n → true: killed
		loop.go:13 SDL c++ → : killed
		loop.go:20 SDL last = x → _ = x: killed
		loop.go:32 ERR return code(strconv.Itoa(n)) → return func() error { _ = strconv.Itoa; return nil }(): build error
		table.go:6 COR x < 0 → true: killed
	`)
	ran := 0
	for _, res := range m.Results {
		if !res.Skipped {
			ran++
		}
	}
	// The full matrix runs 7 mutants × their package's tests: 3+3+2+2+2+0+4.
	// Failing fast stops each at its first kill.
	if ran >= 16 {
		t.Errorf("%d runs recorded; failing fast should stop at the first kill", ran)
	}
}

// TestFailFastVerdicts checks that FailFast (with schemata and selection)
// reproduces every verdict of fixtureMatrix across all 29 fixture mutants:
// kills, survivors, timeouts, panicking subtests, and build errors.
// TestChooseNone: a package none of whose mutants is chosen is neither built
// nor run, so that mutant -diff costs nothing in packages the diff misses.
func TestChooseNone(t *testing.T) {
	t.Parallel()
	onlyLoop := func(file string, _ []mutant.Mutant) []int {
		if filepath.Base(file) != "loop.go" {
			return nil
		}
		return []int{0}
	}
	r := killmatrix.Runner{Workers: runtime.NumCPU(), TimeoutPad: time.Second, Schemata: true, Select: true, FailFast: true, Choose: onlyLoop}
	m, err := r.Run(t.Context(), "testdata", "./discount", "./loop", "./table")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, mu := range m.Mutants {
		fmt.Fprintf(&b, "mutant %s:%d %s\n", mu.File, mu.Line, mu.Operator)
	}
	for _, tt := range m.Tests {
		fmt.Fprintf(&b, "test %s %s\n", filepath.Base(tt.Pkg), tt.Name)
	}
	xtesting.EqualText(t, b.String(), `
		mutant loop.go:10 COR
		test loop TestCount
		test loop TestLast
		test loop TestBroken
	`)
}

func TestFailFastVerdicts(t *testing.T) {
	t.Parallel()
	fast, err := (killmatrix.Runner{
		Workers:    runtime.NumCPU(),
		TimeoutPad: time.Second,
		Schemata:   true,
		Select:     true,
		FailFast:   true,
	}).Run(t.Context(), "testdata", "./discount", "./loop", "./table")
	if err != nil {
		t.Fatal(err)
	}
	got := fast.Verdicts()
	var lines strings.Builder
	for _, mu := range fast.Mutants {
		fmt.Fprintf(&lines, "M%d %s\n", mu.ID, got[mu.ID])
	}
	// Matches fixtureMatrix: M5, M18, M25 survive; M14 times out; M22 does not build; the rest are killed.
	xtesting.EqualText(t, lines.String(), `
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
		M13 killed
		M14 timeout
		M15 killed
		M16 killed
		M17 killed
		M18 survived
		M19 killed
		M20 killed
		M21 killed
		M22 build error
		M23 killed
		M24 killed
		M25 survived
		M26 killed
		M27 killed
		M28 killed
		M29 killed
	`)
}

// TestConfirm runs a test that fails against a mutant on the first try
// only. Without Confirm it reads as a kill; with it, as flaky, which no
// verdict counts as killed.
func TestConfirm(t *testing.T) {
	t.Parallel()
	// Sequential subtests: testdata/flaky keys its marker files by ppid.
	for _, failFast := range []bool{false, true} {
		t.Run(fmt.Sprintf("failfast=%v", failFast), func(t *testing.T) {
			r := killmatrix.Runner{Workers: runtime.NumCPU(), TimeoutPad: time.Second, Schemata: true, Select: failFast, FailFast: failFast, Confirm: true}
			m, err := r.Run(t.Context(), "testdata", "./flaky")
			if err != nil {
				t.Fatal(err)
			}
			xtesting.Prints(t, m, `
				fixture/flaky
				  T1 TestPick

				     T1
				  M1 .   flaky.go:7:5 COR n > 0 → true
				  M2 F   flaky.go:7:5 COR n > 0 → false
				  M3 F   flaky.go:7:7 ROR n > 0 → n < 0
				  M4 F   flaky.go:7:7 ROR n > 0 → n <= 0
				  M5 .   flaky.go:7:7 ROR n > 0 → n >= 0
				  M6 F   flaky.go:7:7 ROR n > 0 → n == 0
				  M7 .   flaky.go:7:7 ROR n > 0 → n != 0
			`)
			for id, v := range m.Verdicts() {
				if v == killmatrix.VerdictKilled {
					t.Errorf("M%d: a flaky kill counted as killed", id)
				}
			}
		})
	}
}

// TestConfirmUnderLoad runs a test that, once a mutant breaks it, also
// fails against the original code until the original runs: a loaded
// machine. Confirm cannot tell, so it checks those kills again at the
// end, alone, and finds them real.
func TestConfirmUnderLoad(t *testing.T) {
	t.Parallel()
	clean := func() {
		markers, _ := filepath.Glob(filepath.Join(os.TempDir(), fmt.Sprintf("killmatrix-load-%d*", os.Getpid())))
		for _, m := range markers {
			_ = os.Remove(m)
		}
	}
	t.Cleanup(clean)
	// Sequential subtests: testdata/load keys its marker files by ppid.
	for _, failFast := range []bool{false, true} {
		t.Run(fmt.Sprintf("failfast=%v", failFast), func(t *testing.T) {
			clean()
			r := killmatrix.Runner{Workers: 1, TimeoutPad: time.Second, Schemata: true, Select: failFast, FailFast: failFast, Confirm: true}
			m, err := r.Run(t.Context(), "testdata", "./load")
			if err != nil {
				t.Fatal(err)
			}
			xtesting.Prints(t, m, `
				fixture/load
				  T1 TestPick

				     T1
				  M1 .   load.go:8:5 COR n > 0 → true
				  M2 K   load.go:8:5 COR n > 0 → false
				  M3 K   load.go:8:7 ROR n > 0 → n < 0
				  M4 K   load.go:8:7 ROR n > 0 → n <= 0
				  M5 .   load.go:8:7 ROR n > 0 → n >= 0
				  M6 K   load.go:8:7 ROR n > 0 → n == 0
				  M7 .   load.go:8:7 ROR n > 0 → n != 0
			`)
		})
	}
}
