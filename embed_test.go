package mutant_test

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/ibrhmkoz/go-kit/mutant"
	"github.com/ibrhmkoz/go-kit/xtesting"
)

// sequential numbers mutants 1, 2, 3, ... like killmatrix does.
func sequential(n int) []int {
	ids := make([]int, n)
	for i := range ids {
		ids[i] = i + 1
	}
	return ids
}

func TestEmbed(t *testing.T) {
	src := `package p

func Discount(price int, vip bool) int {
	if price > 100 && vip {
		return price - 20
	}
	return price
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "discount.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	if _, err := new(types.Config).Check("p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	ms := mutant.Find(fset, f, []byte(src), info)
	out, embedded := mutant.Embed(fset, f, []byte(src), info, sequential(len(ms)))
	// Ids index the alternatives by operator: [6]int{<, <=, >, >=, ==, !=}
	// and [5]int{+, -, *, /, %}; 0 marks the original.
	xtesting.EqualText(t, string(out), `
		package p

		func Discount(price int, vip bool) int { if _mutOff(1, 12) {
			if price > 100 && vip {
				return price - 20
			}
			return price
		} else {/*line discount.go:3:41*/
			if _mutCond(func() bool { return _mutAnd(_mutOrd(price, 100, _mutGTR, [6]int{3, 4, 0, 5, 6, 7}), func() bool { return vip }, 8, true) }, 1, 2, true) {
				return _mutArith(price, 20, _mutSUB, [5]int{9, 0, 10, 11, 12})
			}
			return price
		}}/*line discount.go:8:2*/
	`)
	for i, ok := range embedded {
		if !ok {
			t.Errorf("%v not embedded", ms[i])
		}
	}
}

func TestEmbedKinds(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "statements are guarded, op-assignments switch, errors return nil",
			src: `package p

import "strconv"

func parse(xs []string) (int, error) {
	total := 0
	for _, x := range xs {
		n, err := strconv.Atoi(x)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}
`,
			want: `
				package p

				import "strconv"

				func parse(xs []string) (int, error) { if _mutOff(1, 9) {
					total := 0
					for _, x := range xs {
						n, err := strconv.Atoi(x)
						if err != nil {
							return 0, err
						}
						total += n
					}
					return total, nil
				} else {/*line p.go:5:39*/
					total := 0
					for _, x := range xs {
						n, err := strconv.Atoi(x)
						if _mutCond(func() bool { return _mutNot(err != nil, 3) }, 1, 2, true) {
							return 0, _mutErr(func() error { return err }, 4, true)
						}
						if !_mutOn(5) { switch _mutAt(6, 7, 8, 9) { case 6: total -= n; case 7: total *= n; case 8: total /= n; case 9: total %= n; default: total += n } }
					}
					return total, nil
				}}/*line p.go:15:2*/
			`,
		},
		{
			name: "constant expressions cannot call functions and stay out",
			src: `package p

const kib = 1 << 10

var sizes = [2 + 1]int{}

func half(n float64) float64 { return n / 2 }
`,
			want: `
				package p

				const kib = 1 << 10

				var sizes = [2 + 1]int{}

				func half(n float64) float64 { if _mutOff(5, 7) { return n / 2 } else {/*line p.go:7:31*/ return _mutArithF(n, 2, _mutQUO, [5]int{5, 6, 7, 0, 0}) }}/*line p.go:7:46*/
			`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "p.go", tt.src, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
			conf := types.Config{Importer: stdImporter}
			if _, err := conf.Check("p", fset, []*ast.File{f}, info); err != nil {
				t.Fatal(err)
			}
			ms := mutant.Find(fset, f, []byte(tt.src), info)
			out, _ := mutant.Embed(fset, f, []byte(tt.src), info, sequential(len(ms)))
			xtesting.EqualText(t, string(out), tt.want)
		})
	}
}

func TestWithGo121(t *testing.T) {
	xtesting.EqualText(t, string(mutant.WithGo121([]byte("// Package p does things.\npackage p\n"), "/src/p/p.go")), `
		//go:build go1.21

		//line /src/p/p.go:1
		// Package p does things.
		package p
	`)
	xtesting.EqualText(t, string(mutant.WithGo121([]byte("//go:build linux\n\npackage p\n"), "/src/p/p.go")), `
		//go:build (linux) && go1.21

		package p
	`)
}

// TestEmbedTypeChecks embeds every mutant of a few standard library
// packages and type-checks the result with the runtime file: one bad
// rewrite would break the build of a whole package.
func TestEmbedTypeChecks(t *testing.T) {
	t.Parallel()
	roots := []string{"net/url", "encoding/csv", "text/tabwriter", "path", "bufio"}
	pkgs, err := loadStdlib()
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]*types.Package{}
	packages.Visit(pkgs, nil, func(p *packages.Package) { byPath[p.PkgPath] = p.Types })
	for _, pkg := range pkgs {
		if !slices.Contains(roots, pkg.PkgPath) {
			continue
		}
		t.Run(pkg.PkgPath, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			var files []*ast.File
			total, embedded := 0, 0
			for _, f := range pkg.Syntax {
				name := pkg.Fset.File(f.Pos()).Name()
				src, err := os.ReadFile(name)
				if err != nil {
					t.Fatal(err)
				}
				ms := mutant.Find(pkg.Fset, f, src, pkg.TypesInfo)
				out, ok := mutant.Embed(pkg.Fset, f, src, pkg.TypesInfo, sequential(len(ms)))
				total += len(ok)
				for _, e := range ok {
					if e {
						embedded++
					}
				}
				g, err := parser.ParseFile(fset, filepath.Base(name), out, parser.ParseComments)
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				files = append(files, g)
			}
			rt, err := parser.ParseFile(fset, "zz_mutant.go", mutant.Runtime(pkg.Name), 0)
			if err != nil {
				t.Fatal(err)
			}
			conf := types.Config{Importer: importerFunc(func(path string) (*types.Package, error) { return byPath[path], nil })}
			var errs []error
			conf.Error = func(err error) { errs = append(errs, err) }
			_, _ = conf.Check(pkg.PkgPath, fset, append(files, rt), nil)
			for _, err := range errs[:min(len(errs), 10)] {
				t.Error(err)
			}
			t.Logf("embedded %d of %d mutants", embedded, total)
			if embedded < total*9/10 {
				t.Errorf("embedded %d of %d mutants, want at least 90%%", embedded, total)
			}
		})
	}
}

type importerFunc func(path string) (*types.Package, error)

func (f importerFunc) Import(path string) (*types.Package, error) { return f(path) }

// TestEmbedInfects runs a program built from Embed once with ReachEnvVar
// set and prints, for each mutant, whether its site ran and whether it
// would have changed a value there.
func TestEmbedInfects(t *testing.T) {
	t.Parallel()
	// Ids past the runtime's table of seen ids take another path.
	for _, base := range []int{0, 70000} {
		t.Run(fmt.Sprint("ids from ", base+1), func(t *testing.T) { embedInfects(t, base) })
	}
}

func embedInfects(t *testing.T, base int) {
	src := `package main

import (
	"errors"
	"fmt"
	"math"
)

func less(a, b int) bool  { return a < b }
func both(a, b bool) bool { return a && b }
func either(a, b bool) bool {
	return a || b
}
func half(x float64) float64 { return x / 2 }
func div(a, b int) int        { return a / b }
func check(err error) error {
	if err != nil {
		return err
	}
	return nil
}
func never(a int) int { return a + 1 }

var calls int

func next() bool { calls++; return true }

// The original calls next; if true and true || next() would not.
func skips() bool {
	if next() {
		return true || next()
	}
	return false
}
func zero(a, b int) int   { return a % b }
func nan(x float64) bool  { return x < x }
func sign(x float64) float64 { return x - x }
func sum(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

func main() {
	fmt.Println(less(1, 1), both(false, true), either(false, false), half(4), div(4, 4), check(nil), check(errors.New("x")))
	fmt.Println(skips(), calls, nan(math.NaN()), sign(math.Copysign(0, -1)), sum([]int{0}))
	defer func() { _ = recover() }()
	fmt.Println(zero(1, 0))
	if false {
		never(1)
	}
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	if _, err := (&types.Config{Importer: stdImporter}).Check("main", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	ms := mutant.Find(fset, f, []byte(src), info)
	out, _ := mutant.Embed(fset, f, []byte(src), info, offset(sequential(len(ms)), base))
	dir := t.TempDir()
	for name, content := range map[string][]byte{"go.mod": []byte("module m\n\ngo 1.21\n"), "main.go": out, "runtime.go": mutant.Runtime("main")} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reach := filepath.Join(dir, "reach")
	cmd := exec.CommandContext(t.Context(), "go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), mutant.ReachEnvVar+"="+reach)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	data, _ := os.ReadFile(reach)
	marks := map[string]bool{}
	for line := range strings.FieldsSeq(string(data)) {
		marks[line] = true
	}
	var b strings.Builder
	for i, m := range ms {
		state := "not reached"
		switch {
		case marks[fmt.Sprint(-(base + i + 1))]:
			state = "infected"
		case marks[fmt.Sprint(base+i+1)]:
			state = "reached, not infected"
		}
		fmt.Fprintf(&b, "%d %s → %s: %s\n", m.Pos.Line, m.Original, cmp.Or(m.Mutated, "(deleted)"), state)
	}
	xtesting.EqualText(t, b.String(), `
		9 a < b → a <= b: infected
		9 a < b → a > b: reached, not infected
		9 a < b → a >= b: infected
		9 a < b → a == b: infected
		9 a < b → a != b: reached, not infected
		10 a && b → a || b: infected
		12 a || b → a && b: reached, not infected
		14 x / 2 → x + 2: infected
		14 x / 2 → x - 2: reached, not infected
		14 x / 2 → x * 2: infected
		15 a / b → a + b: infected
		15 a / b → a - b: infected
		15 a / b → a * b: infected
		15 a / b → a % b: infected
		17 err != nil → true: infected
		17 err != nil → false: infected
		17 err != nil → err == nil: infected
		18 return err → return nil: infected
		22 a + 1 → a - 1: not reached
		22 a + 1 → a * 1: not reached
		22 a + 1 → a / 1: not reached
		22 a + 1 → a % 1: not reached
		26 calls++ → (deleted): infected
		30 next() → true: infected
		30 next() → false: infected
		31 true || next() → true && next(): infected
		35 a % b → a + b: infected
		35 a % b → a - b: infected
		35 a % b → a * b: infected
		35 a % b → a / b: infected
		36 x < x → x <= x: reached, not infected
		36 x < x → x > x: reached, not infected
		36 x < x → x >= x: reached, not infected
		36 x < x → x == x: reached, not infected
		36 x < x → x != x: infected
		37 x - x → x + x: infected
		37 x - x → x * x: reached, not infected
		37 x - x → x / x: infected
		41 total += x → _ = x: infected
		41 total += x → total -= x: infected
		41 total += x → total *= x: infected
		41 total += x → total /= x: infected
		41 total += x → total %= x: infected
		47 fmt.Println(less(1, 1), both(false, true), either(false, false), half(4), div(4, 4), check(nil), check(errors.New("x"))) → _ = errors.New: infected
		48 fmt.Println(skips(), calls, nan(math.NaN()), sign(math.Copysign(0, -1)), sum([]int{0})) → _ = math.NaN: infected
		49 _ = recover() → (deleted): infected
		50 fmt.Println(zero(1, 0)) → (deleted): infected
		51 false → true: not reached
		51 false → false: not reached
		52 never(1) → (deleted): not reached
	`)
}

func offset(ids []int, base int) []int {
	for i := range ids {
		ids[i] += base
	}
	return ids
}

// TestEmbedPure: which conditions a mutant may skip without changing more
// than their value. A call other than len, cap or a conversion, or a
// receive, might do something else, so its mutants count as infected.
func TestEmbedPure(t *testing.T) {
	src := `package p

type T struct{ n int }

func (t T) ok() bool { return t.n > 0 }

func f(x int, xs []int, t T, ch chan bool) {
	if int64(x) > 0 {
	}
	if len(xs) > 0 {
	}
	if cap(xs) > 0 {
	}
	if -x < 0 {
	}
	if t.n > 0 && !(x > 1) {
	}
	if g() {
	}
	if t.ok() {
	}
	if <-ch {
	}
	if min(x, 1) > 0 {
	}
}

func g() bool { return true }
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
	ms := mutant.Find(fset, f, []byte(src), info)
	out, _ := mutant.Embed(fset, f, []byte(src), info, sequential(len(ms)))
	var b strings.Builder
	for line := range strings.Lines(string(out)) {
		if strings.Contains(line, "_mutCond(") {
			cond, pure := line[strings.Index(line, "return ")+len("return "):], strings.HasSuffix(strings.TrimSpace(line), "true) {")
			cond = cond[:strings.LastIndex(cond, " }, ")]
			fmt.Fprintf(&b, "%v %s\n", pure, cond)
		}
	}
	xtesting.EqualText(t, b.String(), `
		true _mutOrd(int64(x), 0, _mutGTR, [6]int{8, 9, 0, 10, 11, 12})
		true _mutOrd(len(xs), 0, _mutGTR, [6]int{15, 16, 0, 17, 18, 19})
		true _mutOrd(cap(xs), 0, _mutGTR, [6]int{22, 23, 0, 24, 25, 26})
		true _mutOrd(-x, 0, _mutLSS, [6]int{0, 29, 30, 31, 32, 33})
		true _mutAnd(_mutOrd(t.n, 0, _mutGTR, [6]int{36, 37, 0, 38, 39, 40}), func() bool { return !(_mutOrd(x, 1, _mutGTR, [6]int{42, 43, 0, 44, 45, 46})) }, 41, true)
		false g()
		false t.ok()
		false <-ch
		false _mutOrd(min(x, 1), 0, _mutGTR, [6]int{55, 56, 0, 57, 58, 59})
	`)
}

// TestEmbedClones: a function with a mutant runs as written unless one of
// its mutants is active, but for a body with a label or one that :=
// reuses a named result, where the clone's extra block would change what
// the code means. Every line keeps its number in either copy: a panic in
// the mutated copy and one in the original name the line in the source.
func TestEmbedClones(t *testing.T) {
	t.Parallel()
	src := `package main

import (
	"fmt"
	"os"
	"strconv"
)

func at(xs []int, i int) int {
	if i < len(xs) {
		return xs[i]
	}
	return -1
}

func loop(n int) int {
outer:
	for i := 0; i < n; i++ {
		continue outer
	}
	return n - 1
}

func parse(s string) (n int, err error) {
	n, err = strconv.Atoi(s)
	m, err := strconv.Atoi(s + "0")
	return n + m, err
}

// shadow reassigns x at its top level, which get sees.
func shadow(x int) int {
	get := func() int { return x }
	y, x := 1, x+1
	return get()*10 + y + x
}

var calls int

func tick() {calls++}

func main() {
	i, _ := strconv.Atoi(os.Args[1])
	fmt.Println(at([]int{1}, i), loop(2))
	fmt.Println(parse("1"))
	tick()
	fmt.Println(shadow(5), calls)
}
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	if _, err := (&types.Config{Importer: stdImporter}).Check("main", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	ms := mutant.Find(fset, f, []byte(src), info)
	out, _ := mutant.Embed(fset, f, []byte(src), info, sequential(len(ms)))
	var cloned []string
	for line := range strings.Lines(string(out)) {
		if strings.HasPrefix(line, "func ") {
			name := line[len("func "):strings.Index(line, "(")]
			cloned = append(cloned, fmt.Sprintf("%s %v", name, strings.Contains(line, "_mutOff(")))
		}
	}
	xtesting.EqualText(t, strings.Join(cloned, "\n"), `
		at true
		loop false
		parse false
		shadow false
		tick true
		main true
	`)

	dir := t.TempDir()
	for name, content := range map[string][]byte{"go.mod": []byte("module m\n\ngo 1.21\n"), "main.go": out, "runtime.go": mutant.Runtime("main")} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "m")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	// i < len(xs) → i <= len(xs) indexes past the end at line 11.
	var boundary int
	for k, m := range ms {
		if m.Mutated == "i <= len(xs)" {
			boundary = k + 1
		}
	}
	panics := func(mutantID int, arg string) string {
		cmd := exec.CommandContext(t.Context(), bin, arg)
		cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%d", mutant.EnvVar, mutantID))
		out, _ := cmd.CombinedOutput()
		for line := range strings.Lines(string(out)) {
			if strings.Contains(line, "main.go:") {
				return strings.TrimSpace(line[strings.LastIndex(line, "/")+1:])
			}
		}
		return "no panic: " + string(out)
	}
	// at(xs, -1) panics in the original copy, i <= len(xs) in the mutated
	// one: both at line 11.
	got := strings.Fields(panics(boundary, "1"))[0] + " " + strings.Fields(panics(0, "-1"))[0]
	xtesting.EqualText(t, got, "main.go:11 main.go:11")

	// tick's body is one site: deleting calls++ must take effect.
	run := func(mutantID int) string {
		cmd := exec.CommandContext(t.Context(), bin, "0")
		cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%d", mutant.EnvVar, mutantID))
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	deleted := 0
	for k, m := range ms {
		if m.Original == "calls++" {
			deleted = k + 1
		}
	}
	xtesting.EqualText(t, run(0)+run(deleted), `
		1 1
		11 <nil>
		67 1
		1 1
		11 <nil>
		67 0
	`)
}
