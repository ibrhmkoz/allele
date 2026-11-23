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

func embedCheck(t testing.TB, src string, makeIDs func(ms []mutant.Mutant) []int) ([]mutant.Mutant, []byte, []bool) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Defs:  map[*ast.Ident]types.Object{},
		Uses:  map[*ast.Ident]types.Object{},
	}
	if _, err := (&types.Config{Importer: stdImporter}).Check("p", fset, []*ast.File{f}, info); err != nil {
		t.Fatal(err)
	}
	ms := mutant.Find(fset, f, []byte(src), info)
	out, emb := mutant.Embed(fset, f, []byte(src), info, makeIDs(ms))
	return ms, out, emb
}

func TestEmbedGaps(t *testing.T) {
	t.Parallel()

	t.Run("short and zero ids", func(t *testing.T) {
		t.Parallel()
		src := "package p\nfunc f(x int) bool { return x > 0 }\n"
		ms, _, emb := embedCheck(t, src, func(_ []mutant.Mutant) []int { return []int{0, 2} })
		var got []string
		for i, m := range ms {
			got = append(got, fmt.Sprintf("%s embedded=%v", m.Mutated, emb[i]))
		}
		xtesting.EqualText(t, strings.Join(got, "\n"), `
			x < 0 embedded=false
			x <= 0 embedded=true
			x >= 0 embedded=false
			x == 0 embedded=false
			x != 0 embedded=false
		`)
	})

	t.Run("embeddability boundaries", func(t *testing.T) {
		t.Parallel()
		src := `package p

type myErr struct{}
func (myErr) Error() string { return "" }
func concreteErr() myErr { return myErr{} }

type flag bool
func namedAnd(a, b flag) flag { return a && b }

func drain(ch <-chan int) { <-ch }

func boom() int { panic("boom") }

func clauses(x int, ch <-chan int) int {
	switch x {
	case 1:
		x += 1
	}
	select {
	case <-ch:
		x += 2
	default:
	}
	return x
}

func emptyRes() () { return }
`
		ms, _, emb := embedCheck(t, src, func(ms []mutant.Mutant) []int { return sequential(len(ms)) })
		var got []string
		for i, m := range ms {
			if m.Pos.Line == 5 || m.Pos.Line == 8 || m.Pos.Line == 10 || m.Pos.Line == 12 ||
				(m.Operator == mutant.AOR && m.Mutated == "x -= 1") ||
				(m.Operator == mutant.AOR && m.Mutated == "x -= 2") {
				got = append(got, fmt.Sprintf("L%d %s %s embedded=%v", m.Pos.Line, m.Operator, m.Original, emb[i]))
			}
		}
		xtesting.EqualText(t, strings.Join(got, "\n"), `
			L5 ERR return myErr{} embedded=false
			L8 COR a && b embedded=false
			L10 SDL <-ch embedded=true
			L12 SDL panic("boom") embedded=false
			L17 AOR x += 1 embedded=true
			L21 AOR x += 2 embedded=true
		`)
	})

	t.Run("WithGo121 comment before build tag", func(t *testing.T) {
		t.Parallel()
		in := "// Copyright notice\n\n//go:build linux\n\npackage p\n"
		got := mutant.WithGo121([]byte(in), "/abs/p.go")
		xtesting.EqualText(t, string(got), `
			// Copyright notice

			//go:build (linux) && go1.21

			package p
		`)
	})
}
