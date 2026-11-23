package killmatrix_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ibrhmkoz/go-kit/mutant"
	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
	"github.com/ibrhmkoz/go-kit/xtesting"
)

// TestZeroRunnerDefaults checks zero-value Runner defaults (Workers=1, TimeoutPad=1s),
// go 1.17 WithGo121 injection (L633/656/657), Choose skipping index 0 (L373),
// Select=false not skipping unreached tests (L475), and Workers=1 skipping baseline recheck (L504).
func TestZeroRunnerDefaults(t *testing.T) {
	t.Parallel()
	dir := writeModule(t, map[string]string{
		"go.mod": "module zerorun\n\ngo 1.17\n",
		"p.go": `package zerorun
func F(x int) bool     { return x > 0 }
func Other(x int) bool { return x > 0 }
`,
		"p_test.go": `package zerorun
import ("os"; "testing"; "time")
func TestF(t *testing.T) {
	if id := os.Getenv("GOMUTANT"); id != "" && id != "0" {
		time.Sleep(80 * time.Millisecond)
	}
	if !F(2) { t.Fatal() }
}
func TestOther(t *testing.T) { if !Other(2) { t.Fatal() } }
func TestTransient(t *testing.T) {
	if _, err := os.Stat("once"); err != nil {
		_ = os.WriteFile("once", nil, 0o600)
		t.Fatal("once")
	}
	if !F(2) { t.Fatal() }
}
`,
	})
	// Pick x >= 0 on F (skipping index 0, which would kill TestF if s.find[0] != -1).
	r := killmatrix.Runner{
		Schemata: true,
		Choose: func(_ string, ms []mutant.Mutant) []int {
			for i, m := range ms {
				if m.Pos.Line == 2 && m.Mutated == "x >= 0" {
					return []int{i}
				}
			}
			return nil
		},
	}
	m := run(t, r, dir, ".")
	if !m.Mutants[0].Schema {
		t.Fatalf("Schema=false on go 1.17 module, want true")
	}
	if got := m.Verdicts()[1]; got != killmatrix.VerdictSurvived {
		t.Fatalf("verdict=%s, want survived", got)
	}
	for _, res := range m.Results {
		if res.Skipped {
			t.Errorf("result %+v marked Skipped with Select=false", res)
		}
	}
	if m.Tests[2].State != killmatrix.Flaky {
		t.Fatalf("TestTransient state=%s, want flaky (no baseline recheck at Workers=1)", m.Tests[2].State)
	}
}

// TestConfirmRecheckAndBatchPaths packs baseline edge cases (parallel recheck,
// median sort, crash before reporting, subtest exit 0) and all eight FailFast/
// Confirm/recheck/batch scenarios into ONE package isolated per function via Select=true.
func TestConfirmRecheckAndBatchPaths(t *testing.T) {
	t.Parallel()
	dir := writeModule(t, map[string]string{
		"go.mod": "module ffpaths\n\ngo 1.21\n",
		"p.go": `package ffpaths
func Fload(x int) bool      { return x > 0 }
func SlowSingle(x int) bool { return x > 0 }
func StopTo(x int) bool     { return x > 0 }
func BFlaky(x int) bool     { return x > 0 }
func BStall(x int) bool     { return x > 0 }
func BCrash(x int) bool     { return x > 0 }
func SmallPad(x int) bool   { return x > 0 }
func Recheck3x(x int) bool  { return x > 0 }
func Trans(x int) bool      { return x > 0 }
`,
		"p_test.go": `package ffpaths
import ("os"; "testing"; "time")

func isMut() bool { id := os.Getenv("GOMUTANT"); return id != "" && id != "0" }
func bump(name string) int {
	b, _ := os.ReadFile(name)
	_ = os.WriteFile(name, append(b, 'x'), 0o600)
	return len(b)
}

func TestCrash(t *testing.T) { os.Exit(1) }
func TestMedian(t *testing.T) {
	if bump("med") == 1 { time.Sleep(200 * time.Millisecond) }
}
func TestSubExit0(t *testing.T) {
	if bump("exit0")%2 == 1 { os.Exit(0) }
	t.Run("a", func(t *testing.T) {})
}
func TestTransient(t *testing.T) {
	if !isMut() && bump("trans") == 0 { t.Fatal("transient") }
	if Trans(0) { t.Fatal() }
}

func TestFload(t *testing.T) {
	if !isMut() {
		// Runs 0-2 are baseline (pass); Run 3 is Confirm original (fail -> recheck); Run 4 is solo recheck (pass).
		if bump("fload-orig") == 3 { t.Fatal("loaded original") }
		if !Fload(2) { t.Fatal() }
		return
	}
	if bump("fload-mut") < 2 { t.Fatal("transient load") }
	if !Fload(2) { t.Fatal() }
}

func TestSSFast(t *testing.T)  { if !SlowSingle(2) { t.Fatal() } }
func TestSSSlow1(t *testing.T) { time.Sleep(52 * time.Millisecond); if !SlowSingle(2) { t.Fatal() } }
func TestSSSlow2(t *testing.T) { time.Sleep(55 * time.Millisecond); if SlowSingle(0) { t.Fatal() } }

func TestStopSlow1(t *testing.T) {
	time.Sleep(52 * time.Millisecond)
	_ = StopTo(2)
	if isMut() { for {} }
}
func TestStopSlow2(t *testing.T) { time.Sleep(80 * time.Millisecond); if !StopTo(1) { t.Fatal() } }

// Inside a single fast batch, go test runs tests in declaration order (TestBFA -> TestBFB -> TestBFC).
func TestBFA(t *testing.T) { if !BFlaky(2) { t.Fatal() } }
func TestBFB(t *testing.T) {
	if isMut() && bump("bflaky") == 0 { t.Fatal("flaky") }
	if !BFlaky(2) { t.Fatal() }
}
func TestBFC(t *testing.T) { if BFlaky(0) { t.Fatal() } }

func TestBSA(t *testing.T) {
	if isMut() && bump("bstall") == 0 { time.Sleep(250 * time.Millisecond) }
	if !BStall(2) { t.Fatal() }
}
func TestBSB(t *testing.T) { if BStall(0) { t.Fatal() } }

func TestBCA(t *testing.T) {
	if isMut() && bump("bcrash") == 0 { go func() { panic("flaky crash") }(); select {} }
	if !BCrash(2) { t.Fatal() }
}
func TestBCB(t *testing.T) { if BCrash(0) { t.Fatal() } }

func TestSmallPad(t *testing.T) {
	if isMut() { time.Sleep(500 * time.Millisecond) }
	if !SmallPad(2) { t.Fatal() }
}

func TestRecheck3x(t *testing.T) {
	if isMut() { time.Sleep(80 * time.Millisecond) }
	if !Recheck3x(2) { t.Fatal() }
}
`,
	})
	choose := func(_ string, ms []mutant.Mutant) []int {
		var keep []int
		for i, m := range ms {
			if m.Mutated == "x >= 0" {
				keep = append(keep, i)
			}
		}
		return keep
	}
	r := killmatrix.Runner{
		Workers:       4,
		BaselineRuns:  3,
		TimeoutFactor: 1,
		TimeoutPad:    40 * time.Millisecond,
		Schemata:      true,
		Select:        true,
		FailFast:      true,
		Confirm:       true,
		Choose:        choose,
	}
	m := run(t, r, dir, ".")

	// Verify baseline edge cases: crash, median sort, exit 0 subtest, and parallel recheck.
	var baseStates []string
	for _, tt := range m.Tests {
		switch tt.Name {
		case "TestCrash", "TestMedian", "TestSubExit0/a", "TestTransient":
			baseStates = append(baseStates, fmt.Sprintf("%s %s", tt.Name, tt.State))
		}
		if tt.Name == "TestMedian" && tt.BaselineMS >= 160 {
			t.Errorf("TestMedian BaselineMS=%.1fms, want sorted median < 160ms", tt.BaselineMS)
		}
	}
	xtesting.EqualText(t, strings.Join(baseStates, "\n"), `
		TestCrash failing
		TestMedian ok
		TestSubExit0/a flaky
		TestTransient ok
	`)

	v := m.Verdicts()
	var lines []string
	for _, mu := range m.Mutants {
		lines = append(lines, fmt.Sprintf("L%d %s", mu.Line, v[mu.ID]))
	}
	xtesting.EqualText(t, strings.Join(lines, "\n"), `
		L2 survived
		L3 killed
		L4 timeout
		L5 killed
		L6 killed
		L7 killed
		L8 timeout
		L9 survived
		L10 killed
	`)
	sawFloadFlaky, stopToRuns := false, 0
	for _, res := range m.Results {
		mu := m.Mutants[res.MutantID-1]
		switch mu.Line {
		case 2:
			if res.Status == killmatrix.FlakyKill {
				sawFloadFlaky = true
			}
		case 4:
			stopToRuns++
		}
	}
	if !sawFloadFlaky || stopToRuns != 1 {
		t.Errorf("sawFloadFlaky=%v stopToRuns=%d", sawFloadFlaky, stopToRuns)
	}
}
