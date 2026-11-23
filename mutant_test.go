package mutant_test

import (
	"bytes"
	"cmp"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/ibrhmkoz/go-kit/mutant"
	"github.com/ibrhmkoz/go-kit/xtesting"
)

// stdImporter caches standard library packages across tests so each test
// does not re-read export data from disk.
var stdImporter = &cachedImporter{base: importer.Default(), cache: map[string]*types.Package{}}

type cachedImporter struct {
	mu    sync.Mutex
	base  types.Importer
	cache map[string]*types.Package
}

func (c *cachedImporter) Import(path string) (*types.Package, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if pkg, ok := c.cache[path]; ok {
		return pkg, nil
	}
	pkg, err := c.base.Import(path)
	if err == nil {
		c.cache[path] = pkg
	}
	return pkg, err
}

// find type-checks src as a file of its own package and returns its mutants.
func find(t testing.TB, name, src string) []mutant.Mutant {
	t.Helper()
	return findInPkg(t, "p", name, src)
}

func findInPkg(t testing.TB, pkg, name, src string) []mutant.Mutant {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	conf := types.Config{Importer: stdImporter}
	if _, err := conf.Check(pkg, fset, []*ast.File{f}, info); err != nil {
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
				discount.go:9:3 SDL fmt.Println(total) → _ = fmt.Println [arid: logging]
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

// loadStdlib loads all stdlib packages needed by TestApplyOnStdlib and
// TestEmbedTypeChecks in one packages.Load call.
var loadStdlib = sync.OnceValues(func() ([]*packages.Package, error) {
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps}
	return packages.Load(cfg, "cmp", "os", "strconv", "sort", "strings", "net/url", "encoding/csv", "text/tabwriter", "path", "bufio")
})

// TestApplyOnStdlib checks, over every mutant of a few standard library
// packages, that a mutant replaces exactly its Original with its Mutated and
// that the result still parses.
func TestApplyOnStdlib(t *testing.T) {
	t.Parallel()
	pkgs, err := loadStdlib()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"strconv": true, "sort": true, "strings": true}
	type job struct {
		src []byte
		m   mutant.Mutant
	}
	var jobs []job
	for _, pkg := range pkgs {
		if !want[pkg.PkgPath] {
			continue
		}
		for _, f := range pkg.Syntax {
			name := pkg.Fset.File(f.Pos()).Name()
			src, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range mutant.Find(pkg.Fset, f, src, pkg.TypesInfo) {
				jobs = append(jobs, job{src, m})
			}
		}
	}
	if len(jobs) < 1000 {
		t.Errorf("only %d mutants over strconv, sort and strings; Find is missing nodes", len(jobs))
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Go(func() {
			for {
				i := int(next.Add(1) - 1)
				if i >= len(jobs) {
					return
				}
				j := jobs[i]
				got := j.m.Apply(j.src)
				if len(got)-len(j.src) != len(j.m.Mutated)-len(j.m.Original) {
					t.Errorf("%v: size changed by %d, want %d", j.m, len(got)-len(j.src), len(j.m.Mutated)-len(j.m.Original))
					return
				}
				if !bytes.Contains(j.src, []byte(j.m.Original)) || !bytes.Contains(got, []byte(j.m.Mutated)) {
					t.Errorf("%v: original or mutated text not found", j.m)
					return
				}
				if _, err := parser.ParseFile(token.NewFileSet(), "", got, parser.SkipObjectResolution); err != nil {
					t.Errorf("%v: %v", j.m, err)
					return
				}
			}
		})
	}
	wg.Wait()
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
	"slices"
)

func consume(any) {}

func check(xs []int, n int) bool {
	consume(new(bytes.Buffer))
	if slices.Contains(xs, n) {
		return true
	}
	return false
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	if _, err := (&types.Config{Importer: stdImporter}).Check("p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	// bytes is used only for a type: a nil pointer to it keeps it used.
	// slices.Contains is generic: no expression names it without type
	// arguments, so an import used only for it is not repaired.
	xtesting.EqualText(t, listing(mutant.Find(fset, f, []byte(src), info)), `
		p.go:11:2 SDL consume(new(bytes.Buffer)) → _ = (*bytes.Buffer)(nil)
		p.go:12:5 COR slices.Contains(xs, n) → true
		p.go:12:5 COR slices.Contains(xs, n) → false
	`)
}

func TestAridSwitch(t *testing.T) {
	src := `package p

import "log"

type LeveledLogger interface{ Debug(msg string, args ...any) }

func report(logger any, n int) int {
	if logger != nil {
		switch v := logger.(type) {
		case LeveledLogger:
			v.Debug("n", "n", n)
		case *log.Logger:
			v.Printf("n=%d", n)
		}
	}
	return n
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Implicits: map[ast.Node]types.Object{}}
	if _, err := (&types.Config{Importer: stdImporter}).Check("p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	// A switch whose every case only logs is arid, and so is the if that
	// guards it: its condition only decides whether to log.
	xtesting.EqualText(t, listing(mutant.Find(fset, f, []byte(src), info)), `
		p.go:8:5 COR logger != nil → true [arid: only arid statements]
		p.go:8:5 COR logger != nil → false [arid: only arid statements]
		p.go:8:12 ROR logger != nil → logger == nil [arid: only arid statements]
		p.go:11:4 SDL v.Debug("n", "n", n) → _ = v [arid: logging]
		p.go:13:4 SDL v.Printf("n=%d", n) → _ = v [arid: logging]
	`)
}

func TestArid(t *testing.T) {
	src := `package p

import (
	"fmt"
	"io"
	"log"
)

// Logger is the kind of interface services take for logging.
type Logger interface {
	Debug(msg string, args ...any)
}

func work(l Logger, w io.Writer, n int, debug bool) []int {
	log.Printf("working on %d", n+1)
	l.Debug("work", "n", n-1)
	if debug {
		log.Println("debug", n*2)
	}
	out := make([]int, 0, n+1)
	fmt.Fprintf(w, "%d\n", n+2)
	out = append(out, n*3) //mut:ignore
	return out
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	if _, err := (&types.Config{Importer: stdImporter}).Check("p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	type key struct {
		line int
		op   mutant.Operator
		arid string
	}
	var order []key
	count := map[key]int{}
	for _, m := range mutant.Find(fset, f, []byte(src), info) {
		k := key{m.Pos.Line, m.Operator, cmp.Or(m.Arid, "-")}
		if count[k] == 0 {
			order = append(order, k)
		}
		count[k]++
	}
	var b strings.Builder
	for _, k := range order {
		fmt.Fprintf(&b, "%d %s ×%d: %s\n", k.line, k.op, count[k], k.arid)
	}
	// Logging, as a statement or behind an if that does nothing else, a
	// slice's capacity, and a line marked mut:ignore are arid. Writing to
	// a writer the caller passed is output, not logging.
	xtesting.EqualText(t, b.String(), `
		15 SDL ×1: logging
		15 AOR ×4: logging
		16 SDL ×1: logging
		16 AOR ×4: logging
		17 COR ×2: only arid statements
		18 SDL ×1: logging
		18 AOR ×4: logging
		20 AOR ×4: capacity hint
		21 SDL ×1: -
		21 AOR ×4: -
		22 SDL ×1: mut:ignore
		22 AOR ×4: mut:ignore
	`)
}

// TestAridRules checks line-above mut:ignore, os.Stdout/Stderr detection,
// arid poisoning on if/else and switch branches, and metrics/tracing/map rules.
func TestAridRules(t *testing.T) {
	src := `package p

import (
	"fmt"
	"io"
	"log"
	"os"
)

type myLogger struct{}

func (myLogger) Other(int) {}

type Span struct{}

func (Span) SetAttributes(int) {}
func (Span) Other(int)         {}

type streams struct{ Stdout io.Writer }

func getStreams() streams { return streams{} }

func f(n int, flag bool, l myLogger, sp Span, st streams) int {
	//mut:ignore
	n += 1
	// regular comment
	n += 2
	fmt.Fprintln(os.Stderr, n+1)
	fmt.Fprintln(os.Stdout, n+2)
	fmt.Fprintln(os.Stdin, n+3)
	fmt.Fprintln(io.Discard, n+4)
	fmt.Fprintln(st.Stdout, n+5)
	fmt.Fprintln(getStreams().Stdout, n+6)
	log.Fatal(n + 7)
	l.Other(n + 8)
	sp.SetAttributes(n + 9)
	sp.Other(n + 10)
	m1 := make(map[int]int, n+11)
	s1 := make([]int, n+12)
	c1 := make(chan int, n+13)
	_, _, _ = m1, s1, c1
	if flag {
	}
	if flag {
		log.Println(n + 14)
	} else {
		log.Println(n + 15)
	}
	if flag {
		log.Println(n + 16)
	} else {
		n += 17
	}
	if flag {
		switch n {
		case 1:
		}
	}
	if flag {
		switch n {
		case 1:
			log.Println(n + 18)
		case 2:
			n += 19
		}
	}
	return n
}
`
	var lines []int
	byLine := map[int]string{}
	for _, m := range find(t, "p.go", src) {
		tag := cmp.Or(m.Arid, "-")
		if prev, ok := byLine[m.Pos.Line]; !ok {
			lines = append(lines, m.Pos.Line)
			byLine[m.Pos.Line] = tag
		} else if prev != tag {
			t.Errorf("line %d mixed arid tags: %q vs %q", m.Pos.Line, prev, tag)
		}
	}
	var b strings.Builder
	for _, line := range lines {
		fmt.Fprintf(&b, "%d %s\n", line, byLine[line])
	}
	// Standalone //mut:ignore marks only the next line; os.Stdout/Stderr are
	// logging while os.Stdin, io.Discard, and struct fields are not; a single
	// non-arid statement in an else or switch case clears arid on the outer if.
	xtesting.EqualText(t, b.String(), `
		25 mut:ignore
		27 -
		28 logging
		29 logging
		30 -
		31 -
		32 -
		33 -
		34 -
		35 -
		36 tracing
		37 -
		38 capacity hint
		39 -
		40 -
		41 -
		42 -
		44 only arid statements
		45 logging
		47 logging
		49 -
		50 logging
		52 -
		54 -
		59 -
		62 logging
		64 -
	`)

	metricSrc := `package metrics

type Counter struct{}

func (Counter) Inc(int)   {}
func (Counter) Other(int) {}

func record(c Counter, n int) {
	c.Inc(n + 1)
	c.Other(n + 2)
}
`
	var mb strings.Builder
	seen := map[int]bool{}
	for _, m := range findInPkg(t, "example.com/metrics", "m.go", metricSrc) {
		if !seen[m.Pos.Line] {
			seen[m.Pos.Line] = true
			fmt.Fprintf(&mb, "%d %s\n", m.Pos.Line, cmp.Or(m.Arid, "-"))
		}
	}
	xtesting.EqualText(t, mb.String(), `
		9 metrics
		10 -
	`)
}
