package killmatrix_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ibrhmkoz/allele/internal/killmatrix"
	"github.com/ibrhmkoz/allele/internal/mutant"
	"github.com/ibrhmkoz/allele/pkg/xtesting"
)

// TestRun computes the fixture's matrix with a build per mutant and with
// schemata. Both must print the same: a schema binary that switches a
// mutant on by its id must behave as the mutant built on its own.
func TestRun(t *testing.T) {
	for _, mode := range []struct{ schemata, sel bool }{{false, false}, {true, false}, {true, true}} {
		schemata := mode.schemata
		t.Run(fmt.Sprintf("schemata=%v,select=%v", mode.schemata, mode.sel), func(t *testing.T) {
			t.Parallel()
			// 500 ms pad is 100× the 5 ms fixture tests; BaselineRuns=1 skips
			// redundant baseline repeats on these deterministic fixtures.
			r := killmatrix.Runner{Workers: min(4, runtime.NumCPU()), BaselineRuns: 1, TimeoutPad: 500 * time.Millisecond, Schemata: schemata, Select: mode.sel}
			m := run(t, r, "testdata", "./discount", "./loop", "./table")
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

func TestUnstableNames(t *testing.T) {
	t.Parallel()
	r := killmatrix.Runner{Workers: runtime.NumCPU(), TimeoutPad: time.Second}
	m := run(t, r, "testdata", "./unstable")
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
	r := killmatrix.Runner{Workers: min(4, runtime.NumCPU()), BaselineRuns: 1, TimeoutPad: 500 * time.Millisecond, Schemata: true, Select: true, FailFast: true, Choose: firstPerLine}
	m := run(t, r, "testdata", "./discount", "./loop", "./table")
	verdicts := m.Verdicts()
	var b strings.Builder
	for _, mu := range m.Mutants {
		fmt.Fprintf(&b, "%s:%d %s %s → %s: %s\n", mu.File, mu.Line, mu.Operator, mu.Original, mu.Mutated, verdicts[mu.ID])
	}
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
	// Failing fast stops each at its first kill: 10 runs, one more if two
	// discount tests of equal baseline swap places.
	if ran > 11 {
		t.Errorf("%d runs recorded; failing fast should stop at the first kill", ran)
	}
}

func TestChooseNone(t *testing.T) {
	t.Parallel()
	onlyLoop := func(file string, _ []mutant.Mutant) []int {
		if filepath.Base(file) != "loop.go" {
			return nil
		}
		return []int{0}
	}
	r := killmatrix.Runner{Workers: runtime.NumCPU(), TimeoutPad: time.Second, Schemata: true, Select: true, FailFast: true, Choose: onlyLoop}
	m := run(t, r, "testdata", "./discount", "./loop", "./table")
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
		Workers:      min(4, runtime.NumCPU()),
		BaselineRuns: 1,
		TimeoutPad:   500 * time.Millisecond,
		Schemata:     true,
		Select:       true,
		FailFast:     true,
	}).Run(t.Context(), "testdata", "./discount", "./loop", "./table")
	if err != nil {
		t.Fatal(err)
	}
	got := fast.Verdicts()
	var lines strings.Builder
	for _, mu := range fast.Mutants {
		fmt.Fprintf(&lines, "M%d %s\n", mu.ID, got[mu.ID])
	}
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

func TestRunError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module bad\n\ngo 1.21\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.go"), []byte("package bad\nfunc f(n int) bool { return n > 0 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad_test.go"), []byte("package bad\nimport \"testing\"\nfunc TestBad(t *testing.T) { _ = undefined }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := killmatrix.Runner{Workers: 2, BaselineRuns: 1}
	if _, err := r.Run(t.Context(), dir, "."); err == nil {
		t.Fatal("expected build error, got nil")
	}
}

func TestConfirm(t *testing.T) {
	t.Parallel()
	// Sequential subtests: testdata/flaky keys its marker files by ppid.
	for _, failFast := range []bool{false, true} {
		t.Run(fmt.Sprintf("failfast=%v", failFast), func(t *testing.T) {
			r := killmatrix.Runner{Workers: runtime.NumCPU(), BaselineRuns: 1, TimeoutPad: 500 * time.Millisecond, Schemata: true, Select: failFast, FailFast: failFast, Confirm: true}
			m := run(t, r, "testdata", "./flaky")
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
			r := killmatrix.Runner{Workers: 1, BaselineRuns: 1, TimeoutPad: 500 * time.Millisecond, Schemata: true, Select: failFast, FailFast: failFast, Confirm: true}
			m := run(t, r, "testdata", "./load")
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

// TestFailFastBatches checks that batched runs match the full matrix on
// panics, hangs, and parallel failures, except that a mutant one test kills
// and another hangs on may read as a timeout under FailFast.
func TestFailFastBatches(t *testing.T) {
	t.Parallel()
	full := killmatrix.Runner{Workers: min(4, runtime.NumCPU()), BaselineRuns: 1, TimeoutPad: 500 * time.Millisecond, Schemata: true}
	ref := run(t, full, "testdata", "./batch")
	xtesting.Prints(t, ref, batchMatrix)
	want := ref.Verdicts()
	hangs := map[int]bool{}
	for _, res := range ref.Results {
		if res.Status == killmatrix.Timeout {
			hangs[res.MutantID] = true
		}
	}
	for _, confirm := range []bool{false, true} {
		fast := full
		fast.Select, fast.FailFast, fast.Confirm = true, true, confirm
		got := run(t, fast, "testdata", "./batch").Verdicts()
		for id, v := range want {
			drift := v == killmatrix.VerdictKilled && got[id] == killmatrix.VerdictTimeout && hangs[id]
			if got[id] != v && !drift {
				t.Errorf("confirm=%v: M%d is %s, the full matrix says %s", confirm, id, got[id], v)
			}
		}
	}
}

// M15 hangs TestStepsZero, which runs before TestStepsThree kills it. M11
// crashes the process from Async's goroutine in TestAsyncEnd, alone; M6
// does in TestParallelBelow while TestParallelEnd runs. M3, M12 and M16
// are equivalent for these inputs.
const batchMatrix = `
		fixture/batch
		  T1 TestAtInside
		  T2 TestAsyncEnd
		  T3 TestAtEnd
		  T4 TestParallelEnd
		  T5 TestParallelBelow
		  T6 TestStepsZero
		  T7 TestStepsThree

		      T1 T2 T3 T4 T5 T6 T7
		  M1  K  .  .  .  .  .  .   batch.go:9:5 COR i < 0 || i >= len(xs) → true
		  M2  .  K  K  K  K  .  .   batch.go:9:5 COR i < 0 || i >= len(xs) → false
		  M3  .  .  .  .  .  .  .   batch.go:9:7 ROR i < 0 → i <= 0
		  M4  K  .  .  .  K  .  .   batch.go:9:7 ROR i < 0 → i > 0
		  M5  K  .  .  .  K  .  .   batch.go:9:7 ROR i < 0 → i >= 0
		  M6  .  .  .  .  K  .  .   batch.go:9:7 ROR i < 0 → i == 0
		  M7  K  .  .  .  .  .  .   batch.go:9:7 ROR i < 0 → i != 0
		  M8  .  K  K  K  K  .  .   batch.go:9:11 COR i < 0 || i >= len(xs) → i < 0 && i >= len(xs)
		  M9  K  K  K  K  .  .  .   batch.go:9:16 ROR i >= len(xs) → i < len(xs)
		  M10 K  .  .  .  .  .  .   batch.go:9:16 ROR i >= len(xs) → i <= len(xs)
		  M11 .  K  K  K  .  .  .   batch.go:9:16 ROR i >= len(xs) → i > len(xs)
		  M12 .  .  .  .  .  .  .   batch.go:9:16 ROR i >= len(xs) → i == len(xs)
		  M13 K  K  K  K  .  .  .   batch.go:9:16 ROR i >= len(xs) → i != len(xs)
		  M14 .  .  .  .  .  .  K   batch.go:26:8 ROR n != 0 → n < 0
		  M15 .  .  .  .  .  T  K   batch.go:26:8 ROR n != 0 → n <= 0
		  M16 .  .  .  .  .  .  .   batch.go:26:8 ROR n != 0 → n > 0
		  M17 .  .  .  .  .  K  K   batch.go:26:8 ROR n != 0 → n >= 0
		  M18 .  .  .  .  .  K  K   batch.go:26:8 ROR n != 0 → n == 0
		  M19 .  .  .  .  .  .  T   batch.go:27:3 SDL n-- → (deleted)
		  M20 .  .  .  .  .  .  K   batch.go:28:3 SDL steps++ → (deleted)
`

// TestBatchLimit checks that six 20 ms tests batched into one process do not
// time out even with a 1 ms pad, because their baselines sum to cover the batch.
func TestBatchLimit(t *testing.T) {
	t.Parallel()
	r := killmatrix.Runner{Workers: 2, BaselineRuns: 1, TimeoutPad: time.Millisecond, Schemata: true, Select: true, FailFast: true}
	m := run(t, r, "testdata", "./slowbatch")
	xtesting.EqualText(t, formatVerdicts(m), `
		M1 survived
		M2 killed
		M3 killed
		M4 killed
	`)
}

func TestConfirmStall(t *testing.T) {
	t.Parallel()
	r := killmatrix.Runner{Workers: 2, BaselineRuns: 1, TimeoutPad: 50 * time.Millisecond, Schemata: true, Select: true, FailFast: true, Confirm: true}
	m := run(t, r, "testdata", "./stall")
	xtesting.EqualText(t, formatVerdicts(m), `
		M1 survived
		M2 killed
		M3 killed
		M4 killed
	`)
}

func TestInfect(t *testing.T) {
	t.Parallel()
	base := killmatrix.Runner{Workers: min(4, runtime.NumCPU()), BaselineRuns: 1, TimeoutPad: 500 * time.Millisecond, Schemata: true, Select: true, FailFast: true}
	infect := base
	infect.Infect = true
	patterns := []string{"./discount", "./loop", "./table", "./batch"}
	want := run(t, base, "testdata", patterns...)
	got := run(t, infect, "testdata", patterns...)
	// Which test runs first decides whether batch's M15 reads as killed
	// or as a timeout (TestFailFastBatches); neither is a gap.
	detected := func(m *killmatrix.Matrix) string { return strings.ReplaceAll(formatVerdicts(m), " timeout", " killed") }
	xtesting.EqualText(t, detected(got), detected(want))
	ran := func(m *killmatrix.Matrix, id int) (n int) {
		for _, res := range m.Results {
			if (id == 0 || res.MutantID == id) && res.MS > 0 {
				n++
			}
		}
		return n
	}
	uninfected := 0
	for _, res := range got.Results {
		if res.MS == 0 && !res.Skipped && res.Status == killmatrix.Survived {
			uninfected++
		}
	}
	if uninfected == 0 {
		t.Error("Infect skipped no test")
	}
	boundary := 0
	for _, mu := range got.Mutants {
		if mu.Mutated == "price >= 100" {
			boundary = mu.ID
		}
	}
	if n := ran(got, boundary); boundary == 0 || n != 0 || got.Verdicts()[boundary] != killmatrix.VerdictSurvived {
		t.Errorf("price >= 100 (M%d): %d runs, %s; want none, survived", boundary, n, got.Verdicts()[boundary])
	}
}

// TestSchemataSideEffects checks that mutating `return t, t.fill(s)` to
// `return t, nil` skips fill's side effect in a schema binary just as in an
// individual build.
func TestSchemataSideEffects(t *testing.T) {
	t.Parallel()
	own := killmatrix.Runner{Workers: 2, BaselineRuns: 1, TimeoutPad: 500 * time.Millisecond}
	schema := own
	schema.Schemata, schema.Select, schema.Infect, schema.FailFast = true, true, true, true
	want := formatVerdicts(run(t, own, "testdata", "./sideeffect"))
	xtesting.EqualText(t, formatVerdicts(run(t, schema, "testdata", "./sideeffect")), want)
	if !strings.Contains(want, "killed") {
		t.Errorf("no mutant killed by its own build:\n%s", want)
	}
}
