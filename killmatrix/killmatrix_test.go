package killmatrix_test

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

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
