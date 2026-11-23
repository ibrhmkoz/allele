package mutant_test

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
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

		func Discount(price int, vip bool) int {
			if _mutCond(func() bool { return _mutAnd(_mutOrd(price, 100, _mutGTR, [6]int{3, 4, 0, 5, 6, 7}), func() bool { return vip }, 8) }, 1, 2) {
				return _mutArith(price, 20, _mutSUB, [5]int{9, 0, 10, 11, 12})
			}
			return price
		}
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

				func parse(xs []string) (int, error) {
					total := 0
					for _, x := range xs {
						n, err := strconv.Atoi(x)
						if _mutCond(func() bool { return _mutNot(err != nil, 3) }, 1, 2) {
							return 0, _mutErr(err, 4)
						}
						if !_mutOn(5) { switch _mutAt(6, 7, 8, 9) { case 6: total -= n; case 7: total *= n; case 8: total /= n; case 9: total %= n; default: total += n } }
					}
					return total, nil
				}
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

				func half(n float64) float64 { return _mutArithF(n, 2, _mutQUO, [5]int{5, 6, 7, 0, 0}) }
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
			conf := types.Config{Importer: importer.Default()}
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
	roots := []string{"net/url", "encoding/csv", "text/tabwriter", "path", "bufio"}
	cfg := &packages.Config{Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps}
	pkgs, err := packages.Load(cfg, append([]string{"cmp", "os", "strconv"}, roots...)...)
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
