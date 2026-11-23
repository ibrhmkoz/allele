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
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
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
	// Equivalent names the rule by which the mutant cannot change what the
	// program does, if one applies. Find keeps such mutants so a kill
	// matrix can check the rule: a killed one would prove it wrong.
	Equivalent string
	// Repaired is set when the change removes the only use of a local
	// variable or an import, which Go rejects, and the mutant keeps it used
	// with _ = x.
	Repaired bool

	start, end int
	text       string
	// node is the node the mutant changes and alt what it changes it to:
	// an operator, true or false for a condition, nil for a returned
	// error, "" for a deleted statement. Embed builds on them.
	node ast.Node
	alt  string
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
	s := fmt.Sprintf("%s:%d:%d %s %s → %s", filepath.Base(m.Pos.Filename), m.Pos.Line, m.Pos.Column, m.Operator, m.Original, mutated)
	if m.Equivalent != "" {
		s += " [equivalent: " + m.Equivalent + "]"
	}
	return s
}

// Find returns the mutants of file, ordered by position. src is the file's
// source and info must hold the Types, Defs and Uses of file.
func Find(fset *token.FileSet, file *ast.File, src []byte, info *types.Info) []Mutant {
	f := finder{fset: fset, src: src, info: info}
	f.indexUses(file)
	ast.Inspect(file, f.visit)
	slices.SortStableFunc(f.found, func(a, b Mutant) int { return a.Pos.Offset - b.Pos.Offset })
	return f.found
}

type finder struct {
	fset  *token.FileSet
	src   []byte
	info  *types.Info
	found []Mutant
	// uses holds where each local variable and each import of the file is
	// used, in the compiler's sense: x = v is not a use of x, x++ and
	// x += v are.
	uses map[types.Object][]use
	// parents maps an operand that is itself a binary expression to the
	// expression it is an operand of.
	parents map[ast.Expr]*ast.BinaryExpr
}

// use is one use of a local or an import. expr is an expression that
// uses it the same way: the variable's name, or pkg.Name for an import
// used for a value; "" for an import used only for a type.
type use struct {
	pos  token.Pos
	expr string
}

func (f *finder) indexUses(file *ast.File) {
	assigned := map[*ast.Ident]bool{}
	params := map[*types.Var]bool{}
	selected := map[*ast.Ident]string{} // package ident of pkg.Name → "pkg.Name" when Name is a value
	f.parents = map[ast.Expr]*ast.BinaryExpr{}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.BinaryExpr:
			f.parents[n.X], f.parents[n.Y] = n, n
		case *ast.AssignStmt:
			// x = v and x := v do not use x; x += v and x++ do.
			if n.Tok != token.ASSIGN && n.Tok != token.DEFINE {
				break
			}
			for _, l := range n.Lhs {
				if id, ok := ast.Unparen(l).(*ast.Ident); ok {
					assigned[id] = true
				}
			}
		case *ast.FuncType:
			// Go allows unused parameters and results.
			for _, fields := range []*ast.FieldList{n.Params, n.Results} {
				if fields == nil {
					continue
				}
				for _, field := range fields.List {
					for _, id := range field.Names {
						if v, ok := f.info.Defs[id].(*types.Var); ok {
							params[v] = true
						}
					}
				}
			}
		case *ast.SelectorExpr:
			if x, ok := n.X.(*ast.Ident); ok {
				switch f.info.Uses[n.Sel].(type) {
				case *types.Func, *types.Var, *types.Const:
					selected[x] = x.Name + "." + n.Sel.Name
				}
			}
		}
		return true
	})
	f.uses = map[types.Object][]use{}
	for id, obj := range f.info.Uses {
		if id.Pos() < file.Pos() || id.Pos() >= file.End() {
			continue
		}
		switch o := obj.(type) {
		case *types.PkgName:
			f.uses[o] = append(f.uses[o], use{id.Pos(), selected[id]})
		case *types.Var:
			if assigned[id] || params[o] || o.IsField() || o.Pkg() == nil || o.Parent() == o.Pkg().Scope() {
				continue
			}
			f.uses[o] = append(f.uses[o], use{id.Pos(), o.Name()})
		}
	}
}

// orphans returns expressions that keep used, in order of first use, the
// locals and imports declared outside n whose every use lies in n:
// removing n would leave them unused, which Go rejects. ok is false when
// one is an import used in n only for a type, which no expression can use.
func (f *finder) orphans(n ast.Node) (exprs []string, ok bool) {
	type orphan struct {
		expr  string
		first token.Pos
	}
	var found []orphan
	for obj, uses := range f.uses {
		if obj.Pos() >= n.Pos() && obj.Pos() < n.End() {
			continue // declared in n, removed with it
		}
		first, expr := token.NoPos, ""
		for _, u := range uses {
			if u.pos < n.Pos() || u.pos >= n.End() {
				first = token.NoPos
				break
			}
			if first == token.NoPos || u.pos < first {
				first = u.pos
			}
			if expr == "" {
				expr = u.expr
			}
		}
		if first == token.NoPos {
			continue
		}
		if expr == "" {
			return nil, false
		}
		found = append(found, orphan{expr, first})
	}
	slices.SortFunc(found, func(a, b orphan) int { return int(a.first - b.first) })
	for _, o := range found {
		exprs = append(exprs, o.expr)
	}
	return exprs, true
}

// repair returns text, or when n holds the only use of something, a
// function literal of type typ that keeps it used and then returns text.
func (f *finder) repair(n ast.Node, typ, text string) (string, bool) {
	exprs, ok := f.orphans(n)
	if !ok || len(exprs) == 0 {
		return text, false
	}
	return "func() " + typ + " { " + blank(exprs) + "; return " + text + " }()", true
}

// blank returns a statement that uses exprs: _ = a, or _, _ = a, b.
func blank(exprs []string) string {
	return strings.Repeat("_, ", len(exprs)-1) + "_ = " + strings.Join(exprs, ", ")
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
			text, repaired := f.repair(n.Cond, "bool", lit)
			m := f.add(COR, n, n.Cond, n.Cond.Pos(), n.Cond.End(), text)
			m.Repaired, m.alt = repaired, lit
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
			text, repaired := f.repair(last, "error", "nil")
			m := f.add(ERR, n, n, last.Pos(), last.End(), text)
			m.Repaired, m.alt = repaired, "nil"
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
	switch {
	case n.Op == token.LAND:
		f.swap(COR, n, token.LOR)
	case n.Op == token.LOR:
		f.swap(COR, n, token.LAND)
	case (n.Op == token.EQL || n.Op == token.NEQ) && (f.isErr(n.X) && f.isNil(n.Y) || f.isNil(n.X) && f.isErr(n.Y)):
		f.swap(ERR, n, negate(n.Op))
	case n.Op == token.EQL || n.Op == token.NEQ:
		if !f.ordered(n.X) {
			f.swap(ROR, n, negate(n.Op))
			break
		}
		f.replaceOp(ROR, n, relational)
	case slices.Contains(relational, n.Op):
		f.replaceOp(ROR, n, relational)
	case slices.Contains(arithmetic, n.Op):
		if ops := f.arithmeticFor(n); ops != nil {
			f.replaceOp(AOR, n, ops)
		}
	}
}

// swap records the mutant that puts alt in place of n's operator. An
// operator of the same precedence replaces the token alone. Another
// precedence would regroup the tree written as text: in a && b && c,
// which is (a && b) && c, swapping the first && gives a || b && c, read
// as a || (b && c). So the node is rewritten whole, with parentheses where
// the new operator needs them, and the text between the operands kept so
// no line moves.
func (f *finder) swap(o Operator, n *ast.BinaryExpr, alt token.Token) *Mutant {
	if alt.Precedence() == n.Op.Precedence() {
		return f.add(o, n, n, n.OpPos, n.OpPos+token.Pos(len(n.Op.String())), alt.String())
	}
	x, y := f.source(n.X), f.source(n.Y)
	if b, ok := n.X.(*ast.BinaryExpr); ok && b.Op.Precedence() < alt.Precedence() {
		x = "(" + x + ")"
	}
	if b, ok := n.Y.(*ast.BinaryExpr); ok && b.Op.Precedence() <= alt.Precedence() {
		y = "(" + y + ")"
	}
	before := string(f.src[f.offset(n.X.End()):f.offset(n.OpPos)])
	after := string(f.src[f.offset(n.OpPos)+len(n.Op.String()) : f.offset(n.Y.Pos())])
	text := x + before + alt.String() + after + y
	if p, ok := f.parents[n]; ok && (p.X == n && alt.Precedence() < p.Op.Precedence() || p.Y == n && alt.Precedence() <= p.Op.Precedence()) {
		text = "(" + text + ")"
	}
	m := f.add(o, n, n, n.Pos(), n.End(), text)
	m.Pos, m.alt = f.fset.Position(n.OpPos), alt.String()
	return m
}

func (f *finder) source(n ast.Node) string { return string(f.src[f.offset(n.Pos()):f.offset(n.End())]) }

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

func (f *finder) replaceOp(o Operator, n *ast.BinaryExpr, alts []token.Token) {
	for _, alt := range alts {
		if alt != n.Op {
			m := f.swap(o, n, alt)
			if o == ROR {
				m.Equivalent = f.equivalent(n, n.Op, alt)
			}
		}
	}
}

// equivalent returns the rule by which replacing op with alt in n cannot
// change the result, or "".
func (f *finder) equivalent(n *ast.BinaryExpr, op, alt token.Token) string {
	var x ast.Expr
	switch {
	case f.isZero(n.Y):
		x = n.X
	case f.isZero(n.X):
		x, op, alt = n.Y, mirror(op), mirror(alt)
	default:
		return ""
	}
	if !f.nonnegative(x) {
		return ""
	}
	// x >= 0 always, so x != 0 is x > 0 and x == 0 is x <= 0.
	switch [2]token.Token{op, alt} {
	case [2]token.Token{token.NEQ, token.GTR}, [2]token.Token{token.GTR, token.NEQ},
		[2]token.Token{token.EQL, token.LEQ}, [2]token.Token{token.LEQ, token.EQL}:
		return "nonnegative vs 0"
	}
	return ""
}

func (f *finder) isZero(e ast.Expr) bool {
	v := f.info.Types[e].Value
	return v != nil && (v.Kind() == constant.Int || v.Kind() == constant.Float) && constant.Sign(v) == 0
}

// nonnegative reports whether e is unsigned or a call to len or cap.
func (f *finder) nonnegative(e ast.Expr) bool {
	if b, ok := f.basic(e); ok && b.Info()&types.IsUnsigned != 0 {
		return true
	}
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	b, ok := f.info.Uses[id].(*types.Builtin)
	return ok && (b.Name() == "len" || b.Name() == "cap")
}

// mirror returns the operator that keeps a comparison's meaning when its
// operands swap sides: 0 < x is x > 0.
func mirror(op token.Token) token.Token {
	switch op {
	case token.LSS:
		return token.GTR
	case token.LEQ:
		return token.GEQ
	case token.GTR:
		return token.LSS
	case token.GEQ:
		return token.LEQ
	}
	return op
}

// := is left alone: deleting it leaves the names it declares undefined.
func (f *finder) deleteEach(list []ast.Stmt) {
	for _, s := range list {
		switch s := s.(type) {
		case *ast.ExprStmt, *ast.IncDecStmt:
			f.delete(s)
		case *ast.AssignStmt:
			if s.Tok != token.DEFINE {
				f.delete(s)
			}
		}
	}
}

func (f *finder) delete(s ast.Stmt) {
	if exprs, ok := f.orphans(s); ok && len(exprs) > 0 {
		m := f.add(SDL, s, s, s.Pos(), s.End(), blank(exprs))
		m.Repaired, m.alt = true, ""
		return
	}
	f.add(SDL, s, s, s.Pos(), s.End(), "")
}

// add records a mutant that replaces [from, to) with text and returns it
// for the caller to annotate. ctx is the node shown as Original; node names
// the kind of node changed.
func (f *finder) add(o Operator, node ast.Node, ctx ast.Node, from, to token.Pos, text string) *Mutant {
	start, end := f.offset(from), f.offset(to)
	cs, ce := f.offset(ctx.Pos()), f.offset(ctx.End())
	original := string(f.src[cs:ce])
	mutated := original[:start-cs] + text + original[end-cs:]
	f.found = append(f.found, Mutant{
		Pos:      f.fset.Position(from),
		Operator: o,
		Node:     fmt.Sprintf("%T", node)[len("*ast."):],
		Original: original,
		Mutated:  mutated,
		start:    start,
		end:      end,
		text:     text,
		node:     node,
		alt:      text,
	})
	return &f.found[len(f.found)-1]
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
