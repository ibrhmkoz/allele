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
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
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
			name: "statements in a block are deleted, := is not since it would leave names undefined",
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
				discount.go:8:3 SDL total += xs[i] → (deleted)
				discount.go:8:9 AOR total += xs[i] → total -= xs[i]
				discount.go:8:9 AOR total += xs[i] → total *= xs[i]
				discount.go:8:9 AOR total += xs[i] → total /= xs[i]
				discount.go:8:9 AOR total += xs[i] → total %= xs[i]
				discount.go:9:3 SDL fmt.Println(total) → (deleted)
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
				for _, m := range mutant.Find(pkg.Fset, f, src, pkg.TypesInfo) {
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
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
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
