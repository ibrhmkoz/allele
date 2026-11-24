package mutant

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"math"
	"slices"
	"strings"
)

// EnvVar names the environment variable that picks, by id, the mutant a
// program built from Embed runs; unset or 0 runs none.
const EnvVar = "GOMUTANT"

// ReachEnvVar names the environment variable that, set to a file, makes a
// program built from Embed append to it the id of every mutant whose site
// runs, one per line: a test that never reaches a mutant cannot kill it.
// It appends minus the id of every mutant that would have changed a value
// there, too: one that never does cannot change what the test sees.
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
	return []byte(e.cloned(file)), embedded
}

// cloned renders the file with each function that holds a site cloned:
// its body runs as written while no mutant of the function is active, and
// as mutated otherwise, so that only the function with the active mutant
// pays for the helpers, which keep the compiler from inlining and from
// eliminating bounds checks. The original body stays on its lines; line
// directives give the mutated copy the same positions, and the rest of
// the file its own. Not cloned: a body with labels, since a label cannot
// be declared twice in one function, and one that := assigns a name of
// its signature at its top level (redeclares).
func (e *embedder) cloned(file *ast.File) string {
	var b strings.Builder
	at := 0
	for _, d := range file.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil || labeled(fd.Body) || redeclares(fd) {
			continue
		}
		lb, rb := e.offset(fd.Body.Lbrace), e.offset(fd.Body.Rbrace)
		lo, hi := math.MaxInt, 0
		for _, g := range e.order {
			if g.from < lb || g.to > rb {
				continue
			}
			for _, s := range e.groups[g] {
				for _, a := range s.alts {
					lo, hi = min(lo, a.id), max(hi, a.id)
				}
			}
		}
		if hi == 0 {
			continue
		}
		open, end := e.fset.Position(fd.Body.Lbrace), e.fset.Position(fd.Body.Rbrace)
		b.WriteString(e.inside(span{at, lb + 1}))
		fmt.Fprintf(&b, " if _mutOff(%d, %d) {", lo, hi)
		b.Write(e.src[lb+1 : rb])
		fmt.Fprintf(&b, "} else {/*line %s:%d:%d*/", open.Filename, open.Line, open.Column+1)
		// text, not inside: a body that is one site, {work()}, is that site.
		b.WriteString(e.text(span{lb + 1, rb}))
		fmt.Fprintf(&b, "}}/*line %s:%d:%d*/", end.Filename, end.Line, end.Column+1)
		at = rb + 1
	}
	b.WriteString(e.inside(span{at, len(e.src)}))
	return b.String()
}

// redeclares reports whether fd's body, at its top level, assigns with :=
// a name its signature declares: a receiver, parameter or result. Inside
// the clone's if, that := would declare a new variable instead, which a
// closure made before it, a deferred function or a bare return would not
// see.
func redeclares(fd *ast.FuncDecl) bool {
	names := map[string]bool{}
	for _, fields := range []*ast.FieldList{fd.Recv, fd.Type.Params, fd.Type.Results} {
		if fields == nil {
			continue
		}
		for _, f := range fields.List {
			for _, n := range f.Names {
				names[n.Name] = true
			}
		}
	}
	for _, st := range fd.Body.List {
		if as, ok := st.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
			for _, l := range as.Lhs {
				if id, ok := l.(*ast.Ident); ok && names[id.Name] {
					return true
				}
			}
		}
	}
	return false
}

// labeled reports whether body declares a label.
func labeled(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if _, ok := n.(*ast.LabeledStmt); ok {
			found = true
		}
		return !found
	})
	return found
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
		return fit(e.src, sp.from, e.wrap(sp, g))
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
		b.WriteString(fit(e.src, g.from, e.wrap(g, e.groups[g])))
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
			s = fmt.Sprintf("_mutCond(func() bool { return %s }, %d, %d, %v)", s, site.id("true"), site.id("false"), e.pure(site.node.(*ast.IfStmt).Cond))
		case isNode[*ast.ReturnStmt](site.node):
			// The mutant returns nil without evaluating the result, which
			// may do more than make an error: return t, t.fill(s).
			ret := site.node.(*ast.ReturnStmt)
			s = fmt.Sprintf("_mutErr(func() error { return %s }, %d, %v)", s, site.alts[0].id, e.pure(ret.Results[len(ret.Results)-1]))
		}
	}
	return s
}

var (
	relationalNames = []string{"_mutLSS", "_mutLEQ", "_mutGTR", "_mutGEQ", "_mutEQL", "_mutNEQ"}
	arithmeticNames = []string{"_mutADD", "_mutSUB", "_mutMUL", "_mutQUO", "_mutREM"}
)

// pure reports whether evaluating x can do nothing but compute a value or
// panic: it calls no function but len and cap and receives from no
// channel. Skipping a pure expression that did not panic changes nothing
// but its value, so a mutant that skips it infects only by that value.
func (e *embedder) pure(x ast.Expr) bool {
	pure := true
	ast.Inspect(x, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			if tv, ok := e.info.Types[n.Fun]; ok && tv.IsType() {
				break // a conversion
			}
			id, _ := ast.Unparen(n.Fun).(*ast.Ident)
			if b, ok := e.info.Uses[id].(*types.Builtin); ok && (b.Name() == "len" || b.Name() == "cap") {
				break
			}
			pure = false
		case *ast.UnaryExpr:
			if n.Op == token.ARROW {
				pure = false
			}
		case *ast.FuncLit:
			return false // defined, not called
		}
		return pure
	})
	return pure
}

func (e *embedder) binary(n *ast.BinaryExpr, s *site) string {
	x, y := e.text(e.spanOf(n.X)), e.text(e.spanOf(n.Y))
	switch {
	case n.Op == token.LAND:
		return fmt.Sprintf("_mutAnd(%s, func() bool { return %s }, %d, %v)", x, y, s.id("||"), e.pure(n.Y))
	case n.Op == token.LOR:
		return fmt.Sprintf("_mutOr(%s, func() bool { return %s }, %d, %v)", x, y, s.id("&&"), e.pure(n.Y))
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
// output calls. It imports os, strconv and sync, so it cannot join a package
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
	"sync/atomic"
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

// _mutSeen holds, for each mutant id below its length, whether the site
// ran (even index) and whether the mutant infected (odd index): one atomic
// load per evaluation, since a baseline's hot loops evaluate sites
// millions of times and the time it takes sets the mutants' time limits.
// Larger ids fall back to a map.
var (
	_mutSeen     [1 << 17]atomic.Bool
	_mutSeenRest sync.Map
)

// _mutFirst reports whether key, an id or minus an id, is seen now for the
// first time.
func _mutFirst(key int) bool {
	i := 2 * key
	if key < 0 {
		i = -2*key + 1
	}
	if i < len(_mutSeen) {
		return !_mutSeen[i].Load() && !_mutSeen[i].Swap(true)
	}
	_, seen := _mutSeenRest.LoadOrStore(key, true)
	return !seen
}

func _mutWrite(key int) {
	_, _ = _mutReach.Write(strconv.AppendInt(nil, int64(key), 10))
	_, _ = _mutReach.Write([]byte("\n"))
}

func _mutMark(id int) {
	if _mutReach != nil && id != 0 && _mutFirst(id) {
		_mutWrite(id)
	}
}

// _mutInfect records that mutant id would have changed a value here: a
// test that reaches a mutant's site and never infects it cannot kill it.
func _mutInfect(id int) {
	if _mutReach != nil && id != 0 && _mutFirst(-id) {
		_mutWrite(-id)
	}
}

// _mutInfected reports whether mutant id is already known to infect, so
// that hot loops stop comparing once every mutant of a site has.
func _mutInfected(id int) bool {
	if i := 2*id + 1; i < len(_mutSeen) {
		return _mutSeen[i].Load()
	}
	_, ok := _mutSeenRest.Load(-id)
	return ok
}

// _mutInfectAll marks every mutant of a site whose change cannot be told
// apart without running it.
func _mutInfectAll(ids []int) {
	if _mutReach != nil {
		for _, id := range ids {
			_mutInfect(id)
		}
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

// _mutOff reports whether a function whose mutants are lo to hi may run as
// written: none of them is active, and no baseline is recording reach.
func _mutOff(lo, hi int) bool {
	return _mutReach == nil && (_mutActive < lo || _mutActive > hi)
}

// _mutOn guards a deletable statement: deleting one that runs infects.
func _mutOn(id int) bool {
	_mutMark(id)
	_mutInfect(id)
	return _mutIs(id)
}

// _mutAt returns the active mutant for an op-assignment's switch.
func _mutAt(ids ...int) int {
	_mutMarkAll(ids)
	_mutInfectAll(ids)
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
	if _mutReach != nil {
		want := _mutOrdOp(x, y, op)
		for alt, id := range ids {
			if id != 0 && !_mutInfected(id) && _mutOrdOp(x, y, alt) != want {
				_mutInfect(id)
			}
		}
	}
	return _mutOrdOp(x, y, _mutPick(op, ids[:]))
}

func _mutOrdOp[T cmp.Ordered](x, y T, op int) bool {
	switch op {
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
	if _mutOn(id) { // !b always differs: reaching infects
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
	if _mutReach != nil {
		// Dividing by zero panics: when one side would and the other would
		// not, that is an infection, found without dividing.
		divides := func(op int) bool { return op == _mutQUO || op == _mutREM }
		if y == 0 && divides(op) {
			_mutInfectAll(ids[:])
		} else {
			want := _mutArithOp(x, y, op)
			for alt, id := range ids {
				if id != 0 && !_mutInfected(id) && (y == 0 && divides(alt) || _mutArithOp(x, y, alt) != want) {
					_mutInfect(id)
				}
			}
		}
	}
	return _mutArithOp(x, y, _mutPick(op, ids[:]))
}

func _mutArithOp[T _mutInteger](x, y T, op int) T {
	switch op {
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
	if _mutReach != nil {
		want := _mutArithFOp(x, y, op)
		for alt, id := range ids {
			// NaN differs from itself, so a NaN result counts as infected;
			// 1/v tells -0 from +0, which == does not.
			if id == 0 || _mutInfected(id) {
				continue
			}
			if got := _mutArithFOp(x, y, alt); got != want || 1/got != 1/want {
				_mutInfect(id)
			}
		}
	}
	return _mutArithFOp(x, y, _mutPick(op, ids[:]))
}

func _mutArithFOp[T _mutFloat](x, y T, op int) T {
	switch op {
	case _mutADD:
		return x + y
	case _mutSUB:
		return x - y
	case _mutMUL:
		return x * y
	}
	return x / y
}

// _mutAnd is x && y(), or x || y() for mutant id. With x false the
// mutant would call y, which the original does not: counted as infected
// rather than called. With x true the mutant skips y: infected unless y is
// pure and true.
func _mutAnd(x bool, y func() bool, id int, pure bool) bool {
	_mutMark(id)
	if _mutIs(id) {
		return x || y()
	}
	if !x {
		_mutInfect(id)
		return false
	}
	v := y()
	if !v || !pure {
		_mutInfect(id)
	}
	return v
}

// _mutOr is _mutAnd's mirror: x || y(), or x && y() for mutant id.
func _mutOr(x bool, y func() bool, id int, pure bool) bool {
	_mutMark(id)
	if _mutIs(id) {
		return x && y()
	}
	if x {
		_mutInfect(id)
		return true
	}
	v := y()
	if v || !pure {
		_mutInfect(id)
	}
	return v
}

// _mutCond is the condition c, or true for mutant t and false for f: both
// skip c, so they infect unless c is pure and has their value.
func _mutCond(c func() bool, t, f int, pure bool) bool {
	_mutMark(t)
	_mutMark(f)
	switch {
	case _mutIs(t):
		return true
	case _mutIs(f):
		return false
	}
	v := c()
	if v || !pure {
		_mutInfect(f)
	}
	if !v || !pure {
		_mutInfect(t)
	}
	return v
}

// _mutErr is the error f returns, or nil without calling f for mutant id.
// The mutant infects where the error is not nil, or where f is not pure:
// skipping it skips whatever else it does.
func _mutErr(f func() error, id int, pure bool) error {
	_mutMark(id)
	if _mutIs(id) {
		return nil
	}
	err := f()
	if err != nil || !pure {
		_mutInfect(id)
	}
	return err
}
`)
}
