package killmatrix_test

import (
	"runtime"
	"testing"
	"time"

	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
	"github.com/ibrhmkoz/go-kit/xtesting"
)

func TestRun(t *testing.T) {
	r := killmatrix.Runner{Workers: runtime.NumCPU(), TimeoutPad: 300 * time.Millisecond}
	m, err := r.Run(t.Context(), "testdata", "./...")
	if err != nil {
		t.Fatal(err)
	}
	// M5 is the boundary test the suite lacks (price == 100), M18 is
	// equivalent for n >= 0, M14 loops forever and M21 leaves x unused.
	// Each subtest of TestAbs is a column; where index panics, the subtests
	// it hides count as killed too (M23, M25-M27).
	xtesting.Prints(t, m, `
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
		  M13 K  .   loop.go:8:6 COR i >= n → true
		  M14 T  .   loop.go:8:6 COR i >= n → false
		  M15 K  .   loop.go:8:8 ROR i >= n → i < n
		  M16 K  .   loop.go:8:8 ROR i >= n → i <= n
		  M17 K  .   loop.go:8:8 ROR i >= n → i > n
		  M18 .  .   loop.go:8:8 ROR i >= n → i == n
		  M19 K  .   loop.go:8:8 ROR i >= n → i != n
		  M20 K  .   loop.go:11:3 SDL c++ → (deleted)
		  M21 B  B   loop.go:18:3 SDL last = x → (deleted)

		fixture/table
		  T7 TestAbs/index
		  T8 TestAbs/negative
		  T9 TestAbs/positive
		  T10 TestAbs/zero

		      T7 T8 T9 T10
		  M22 .  .  K  .    table.go:6:5 COR x < 0 → true
		  M23 K  K  K  K    table.go:6:5 COR x < 0 → false
		  M24 .  .  .  .    table.go:6:7 ROR x < 0 → x <= 0
		  M25 K  K  K  K    table.go:6:7 ROR x < 0 → x > 0
		  M26 K  K  K  K    table.go:6:7 ROR x < 0 → x >= 0
		  M27 K  K  K  K    table.go:6:7 ROR x < 0 → x == 0
		  M28 .  .  K  .    table.go:6:7 ROR x < 0 → x != 0
	`)
}
