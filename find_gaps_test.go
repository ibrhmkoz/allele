package mutant_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/ibrhmkoz/go-kit/mutant"
	"github.com/ibrhmkoz/go-kit/xtesting"
)

func findWithImporter(t testing.TB, name, src string, imp types.Importer) []mutant.Mutant {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Defs:  map[*ast.Ident]types.Object{},
		Uses:  map[*ast.Ident]types.Object{},
	}
	if _, err := (&types.Config{Importer: imp}).Check("p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	return mutant.Find(fset, f, []byte(src), info)
}

func TestFindMultilineAndPrecedence(t *testing.T) {
	t.Parallel()
	src := `package p

func f(a, b, c int, p *int) bool {
	_ = a + b + c
	if a > 0 &&
		b > 0 {
		return nil == p
	}
	return false
}
`
	ms := find(t, "prec.go", src)
	var lines []string
	for _, m := range ms {
		if m.Operator == mutant.ERR || m.Original == "a + b + c" && m.Mutated == "(a + b) * c" ||
			m.Node == "IfStmt" && m.Mutated == "true" || m.Original == "nil == p" {
			lines = append(lines, m.String())
		}
	}
	xtesting.EqualText(t, strings.Join(lines, "\n"), `
		prec.go:4:12 AOR a + b + c → (a + b) * c
		prec.go:5:5 COR a > 0 && … → true
		prec.go:7:14 ROR nil == p → nil != p
	`)
}

func TestFindAridGaps(t *testing.T) {
	t.Parallel()
	// Synthetic packages: example.com/catalog/foo has "log" only in a parent
	// path segment (not its package name); example.com/metrics has "metrics"
	// without "prometheus".
	fooPkg := types.NewPackage("example.com/catalog/foo", "foo")
	emptySig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	fooPkg.Scope().Insert(types.NewFunc(token.NoPos, fooPkg, "Info", emptySig))
	clientNamed := types.NewNamed(types.NewTypeName(token.NoPos, fooPkg, "Client", nil), types.NewStruct(nil, nil), nil)
	recv := types.NewVar(token.NoPos, fooPkg, "c", clientNamed)
	clientNamed.AddMethod(types.NewFunc(token.NoPos, fooPkg, "Info", types.NewSignatureType(recv, nil, nil, nil, nil, false)))
	fooPkg.Scope().Insert(clientNamed.Obj())
	fooPkg.MarkComplete()

	metricsPkg := types.NewPackage("example.com/metrics", "metrics")
	metricsPkg.Scope().Insert(types.NewFunc(token.NoPos, metricsPkg, "Inc", emptySig))
	metricsPkg.MarkComplete()

	imp := importerFunc(func(path string) (*types.Package, error) {
		switch path {
		case "example.com/catalog/foo":
			return fooPkg, nil
		case "example.com/metrics":
			return metricsPkg, nil
		}
		return stdImporter.Import(path)
	})

	src := `package p

import (
	"example.com/catalog/foo"
	"example.com/metrics"
	"log"
)

func f(x int, c foo.Client, m map[int]int) {
	switch x {
	case x + 1:
		log.Println(x)
	}
	foo.Info()
	c.Info()
	metrics.Inc()
	println(m, x+1)
}
`
	ms := findWithImporter(t, "arid_gaps.go", src, imp)
	var lines []string
	for _, m := range ms {
		if m.Operator == mutant.SDL || m.Mutated == "x - 1" || m.Mutated == "x-1" {
			lines = append(lines, fmt.Sprintf("%d %s %q", m.Pos.Line, m.Operator, m.Arid))
		}
	}
	xtesting.EqualText(t, strings.Join(lines, "\n"), `
		11 AOR "only arid statements"
		12 SDL "logging"
		14 SDL ""
		15 SDL ""
		16 SDL "metrics"
		17 SDL ""
		17 AOR ""
	`)
}

func TestFindRepairGaps(t *testing.T) {
	t.Parallel()
	src := `package p

import "math"

type T struct{ x int }

func (r T) recvOnly() {
	if r.x > 0 {
		return
	}
}

func repairs(n int, use func(int), mkErr func() error) error {
	var assigned int
	assigned = 1
	if assigned > 0 {
		return nil
	}
	if n < math.MaxInt {
		return nil
	}
	a, b, c, d, e := 1, 2, 3, 4, 5
	if a+b+c+d+e > 0 {
		return nil
	}
	kept := 1
	use(kept)
	err := mkErr()
	return err
}
`
	ms := find(t, "repairs.go", src)
	var lines []string
	for _, m := range ms {
		switch {
		case m.Node == "IfStmt" && strings.HasSuffix(m.Mutated, "true") || strings.HasSuffix(m.Mutated, "true }()"):
			lines = append(lines, fmt.Sprintf("L%d %s repaired=%v %s", m.Pos.Line, m.Operator, m.Repaired, m.Mutated))
		case m.Pos.Line == 27 && m.Operator == mutant.SDL:
			lines = append(lines, fmt.Sprintf("L%d %s repaired=%v %s", m.Pos.Line, m.Operator, m.Repaired, m.Mutated))
		case m.Pos.Line == 29 && m.Operator == mutant.ERR:
			lines = append(lines, fmt.Sprintf("L%d %s repaired=%v %s", m.Pos.Line, m.Operator, m.Repaired, m.Mutated))
		}
	}
	xtesting.EqualText(t, strings.Join(lines, "\n"), `
		L8 COR repaired=false true
		L16 COR repaired=true func() bool { _ = assigned; return true }()
		L19 COR repaired=true func() bool { _ = math.MaxInt; return true }()
		L23 COR repaired=true func() bool { _, _, _, _, _ = a, b, c, d, e; return true }()
		L27 SDL repaired=true _ = kept
		L29 ERR repaired=true return func() error { _ = err; return nil }()
	`)
}

func TestFindEquivalenceGaps(t *testing.T) {
	t.Parallel()
	src := `package p

func f(x int, s string, xs []int, y int) {
	var X int
	X = 1
	_ = X
	_ = x != 0
	_ = s != "a"
	_ = len(xs) != -1
	_ = min(x, y) != 0
}
`
	ms := find(t, "equiv_gaps.go", src)
	var lines []string
	for _, m := range ms {
		if m.Operator == mutant.SDL && m.Pos.Line <= 6 ||
			m.Mutated == "x > 0" || m.Mutated == `s > "a"` ||
			m.Mutated == "len(xs) > -1" || m.Mutated == "min(x, y) > 0" {
			lines = append(lines, fmt.Sprintf("L%d %s eq=%q", m.Pos.Line, m.Mutated, m.Equivalent))
		}
	}
	xtesting.EqualText(t, strings.Join(lines, "\n"), `
		L5  eq=""
		L6  eq="blank assignment that cannot panic"
		L7 x > 0 eq=""
		L8 s > "a" eq=""
		L9 len(xs) > -1 eq=""
		L10 min(x, y) > 0 eq=""
	`)
}
