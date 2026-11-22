// Package mutant finds the mutants of a Go source file: small, deliberate
// changes such as > to >= that a good test suite should notice.
//
// Find is exhaustive on purpose. It is the reference that cheaper strategies,
// such as one mutant per line or a subset of operators, are measured against,
// so it does not filter for usefulness.
package mutant

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
)

// Operator names the kind of change a mutant makes.
type Operator string

const (
	// ROR replaces a relational operator: > becomes <, <=, >=, == or !=.
	ROR Operator = "ROR"
	// COR swaps && and || and replaces an if condition with true or false.
	COR Operator = "COR"
	// AOR replaces an arithmetic operator: - becomes +, *, / or %.
	AOR Operator = "AOR"
	// SDL deletes a statement.
	SDL Operator = "SDL"
	// ERR flips err != nil and returns nil instead of an error.
	ERR Operator = "ERR"
)

// Mutant is one change to one file.
type Mutant struct {
	Pos      token.Position
	Operator Operator
	// Node is the kind of syntax node changed, such as BinaryExpr or IfStmt.
	Node string
	// Original and Mutated are the smallest enclosing expression or
	// statement before and after the change; Mutated is empty for SDL.
	Original string
	Mutated  string

	start, end int
	text       string
}

// Apply returns a copy of src with the mutant applied. src must be the
// source Find was given.
func (m Mutant) Apply(src []byte) []byte {
	out := make([]byte, 0, len(src)-(m.end-m.start)+len(m.text))
	out = append(out, src[:m.start]...)
	out = append(out, m.text...)
	return append(out, src[m.end:]...)
}

func (m Mutant) String() string {
	mutated := m.Mutated
	if mutated == "" {
		mutated = "(deleted)"
	}
	return fmt.Sprintf("%s:%d:%d %s %s → %s", filepath.Base(m.Pos.Filename), m.Pos.Line, m.Pos.Column, m.Operator, m.Original, mutated)
}

// Find returns the mutants of file, ordered by position. src is the file's
// source and info must hold the type of every expression in file.
func Find(fset *token.FileSet, file *ast.File, src []byte, info *types.Info) []Mutant {
	f := finder{fset: fset, src: src, info: info}
	ast.Inspect(file, f.visit)
	slices.SortStableFunc(f.found, func(a, b Mutant) int { return a.start - b.start })
	return f.found
}

type finder struct {
	fset  *token.FileSet
	src   []byte
	info  *types.Info
	found []Mutant
}

var (
	relational = []token.Token{token.LSS, token.LEQ, token.GTR, token.GEQ, token.EQL, token.NEQ}
	arithmetic = []token.Token{token.ADD, token.SUB, token.MUL, token.QUO, token.REM}
	errorType  = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
)

func (f *finder) visit(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.IfStmt:
		for _, lit := range []string{"true", "false"} {
			f.add(COR, n, n.Cond, n.Cond.Pos(), n.Cond.End(), lit)
		}
	case *ast.BinaryExpr:
		f.binary(n)
	case *ast.AssignStmt:
		f.assign(n)
	case *ast.ReturnStmt:
		if len(n.Results) == 0 {
			break
		}
		last := n.Results[len(n.Results)-1]
		if tv := f.info.Types[last]; !tv.IsNil() && implementsError(tv.Type) {
			f.add(ERR, n, n, last.Pos(), last.End(), "nil")
		}
	// Statements are deleted only from statement lists: deleting a for
	// loop's post statement or a select case's receive breaks the syntax.
	case *ast.BlockStmt:
		f.deleteEach(n.List)
	case *ast.CaseClause:
		f.deleteEach(n.Body)
	case *ast.CommClause:
		f.deleteEach(n.Body)
	}
	return true
}

func (f *finder) binary(n *ast.BinaryExpr) {
	op, end := n.OpPos, n.OpPos+token.Pos(len(n.Op.String()))
	switch {
	case n.Op == token.LAND:
		f.add(COR, n, n, op, end, token.LOR.String())
	case n.Op == token.LOR:
		f.add(COR, n, n, op, end, token.LAND.String())
	case (n.Op == token.EQL || n.Op == token.NEQ) && (f.isErr(n.X) && f.isNil(n.Y) || f.isNil(n.X) && f.isErr(n.Y)):
		f.add(ERR, n, n, op, end, negate(n.Op).String())
	case n.Op == token.EQL || n.Op == token.NEQ:
		if !f.ordered(n.X) {
			f.add(ROR, n, n, op, end, negate(n.Op).String())
			break
		}
		f.replaceOp(ROR, n, op, n.Op, relational)
	case slices.Contains(relational, n.Op):
		f.replaceOp(ROR, n, op, n.Op, relational)
	case slices.Contains(arithmetic, n.Op):
		if ops := f.arithmeticFor(n); ops != nil {
			f.replaceOp(AOR, n, op, n.Op, ops)
		}
	}
}

// assign mutates op-assignments such as += the way binary mutates +.
func (f *finder) assign(n *ast.AssignStmt) {
	op, ok := assignOps[n.Tok]
	if !ok {
		return
	}
	ops := f.arithmeticFor(n.Lhs[0])
	for _, alt := range ops {
		if alt != op {
			f.add(AOR, n, n, n.TokPos, n.TokPos+token.Pos(len(n.Tok.String())), alt.String()+"=")
		}
	}
}

var assignOps = map[token.Token]token.Token{
	token.ADD_ASSIGN: token.ADD,
	token.SUB_ASSIGN: token.SUB,
	token.MUL_ASSIGN: token.MUL,
	token.QUO_ASSIGN: token.QUO,
	token.REM_ASSIGN: token.REM,
}

// arithmeticFor returns the operators that keep an expression of e's type
// compiling: none for strings, where + concatenates, and no % for floats.
func (f *finder) arithmeticFor(e ast.Expr) []token.Token {
	b, ok := f.basic(e)
	switch {
	case !ok || b.Info()&types.IsNumeric == 0:
		return nil
	case b.Info()&types.IsInteger == 0:
		return arithmetic[:4]
	default:
		return arithmetic
	}
}

func (f *finder) replaceOp(o Operator, n *ast.BinaryExpr, pos token.Pos, op token.Token, alts []token.Token) {
	for _, alt := range alts {
		if alt != op {
			f.add(o, n, n, pos, pos+token.Pos(len(op.String())), alt.String())
		}
	}
}

// := is left alone: deleting it leaves the names it declares undefined.
func (f *finder) deleteEach(list []ast.Stmt) {
	for _, s := range list {
		switch s := s.(type) {
		case *ast.ExprStmt, *ast.IncDecStmt:
			f.add(SDL, s, s, s.Pos(), s.End(), "")
		case *ast.AssignStmt:
			if s.Tok != token.DEFINE {
				f.add(SDL, s, s, s.Pos(), s.End(), "")
			}
		}
	}
}

// add records a mutant that replaces [from, to) with text. ctx is the node
// shown as Original; node names the kind of node changed.
func (f *finder) add(o Operator, node ast.Node, ctx ast.Node, from, to token.Pos, text string) {
	start, end := f.offset(from), f.offset(to)
	cs, ce := f.offset(ctx.Pos()), f.offset(ctx.End())
	original := string(f.src[cs:ce])
	mutated := ""
	if o != SDL {
		mutated = original[:start-cs] + text + original[end-cs:]
	}
	f.found = append(f.found, Mutant{
		Pos:      f.fset.Position(from),
		Operator: o,
		Node:     fmt.Sprintf("%T", node)[len("*ast."):],
		Original: original,
		Mutated:  mutated,
		start:    start,
		end:      end,
		text:     text,
	})
}

func (f *finder) offset(p token.Pos) int { return f.fset.Position(p).Offset }

func (f *finder) basic(e ast.Expr) (*types.Basic, bool) {
	t := f.info.TypeOf(e)
	if t == nil {
		return nil, false
	}
	b, ok := t.Underlying().(*types.Basic)
	return b, ok
}

func (f *finder) ordered(e ast.Expr) bool {
	b, ok := f.basic(e)
	return ok && b.Info()&types.IsOrdered != 0
}

func (f *finder) isNil(e ast.Expr) bool { return f.info.Types[e].IsNil() }

func (f *finder) isErr(e ast.Expr) bool {
	t := f.info.TypeOf(e)
	return t != nil && types.IsInterface(t) && implementsError(t)
}

func implementsError(t types.Type) bool { return t != nil && types.Implements(t, errorType) }

func negate(op token.Token) token.Token {
	if op == token.EQL {
		return token.NEQ
	}
	return token.EQL
}
