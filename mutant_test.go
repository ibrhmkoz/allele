package mutant_test

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/ibrhmkoz/go-kit/mutant"
	"github.com/ibrhmkoz/go-kit/xtesting"
)

// find type-checks src as a file of its own package and returns its mutants.
func find(t testing.TB, name, src string) []mutant.Mutant {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: importer.Default()}
	if _, err := conf.Check("p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	return mutant.Find(fset, f, []byte(src), info)
}

func listing(ms []mutant.Mutant) string {
	var b strings.Builder
	for _, m := range ms {
		fmt.Fprintln(&b, m)
	}
	return b.String()
}

func TestFind(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "relational, logical and arithmetic operators, and the if condition",
			src: `package p

func Discount(price int, vip bool) int {
	if price > 100 && vip {
		return price - 20
	}
	return price
}
`,
			want: `
				discount.go:4:5 COR price > 100 && vip → true
				discount.go:4:5 COR price > 100 && vip → false
				discount.go:4:11 ROR price > 100 → price < 100
				discount.go:4:11 ROR price > 100 → price <= 100
				discount.go:4:11 ROR price > 100 → price >= 100
				discount.go:4:11 ROR price > 100 → price == 100
				discount.go:4:11 ROR price > 100 → price != 100
				discount.go:4:17 COR price > 100 && vip → price > 100 || vip
				discount.go:5:16 AOR price - 20 → price + 20
				discount.go:5:16 AOR price - 20 → price * 20
				discount.go:5:16 AOR price - 20 → price / 20
				discount.go:5:16 AOR price - 20 → price % 20
			`,
		},
		{
			name: "an error checked against nil and a returned error get ERR, a returned nil does not",
			src: `package p

import "os"

func Load(name string) ([]byte, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return data, nil
}
`,
			want: `
				discount.go:7:5 COR err != nil → true
				discount.go:7:5 COR err != nil → false
				discount.go:7:9 ERR err != nil → err == nil
				discount.go:8:15 ERR return nil, err → return nil, nil
			`,
		},
		{
			name: "statements in a block are deleted, := is not since it would leave names undefined; i keeps a use",
			src: `package p

import "fmt"

func Sum(xs []int) int {
	total := 0
	for i := range xs {
		total += xs[i]
		fmt.Println(total)
	}
	return total
}
`,
			want: `
				discount.go:8:3 SDL total += xs[i] → _ = i
				discount.go:8:9 AOR total += xs[i] → total -= xs[i]
				discount.go:8:9 AOR total += xs[i] → total *= xs[i]
				discount.go:8:9 AOR total += xs[i] → total /= xs[i]
				discount.go:8:9 AOR total += xs[i] → total %= xs[i]
				discount.go:9:3 SDL fmt.Println(total) → _ = fmt.Println
			`,
		},
		{
			name: "only == and != swap for values without an order",
			src: `package p

type node struct{ next *node }

func last(n *node) bool { return n.next == nil }
`,
			want: `
				discount.go:5:41 ROR n.next == nil → n.next != nil
			`,
		},
		{
			name: "% is dropped for floats, + on strings is left alone",
			src: `package p

func mean(sum float64, n float64) float64 { return sum / n }

func greet(name string) string { return "hi " + name }
`,
			want: `
				discount.go:3:56 AOR sum / n → sum + n
				discount.go:3:56 AOR sum / n → sum - n
				discount.go:3:56 AOR sum / n → sum * n
			`,
		},
		{
			name: "a condition that holds the only use of locals keeps them used",
			src: `package p

func cmp(a, b []int) int {
	si, oi := len(a), len(b)
	if si > oi {
		return 1
	}
	return -1
}
`,
			want: `
				discount.go:5:5 COR si > oi → func() bool { _, _ = si, oi; return true }()
				discount.go:5:5 COR si > oi → func() bool { _, _ = si, oi; return false }()
				discount.go:5:8 ROR si > oi → si < oi
				discount.go:5:8 ROR si > oi → si <= oi
				discount.go:5:8 ROR si > oi → si >= oi
				discount.go:5:8 ROR si > oi → si == oi
				discount.go:5:8 ROR si > oi → si != oi
			`,
		},
		{
			name: "a deleted statement that holds the only use of a local keeps it used; assigning is not using",
			src: `package p

func last(xs []int) (n int) {
	for _, x := range xs {
		n = x
	}
	return
}
`,
			want: `
				discount.go:5:3 SDL n = x → _ = x
			`,
		},
		{
			name: "a condition that holds the only use of an import keeps it used",
			src: `package p

import "strings"

func trim(s string) string {
	if strings.HasPrefix(s, " ") {
		return s[1:]
	}
	return s
}
`,
			want: `
				discount.go:6:5 COR strings.HasPrefix(s, " ") → func() bool { _ = strings.HasPrefix; return true }()
				discount.go:6:5 COR strings.HasPrefix(s, " ") → func() bool { _ = strings.HasPrefix; return false }()
			`,
		},
		{
			name: "returning nil instead of the only use of err or of an import keeps them used",
			src: `package p

import (
	"fmt"
	"strconv"
)

func parse(s string) (int, error) {
	n, err := strconv.Atoi(s)
	return n, err
}

func check(n int) error {
	return fmt.Errorf("bad %d", n)
}
`,
			want: `
				discount.go:10:12 ERR return n, err → return n, func() error { _ = err; return nil }()
				discount.go:14:9 ERR return fmt.Errorf("bad %d", n) → return func() error { _ = fmt.Errorf; return nil }()
			`,
		},
		{
			name: "names declared inside a deleted statement go with it",
			src: `package p

func setup() func() int {
	var get func() int
	get = func() int { n := 2; return n }
	return get
}
`,
			want: `
				discount.go:5:2 SDL get = func() int { n := 2; return n } → (deleted)
			`,
		},
		{
			name: "an operator of another precedence keeps the tree: a && b && c is (a && b) && c",
			src: `package p

func f(a, b, c bool) bool { return a && b && c }

func g(x, y, z int) int { return x - y * z }
`,
			want: `
				discount.go:3:38 COR a && b → (a || b)
				discount.go:3:43 COR a && b && c → a && b || c
				discount.go:5:36 AOR x - y * z → x + y * z
				discount.go:5:36 AOR x - y * z → x * (y * z)
				discount.go:5:36 AOR x - y * z → x / (y * z)
				discount.go:5:36 AOR x - y * z → x % (y * z)
				discount.go:5:40 AOR y * z → (y + z)
				discount.go:5:40 AOR y * z → (y - z)
				discount.go:5:40 AOR y * z → y / z
				discount.go:5:40 AOR y * z → y % z
			`,
		},
		{
			name: "x++ uses x, so a condition need not keep it used",
			src: `package p

func spin(n int) {
	for i := 0; ; i++ {
		if i >= n {
			return
		}
	}
}
`,
			want: `
				discount.go:5:6 COR i >= n → true
				discount.go:5:6 COR i >= n → false
				discount.go:5:8 ROR i >= n → i < n
				discount.go:5:8 ROR i >= n → i <= n
				discount.go:5:8 ROR i >= n → i > n
				discount.go:5:8 ROR i >= n → i == n
				discount.go:5:8 ROR i >= n → i != n
			`,
		},
		{
			name: "a value that cannot be negative compared with 0: != is >, == is <=",
			src: `package p

func empty(s []int, u uint) bool { return len(s) == 0 || u != 0 }
`,
			want: `
				discount.go:3:50 ROR len(s) == 0 → len(s) < 0
				discount.go:3:50 ROR len(s) == 0 → len(s) <= 0 [equivalent: smallest value compared]
				discount.go:3:50 ROR len(s) == 0 → len(s) > 0
				discount.go:3:50 ROR len(s) == 0 → len(s) >= 0
				discount.go:3:50 ROR len(s) == 0 → len(s) != 0
				discount.go:3:55 COR len(s) == 0 || u != 0 → len(s) == 0 && u != 0
				discount.go:3:60 ROR u != 0 → u < 0
				discount.go:3:60 ROR u != 0 → u <= 0
				discount.go:3:60 ROR u != 0 → u > 0 [equivalent: smallest value compared]
				discount.go:3:60 ROR u != 0 → u >= 0
				discount.go:3:60 ROR u != 0 → u == 0
			`,
		},
		{
			name: "every string is >= \"\", so != \"\" is > \"\" and == \"\" is <= \"\"",
			src: `package p

func set(s string) bool { return s != "" }
`,
			want: `
				discount.go:3:36 ROR s != "" → s < ""
				discount.go:3:36 ROR s != "" → s <= ""
				discount.go:3:36 ROR s != "" → s > "" [equivalent: smallest value compared]
				discount.go:3:36 ROR s != "" → s >= ""
				discount.go:3:36 ROR s != "" → s == ""
			`,
		},
		{
			name: "deleting _ = a[k] on an array with a constant index changes nothing: the compiler checks the bound",
			src: `package p

func last(a [16]byte, s []byte) byte {
	_ = a[15]
	_ = s[15]
	return a[15]
}
`,
			want: `
				discount.go:4:2 SDL _ = a[15] → (deleted) [equivalent: blank assignment that cannot panic]
				discount.go:5:2 SDL _ = s[15] → (deleted)
			`,
		},
		{
			name: "0 on the left mirrors the rule",
			src: `package p

func any(n uint) bool { return 0 < n }
`,
			want: `
				discount.go:3:34 ROR 0 < n → 0 <= n
				discount.go:3:34 ROR 0 < n → 0 > n
				discount.go:3:34 ROR 0 < n → 0 >= n
				discount.go:3:34 ROR 0 < n → 0 == n
				discount.go:3:34 ROR 0 < n → 0 != n [equivalent: smallest value compared]
			`,
		},
		{
			name: "a case body is a statement list, a select case's receive is not",
			src: `package p

func drain(ch chan int, n int) {
	select {
	case <-ch:
		n++
	}
}
`,
			want: `
				discount.go:6:3 SDL n++ → (deleted)
			`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			xtesting.EqualText(t, listing(find(t, "discount.go", tt.src)), tt.want)
		})
	}
}

func TestApply(t *testing.T) {
	src := `package p

func Discount(price int, vip bool) int {
	if price > 100 && vip {
		return price - 20
	}
	return price
}
`
	ms := find(t, "discount.go", src)
	xtesting.EqualText(t, string(ms[2].Apply([]byte(src))), `
		package p

		func Discount(price int, vip bool) int {
			if price < 100 && vip {
				return price - 20
			}
			return price
		}
	`)
}

// TestApplyOnStdlib checks, over every mutant of a few standard library
// packages, that a mutant replaces exactly its Original with its Mutated and
// that the result still parses.
func TestApplyOnStdlib(t *testing.T) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo}
	pkgs, err := packages.Load(cfg, "strconv", "sort", "strings")
	if err != nil {
		t.Fatal(err)
	}
	var total atomic.Int64
	t.Cleanup(func() {
		if n := total.Load(); n < 1000 {
			t.Errorf("only %d mutants over strconv, sort and strings; Find is missing nodes", n)
		}
	})
	for _, pkg := range pkgs {
		for _, f := range pkg.Syntax {
			name := pkg.Fset.File(f.Pos()).Name()
			t.Run(filepath.Base(name), func(t *testing.T) {
				t.Parallel()
				src, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				for _, m := range append(mutant.Find(pkg.Fset, f, src, pkg.TypesInfo), mutant.Extreme(pkg.Fset, f, src, pkg.TypesInfo)...) {
					total.Add(1)
					got := m.Apply(src)
					if len(got)-len(src) != len(m.Mutated)-len(m.Original) {
						t.Fatalf("%v: size changed by %d, want %d", m, len(got)-len(src), len(m.Mutated)-len(m.Original))
					}
					if !bytes.Contains(src, []byte(m.Original)) || !bytes.Contains(got, []byte(m.Mutated)) {
						t.Fatalf("%v: original or mutated text not found", m)
					}
					if _, err := parser.ParseFile(token.NewFileSet(), "", got, parser.SkipObjectResolution); err != nil {
						t.Fatalf("%v: %v", m, err)
					}
				}
			})
		}
	}
}

func ExampleFind() {
	src := `package p

func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
`
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "max.go", src, 0)
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	_, _ = new(types.Config).Check("p", fset, []*ast.File{f}, info)

	ms := mutant.Find(fset, f, []byte(src), info)
	for _, m := range ms[:3] {
		fmt.Println(m)
	}
	fmt.Printf("%d mutants\n", len(ms))
	// Output:
	// max.go:4:5 COR a > b → true
	// max.go:4:5 COR a > b → false
	// max.go:4:7 ROR a > b → a < b
	// 7 mutants
}

func TestRepairTypeOnlyImport(t *testing.T) {
	src := `package p

import (
	"bytes"
	"fmt"
)

func render(n int) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "%d", n)
	return buf.String()
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	if _, err := (&types.Config{Importer: importer.Default()}).Check("p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	// bytes is used only for a type: a nil pointer to it keeps it used.
	xtesting.EqualText(t, listing(mutant.Extreme(fset, f, []byte(src), info)), `
		p.go:8:27 EXT { … → { _, _ = (*bytes.Buffer)(nil), fmt.Fprintf; return *new(string) }
	`)
}

func TestExtreme(t *testing.T) {
	src := `package p

import "strings"

type Counter struct{ n int }

var ready bool

func Name(s string) string { return strings.ToUpper(s) }

func Pair(n int) (int, error) { if n < 0 { return 0, nil }; return n * 2, nil }

func (c *Counter) Inc() { c.n++ }

func named(x int) (y int) { y = x + 1; return }

func empty() {}

func init() { ready = true }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: importer.Default()}
	if _, err := conf.Check("p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	// One mutant per function with a body: an empty body has nothing to
	// remove, and init runs before any test can see it go.
	xtesting.EqualText(t, listing(mutant.Extreme(fset, f, []byte(src), info)), `
		p.go:9:28 EXT { return strings.ToUpper(s) } → { _ = strings.ToUpper; return *new(string) }
		p.go:11:31 EXT { if n < 0 { return 0, nil }; return n * 2, nil } → { return *new(int), *new(error) }
		p.go:13:25 EXT { c.n++ } → {}
		p.go:15:27 EXT { y = x + 1; return } → { return }
	`)
}
