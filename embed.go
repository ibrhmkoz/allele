package mutant

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"
)

// EnvVar names the environment variable that picks, by id, the mutant a
// program built from Embed runs; unset or 0 runs none.
const EnvVar = "GOMUTANT"

// ReachEnvVar names the environment variable that, set to a file, makes a
// program built from Embed append to it the id of every mutant whose site
// runs, one per line: a test that never reaches a mutant cannot kill it.
const ReachEnvVar = "GOMUTANT_REACH"

// Embed returns file's source with its mutants built in, each switched on
// at run time by its id in EnvVar (mutant schemata: Untch, Offutt and
// Harrold, ISSTA 1993). One build then serves every mutant.
//
// ids[i] is the id of Find's i-th mutant, 0 to leave it out. embedded[i]
// reports whether it is in; some cannot be: an expression in a constant
// context cannot call a function, and a few types cannot pass through the
// helpers. The output keeps every line on its line, so positions in
// panics and test output do not move. It calls helpers that Runtime
// defines.
func Embed(fset *token.FileSet, file *ast.File, src []byte, info *types.Info, ids []int) (out []byte, embedded []bool) {
	ms := Find(fset, file, src, info)
	e := &embedder{fset: fset, src: src, info: info, groups: map[span][]*site{}, results: resultTypes(file, info), listed: listed(file)}
	embedded = make([]bool, len(ms))
	// x += y has two sites: the statement, deleted, and its operator.
	type key struct {
		node    ast.Node
		deleted bool
	}
	sites := map[key]*site{}
	for i, m := range ms {
		if i >= len(ids) || ids[i] == 0 || !e.embeddable(m) {
			continue
		}
		k := key{m.node, m.Operator == SDL}
		s, ok := sites[k]
		if !ok {
			s = &site{node: m.node, op: m.Operator}
			sites[k] = s
			sp := e.span(s)
			e.groups[sp] = append(e.groups[sp], s)
		}
		s.alts = append(s.alts, alt{m.alt, ids[i]})
		embedded[i] = true
	}
	for sp, g := range e.groups {
		slices.SortFunc(g, func(a, b *site) int { return a.rank() - b.rank() })
		e.order = append(e.order, sp)
	}
	slices.SortFunc(e.order, func(a, b span) int {
		if a.from != b.from {
			return a.from - b.from
		}
		return b.to - a.to
	})
	return []byte(e.inside(span{0, len(src)})), embedded
}

type span struct{ from, to int }

// site is a node with the mutants built into it.
type site struct {
	node ast.Node
	op   Operator
	alts []alt
}

type alt struct {
	to string
	id int
}

// rank orders sites that share a span from outermost to innermost: a
// deleted statement guards everything in it, a condition wraps the
// expression it is.
func (s *site) rank() int {
	switch {
	case s.op == SDL:
		return 0
	case isNode[*ast.IfStmt](s.node):
		return 1
	case isNode[*ast.ReturnStmt](s.node):
		return 2
	}
	return 3
}

func isNode[T ast.Node](n ast.Node) bool { _, ok := n.(T); return ok }

func (s *site) id(to string) int {
	for _, a := range s.alts {
		if a.to == to {
			return a.id
		}
	}
	return 0
}

type embedder struct {
	fset    *token.FileSet
	src     []byte
	info    *types.Info
	groups  map[span][]*site
	order   []span // spans of groups, outer before inner
	results map[*ast.ReturnStmt]types.Type
	listed  map[ast.Stmt]bool // statements in a block or case body
}

func (e *embedder) offset(p token.Pos) int { return e.fset.Position(p).Offset }

func (e *embedder) spanOf(n ast.Node) span { return span{e.offset(n.Pos()), e.offset(n.End())} }

// span is the source a site rewrites.
func (e *embedder) span(s *site) span {
	switch n := s.node.(type) {
	case *ast.IfStmt:
		return e.spanOf(n.Cond)
	case *ast.ReturnStmt:
		if s.op == ERR {
			return e.spanOf(n.Results[len(n.Results)-1])
		}
	}
	return e.spanOf(s.node)
}

// text renders sp, built in if a group of sites spans exactly it.
func (e *embedder) text(sp span) string {
	if g, ok := e.groups[sp]; ok {
		return e.wrap(sp, g)
	}
	return e.inside(sp)
}

// inside renders sp with each outermost group strictly inside it built in.
func (e *embedder) inside(sp span) string {
	var b strings.Builder
	at := sp.from
	for _, g := range e.order {
		if g.from < at || g.to > sp.to || g == sp {
			continue
		}
		b.Write(e.src[at:g.from])
		b.WriteString(e.wrap(g, e.groups[g]))
		at = g.to
	}
	b.Write(e.src[at:sp.to])
	return b.String()
}

// wrap renders the sites of one span, innermost first.
func (e *embedder) wrap(sp span, g []*site) string {
	inner := g[len(g)-1]
	var s string
	switch n := inner.node.(type) {
	case *ast.BinaryExpr:
		s = e.binary(n, inner)
		g = g[:len(g)-1]
	case *ast.AssignStmt:
		if inner.op == AOR {
			s = e.opAssign(n, inner)
			g = g[:len(g)-1]
			break
		}
		s = e.inside(sp)
	default:
		s = e.inside(sp)
	}
	for _, site := range slices.Backward(g) {
		switch {
		case site.op == SDL:
			s = fmt.Sprintf("if !_mutOn(%d) { %s }", site.alts[0].id, s)
		case isNode[*ast.IfStmt](site.node):
			s = fmt.Sprintf("_mutCond(func() bool { return %s }, %d, %d)", s, site.id("true"), site.id("false"))
		case isNode[*ast.ReturnStmt](site.node):
			s = fmt.Sprintf("_mutErr(%s, %d)", s, site.alts[0].id)
		}
	}
	return s
}

var (
	relationalNames = []string{"_mutLSS", "_mutLEQ", "_mutGTR", "_mutGEQ", "_mutEQL", "_mutNEQ"}
	arithmeticNames = []string{"_mutADD", "_mutSUB", "_mutMUL", "_mutQUO", "_mutREM"}
)

func (e *embedder) binary(n *ast.BinaryExpr, s *site) string {
	x, y := e.text(e.spanOf(n.X)), e.text(e.spanOf(n.Y))
	switch {
	case n.Op == token.LAND:
		return fmt.Sprintf("_mutAnd(%s, func() bool { return %s }, %d)", x, y, s.id("||"))
	case n.Op == token.LOR:
		return fmt.Sprintf("_mutOr(%s, func() bool { return %s }, %d)", x, y, s.id("&&"))
	case slices.Contains(relational, n.Op) && !e.ordered(n.X) && !e.ordered(n.Y):
		// Values without an order only swap == and !=, and maps, slices
		// and funcs are not comparable: negate the comparison as written.
		return fmt.Sprintf("_mutNot(%s %s %s, %d)", x, n.Op, y, s.id(negate(n.Op).String()))
	case slices.Contains(relational, n.Op):
		return fmt.Sprintf("_mutOrd(%s, %s, %s, %s)", x, y, relationalNames[slices.Index(relational, n.Op)], e.idArray(s, relational))
	default:
		helper := "_mutArith"
		if b, _ := e.basicOf(n); b.Info()&types.IsInteger == 0 {
			helper = "_mutArithF"
		}
		return fmt.Sprintf("%s(%s, %s, %s, %s)", helper, x, y, arithmeticNames[slices.Index(arithmetic, n.Op)], e.idArray(s, arithmetic))
	}
}

// idArray lists, for each operator in ops, the id of the mutant that
// replaces the original with it, 0 for none.
func (e *embedder) idArray(s *site, ops []token.Token) string {
	parts := make([]string, len(ops))
	for i, op := range ops {
		parts[i] = fmt.Sprint(s.id(op.String()))
	}
	return fmt.Sprintf("[%d]int{%s}", len(ops), strings.Join(parts, ", "))
}

func (e *embedder) opAssign(n *ast.AssignStmt, s *site) string {
	l, r := e.text(e.spanOf(n.Lhs[0])), e.text(e.spanOf(n.Rhs[0]))
	var b strings.Builder
	ids := make([]string, len(s.alts))
	for i, a := range s.alts {
		ids[i] = fmt.Sprint(a.id)
	}
	fmt.Fprintf(&b, "switch _mutAt(%s) {", strings.Join(ids, ", "))
	for _, a := range s.alts {
		fmt.Fprintf(&b, " case %d: %s %s %s;", a.id, l, a.to, r)
	}
	fmt.Fprintf(&b, " default: %s %s %s }", l, n.Tok, r)
	return b.String()
}

// embeddable reports whether m can be built in: whether the helper its
// site calls accepts the operands' types where the original appears.
func (e *embedder) embeddable(m Mutant) bool {
	switch n := m.node.(type) {
	case *ast.IfStmt:
		return e.isBool(n.Cond)
	case *ast.ReturnStmt:
		if m.Operator == SDL {
			return true
		}
		// _mutErr returns error; only a function returning error takes it as is.
		return types.Identical(e.results[n], types.Universe.Lookup("error").Type())
	case *ast.AssignStmt:
		// A switch is a statement: x += y in a for loop's post or an if's
		// init cannot become one.
		return m.Operator == SDL || e.listed[n]
	case *ast.BinaryExpr:
		if e.info.Types[n].Value != nil || e.typeParam(n.X) || e.typeParam(n.Y) || !e.unify(n.X, n.Y) {
			return false
		}
		switch {
		case n.Op == token.LAND || n.Op == token.LOR:
			return e.isBool(n) && e.isBool(n.X) && e.isBool(n.Y)
		case slices.Contains(relational, n.Op):
			return e.isBool(n)
		default:
			_, ok := e.basicOf(n)
			return ok
		}
	case *ast.ExprStmt:
		// panic as a block's last statement ends it; guarded, it does not,
		// and the function would lack a return.
		call, ok := n.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := ast.Unparen(call.Fun).(*ast.Ident)
		if !ok {
			return true
		}
		b, ok := e.info.Uses[id].(*types.Builtin)
		return !ok || b.Name() != "panic"
	}
	return true
}

// isBool reports whether x is bool or untyped bool, the types the helpers
// take and return; a named bool type would not convert.
func (e *embedder) isBool(x ast.Expr) bool {
	t := e.info.TypeOf(x)
	return t == types.Typ[types.Bool] || t == types.Typ[types.UntypedBool]
}

// unify reports whether one type parameter T can stand for both operands:
// the same type, or one of them untyped.
func (e *embedder) unify(x, y ast.Expr) bool {
	tx, ty := e.info.TypeOf(x), e.info.TypeOf(y)
	return types.Identical(tx, ty) || untyped(tx) || untyped(ty)
}

func untyped(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && b.Info()&types.IsUntyped != 0
}

func (e *embedder) typeParam(x ast.Expr) bool {
	_, ok := e.info.TypeOf(x).(*types.TypeParam)
	return ok
}

func (e *embedder) ordered(x ast.Expr) bool {
	b, ok := e.basicOf(x)
	return ok && !untyped(b) && b.Info()&types.IsOrdered != 0
}

func (e *embedder) basicOf(x ast.Expr) (*types.Basic, bool) {
	t := e.info.TypeOf(x)
	if t == nil {
		return nil, false
	}
	b, ok := t.Underlying().(*types.Basic)
	return b, ok
}

func listed(file *ast.File) map[ast.Stmt]bool {
	m := map[ast.Stmt]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		var list []ast.Stmt
		switch n := n.(type) {
		case *ast.BlockStmt:
			list = n.List
		case *ast.CaseClause:
			list = n.Body
		case *ast.CommClause:
			list = n.Body
		}
		for _, s := range list {
			m[s] = true
		}
		return true
	})
	return m
}

// resultTypes maps each return statement to the type of its function's
// last result.
func resultTypes(file *ast.File, info *types.Info) map[*ast.ReturnStmt]types.Type {
	m := map[*ast.ReturnStmt]types.Type{}
	var stack []types.Type
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		var typ *ast.FuncType
		var body *ast.BlockStmt
		switch n := n.(type) {
		case *ast.FuncDecl:
			typ, body = n.Type, n.Body
		case *ast.FuncLit:
			typ, body = n.Type, n.Body
		case *ast.ReturnStmt:
			if len(stack) > 0 {
				m[n] = stack[len(stack)-1]
			}
			return true
		default:
			return true
		}
		var last types.Type
		if typ.Results != nil && len(typ.Results.List) > 0 {
			last = info.TypeOf(typ.Results.List[len(typ.Results.List)-1].Type)
		}
		if body != nil {
			stack = append(stack, last)
			ast.Inspect(body, visit)
			stack = stack[:len(stack)-1]
		}
		return false
	}
	ast.Inspect(file, visit)
	return m
}

// WithGo121 returns src set to Go 1.21 by its build constraint, for a
// module older than that: generic helpers need it, and 1.21 still keeps
// the per-loop loop variables of older Go. A //line directive keeps every
// line where it was. filename is the file's absolute path.
func WithGo121(src []byte, filename string) []byte {
	for _, line := range bytes.SplitAfter(src, []byte("\n")) {
		text := bytes.TrimSpace(line)
		if expr, ok := bytes.CutPrefix(text, []byte("//go:build ")); ok {
			at := bytes.Index(src, line)
			repl := fmt.Sprintf("//go:build (%s) && go1.21\n", expr)
			return append(append(append([]byte{}, src[:at]...), repl...), src[at+len(line):]...)
		}
		if bytes.HasPrefix(text, []byte("package ")) {
			break
		}
	}
	return append([]byte(fmt.Sprintf("//go:build go1.21\n\n//line %s:1\n", filename)), src...)
}

// Runtime returns a file of package pkg that defines the helpers Embed's
// output calls. It imports os and strconv, so it cannot join a package
// they depend on.
func Runtime(pkg string) []byte {
	return []byte(`// Code generated by mutant. DO NOT EDIT.

//go:build go1.21

package ` + pkg + `

import (
	"cmp"
	"os"
	"strconv"
	"sync"
)

var _mutActive = func() int { n, _ := strconv.Atoi(os.Getenv("` + EnvVar + `")); return n }()

const (
	_mutLSS = iota
	_mutLEQ
	_mutGTR
	_mutGEQ
	_mutEQL
	_mutNEQ
)

const (
	_mutADD = iota
	_mutSUB
	_mutMUL
	_mutQUO
	_mutREM
)

// _mutReach, when $` + ReachEnvVar + ` names a file, receives the id of each mutant
// whose site runs, once, as it first runs: a test binary ends in os.Exit,
// so nothing written later would land.
var _mutReach = func() *os.File {
	path := os.Getenv("` + ReachEnvVar + `")
	if path == "" {
		return nil
	}
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	return f
}()

var _mutSeen sync.Map

func _mutMark(id int) {
	if _mutReach == nil || id == 0 {
		return
	}
	if _, seen := _mutSeen.LoadOrStore(id, true); !seen {
		_, _ = _mutReach.Write(strconv.AppendInt(nil, int64(id), 10))
		_, _ = _mutReach.Write([]byte("\n"))
	}
}

func _mutMarkAll(ids []int) {
	if _mutReach != nil {
		for _, id := range ids {
			_mutMark(id)
		}
	}
}

func _mutIs(id int) bool { return id != 0 && _mutActive == id }

// _mutOn guards a deletable statement.
func _mutOn(id int) bool {
	_mutMark(id)
	return _mutIs(id)
}

// _mutAt returns the active mutant for an op-assignment's switch.
func _mutAt(ids ...int) int {
	_mutMarkAll(ids)
	return _mutActive
}

// _mutPick returns the operator the active mutant puts in place of op:
// ids[alt] is the id of the mutant that uses alt.
func _mutPick(op int, ids []int) int {
	if _mutActive != 0 {
		for alt, id := range ids {
			if id == _mutActive {
				return alt
			}
		}
	}
	return op
}

func _mutOrd[T cmp.Ordered](x, y T, op int, ids [6]int) bool {
	_mutMarkAll(ids[:])
	switch _mutPick(op, ids[:]) {
	case _mutLSS:
		return x < y
	case _mutLEQ:
		return x <= y
	case _mutGTR:
		return x > y
	case _mutGEQ:
		return x >= y
	case _mutEQL:
		return x == y
	}
	return x != y
}

func _mutNot(b bool, id int) bool {
	if _mutOn(id) {
		return !b
	}
	return b
}

type _mutInteger interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 | ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr
}

type _mutFloat interface {
	~float32 | ~float64 | ~complex64 | ~complex128
}

func _mutArith[T _mutInteger](x, y T, op int, ids [5]int) T {
	_mutMarkAll(ids[:])
	switch _mutPick(op, ids[:]) {
	case _mutADD:
		return x + y
	case _mutSUB:
		return x - y
	case _mutMUL:
		return x * y
	case _mutQUO:
		return x / y
	}
	return x % y
}

func _mutArithF[T _mutFloat](x, y T, op int, ids [5]int) T {
	_mutMarkAll(ids[:])
	switch _mutPick(op, ids[:]) {
	case _mutADD:
		return x + y
	case _mutSUB:
		return x - y
	case _mutMUL:
		return x * y
	}
	return x / y
}

func _mutAnd(x bool, y func() bool, id int) bool {
	if _mutOn(id) {
		return x || y()
	}
	return x && y()
}

func _mutOr(x bool, y func() bool, id int) bool {
	if _mutOn(id) {
		return x && y()
	}
	return x || y()
}

func _mutCond(c func() bool, t, f int) bool {
	_mutMark(t)
	_mutMark(f)
	switch {
	case _mutIs(t):
		return true
	case _mutIs(f):
		return false
	}
	return c()
}

func _mutErr(err error, id int) error {
	if _mutOn(id) {
		return nil
	}
	return err
}
`)
}
