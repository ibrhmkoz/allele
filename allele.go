// Package allele finds the mutants of a Go source file: small, deliberate
// changes such as > to >= that a good test suite should notice.
//
// Find is exhaustive on purpose. It is the reference that cheaper strategies,
// such as one mutant per line or a subset of operators, are measured against,
// so it does not filter for usefulness.
package allele

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
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
	// Arid names why a mutant is in code no one writes a test for, if it
	// is: logging, metrics, tracing, a capacity hint, a allele:ignore comment
	// or an if that does only such things (Google's arid nodes). Find keeps
	// such mutants so the rule can be measured.
	Arid string
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
	text := fit(src, m.start, m.text)
	out := make([]byte, 0, len(src)-(m.end-m.start)+len(text))
	out = append(out, src[:m.start]...)
	out = append(out, text...)
	return append(out, src[m.end:]...)
}

// fit is text to put in place of the source from src[from], with a space in front
// where it would otherwise run into the word before: unformatted source can
// write if(x>0){, and putting true in place of (x>0) must not make iftrue.
// Only a keyword can touch a span's start that way; nothing in Go's syntax
// follows an expression without a separator, so its end needs no check.
func fit(src []byte, from int, text string) string {
	if text != "" && from > 0 && word(src[from-1]) && word(text[0]) { //allele:ignore only a deleted statement is empty, and none follows a word
		return " " + text
	}
	return text
}

func word(c byte) bool {
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c >= utf8.RuneSelf
}

func (m Mutant) String() string {
	mutated := m.Mutated
	if mutated == "" {
		mutated = "(deleted)"
	}
	s := fmt.Sprintf("%s:%d:%d %s %s → %s", filepath.Base(m.Pos.Filename), m.Pos.Line, m.Pos.Column, m.Operator, firstLine(m.Original), firstLine(mutated))
	if m.Equivalent != "" {
		s += " [equivalent: " + m.Equivalent + "]"
	}
	if m.Arid != "" {
		s += " [arid: " + m.Arid + "]"
	}
	return s
}

// firstLine shortens text that spans lines, such as a function body, to
// its first line and an ellipsis.
func firstLine(s string) string {
	if line, _, more := strings.Cut(s, "\n"); more {
		return strings.TrimSpace(line) + " …"
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
	f.markArid(file)
	return f.found
}

type aridSpan struct {
	from, to int
	why      string
}

// markArid tags each mutant with the reason of the smallest arid span
// holding its change, or with allele:ignore when its line asks for it.
func (f *finder) markArid(file *ast.File) {
	var spans []aridSpan
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case ast.Stmt:
			if why := f.aridStmt(n); why != "" {
				spans = append(spans, aridSpan{f.offset(n.Pos()), f.offset(n.End()), why})
			}
		case *ast.CallExpr:
			if arg := f.capacityArg(n); arg != nil {
				spans = append(spans, aridSpan{f.offset(arg.Pos()), f.offset(arg.End()), "capacity hint"})
			}
		}
		return true
	})
	ignored := map[int]bool{}
	for _, g := range file.Comments {
		for _, c := range g.List {
			if !strings.Contains(c.Text, "allele:ignore") {
				continue
			}
			pos := f.fset.Position(c.Pos())
			line := pos.Line
			// A comment alone on its line marks the next line.
			if start := pos.Offset - pos.Column + 1; strings.TrimSpace(string(f.src[start:pos.Offset])) == "" {
				line++
			}
			ignored[line] = true
		}
	}
	for i := range f.found {
		m := &f.found[i]
		if ignored[m.Pos.Line] {
			m.Arid = "allele:ignore"
			continue
		}
		best := -1
		for j, sp := range spans {
			if sp.from <= m.start && m.end <= sp.to && (best < 0 || sp.to-sp.from < spans[best].to-spans[best].from) {
				best = j
			}
		}
		if best >= 0 {
			m.Arid = spans[best].why
		}
	}
}

// aridStmt returns why s is arid: a call that only logs, counts or traces,
// or an if whose branches do only such things.
func (f *finder) aridStmt(s ast.Stmt) string {
	switch s := s.(type) {
	case *ast.ExprStmt:
		if call, ok := s.X.(*ast.CallExpr); ok {
			return f.aridCall(call)
		}
	case *ast.BlockStmt:
		if len(s.List) == 0 {
			return ""
		}
		for _, st := range s.List {
			if f.aridStmt(st) == "" {
				return ""
			}
		}
		return "only arid statements"
	case *ast.IfStmt:
		if f.aridStmt(s.Body) != "" && (s.Else == nil || f.aridStmt(s.Else) != "") {
			return "only arid statements"
		}
	case *ast.SwitchStmt:
		return f.aridCases(s.Body)
	case *ast.TypeSwitchStmt:
		return f.aridCases(s.Body)
	}
	return ""
}

// aridCases returns why a switch is arid: every case does only arid
// things, such as picking how to log by the logger's type.
func (f *finder) aridCases(body *ast.BlockStmt) string {
	n := 0
	for _, c := range body.List {
		for _, st := range c.(*ast.CaseClause).Body {
			if f.aridStmt(st) == "" {
				return ""
			}
			n++
		}
	}
	if n == 0 {
		return ""
	}
	return "only arid statements"
}

var (
	logMethods    = setOf("Print", "Printf", "Println", "Debug", "Debugf", "Debugw", "Info", "Infof", "Infow", "Warn", "Warnf", "Warnw", "Warning", "Warningf", "Error", "Errorf", "Errorw", "Trace", "Tracef", "Log", "Logf", "LogAttrs", "DebugContext", "InfoContext", "WarnContext", "ErrorContext")
	metricMethods = setOf("Inc", "Dec", "Add", "Sub", "Set", "Observe", "Record", "IncrCounter", "AddSample", "SetGauge", "MeasureSince", "EmitKey")
	traceMethods  = setOf("End", "SetAttributes", "AddEvent", "RecordError", "SetStatus", "SetName", "SetTag", "Finish", "LogFields", "LogKV")
)

func setOf(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[n] = true
	}
	return m
}

// aridCall returns why a call made for its effect is arid: it logs
// (package log, slog or another logging package, fmt.Print, or a method of
// a type named like a logger; never Fatal or Panic, which stop the
// program), records a metric, or touches a trace.
func (f *finder) aridCall(call *ast.CallExpr) string {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	name := sel.Sel.Name
	if x, ok := sel.X.(*ast.Ident); ok {
		if pkg, ok := f.info.Uses[x].(*types.PkgName); ok {
			path := pkg.Imported().Path()
			last := path[strings.LastIndex(path, "/")+1:]
			switch {
			case path == "fmt":
				if strings.HasPrefix(name, "Print") || strings.HasPrefix(name, "Fprint") && len(call.Args) > 0 && f.isStdStream(call.Args[0]) {
					return "logging"
				}
			case strings.Contains(last, "log") && logMethods[name]:
				return "logging"
			case strings.Contains(path, "metrics") || strings.Contains(path, "prometheus"):
				return "metrics"
			}
			return ""
		}
	}
	t := f.info.TypeOf(sel.X)
	if t == nil {
		return ""
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return ""
	}
	tname, path := strings.ToLower(named.Obj().Name()), named.Obj().Pkg().Path()
	last := path[strings.LastIndex(path, "/")+1:]
	switch {
	case logMethods[name] && (strings.Contains(tname, "logger") || strings.Contains(last, "log")):
		return "logging"
	case metricMethods[name] && (strings.Contains(path, "metric") || strings.Contains(path, "prometheus")):
		return "metrics"
	case traceMethods[name] && (strings.Contains(path, "trace") || strings.Contains(path, "otel") || strings.Contains(tname, "span")):
		return "tracing"
	}
	return ""
}

func (f *finder) isStdStream(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	pkg, ok := f.info.Uses[x].(*types.PkgName)
	return ok && pkg.Imported().Path() == "os" && (sel.Sel.Name == "Stdout" || sel.Sel.Name == "Stderr")
}

// capacityArg returns the capacity of make([]T, len, cap) or the size hint
// of make(map[K]V, n): they change speed and memory, not results. A
// channel's buffer size changes behavior and is not one.
func (f *finder) capacityArg(call *ast.CallExpr) ast.Expr {
	id, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return nil
	}
	if b, ok := f.info.Uses[id].(*types.Builtin); !ok || b.Name() != "make" || len(call.Args) < 2 {
		return nil
	}
	t := f.info.TypeOf(call.Args[0])
	if t == nil {
		return nil
	}
	switch t.Underlying().(type) {
	case *types.Slice:
		if len(call.Args) == 3 {
			return call.Args[2]
		}
	case *types.Map:
		return call.Args[1]
	}
	return nil
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
// uses it the same way: the variable's name, pkg.Name for an import used
// for a value, (*pkg.T)(nil) for one used for a type; "" for a generic
// function or type, which needs type arguments to be named.
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
		case *ast.FuncDecl:
			if n.Recv != nil {
				for _, field := range n.Recv.List {
					for _, id := range field.Names {
						if v, ok := f.info.Defs[id].(*types.Var); ok {
							params[v] = true
						}
					}
				}
			}
		case *ast.FuncType:
			// Go allows unused receivers, parameters and results.
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
				switch obj := f.info.Uses[n.Sel].(type) {
				case *types.Func:
					// A generic function cannot be used without its type
					// arguments, so it is left out.
					if sig, ok := obj.Type().(*types.Signature); ok && sig.TypeParams().Len() == 0 {
						selected[x] = x.Name + "." + n.Sel.Name
					}
				case *types.Var, *types.Const:
					selected[x] = x.Name + "." + n.Sel.Name
				case *types.TypeName:
					// A type is used through a nil pointer to it; a generic
					// type would need its type arguments, so it is left out.
					if named, ok := obj.Type().(*types.Named); !ok || named.TypeParams().Len() == 0 {
						selected[x] = "(*" + x.Name + "." + n.Sel.Name + ")(nil)"
					}
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
// one is an import used in n only through generic functions or types,
// which no simple expression can use.
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
		// uses come in map order: take the first expression in the source,
		// so that a mutant reads the same every run.
		first, expr, exprAt := token.NoPos, "", token.NoPos
		for _, u := range uses {
			if u.pos < n.Pos() || u.pos >= n.End() {
				first = token.NoPos
				break
			}
			if first == token.NoPos || u.pos < first {
				first = u.pos
			}
			if u.expr != "" && (exprAt == token.NoPos || u.pos < exprAt) {
				expr, exprAt = u.expr, u.pos
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
	var x, min ast.Expr
	switch {
	case f.isSmallest(n.Y):
		x, min = n.X, n.Y
	case f.isSmallest(n.X):
		x, min, op, alt = n.Y, n.X, mirror(op), mirror(alt)
	default:
		return ""
	}
	if !f.atLeast(x, min) {
		return ""
	}
	// x >= min always, so x != min is x > min and x == min is x <= min.
	switch [2]token.Token{op, alt} {
	case [2]token.Token{token.NEQ, token.GTR}, [2]token.Token{token.GTR, token.NEQ},
		[2]token.Token{token.EQL, token.LEQ}, [2]token.Token{token.LEQ, token.EQL}:
		return "smallest value compared"
	}
	return ""
}

// isSmallest reports whether e is a constant 0 or "".
func (f *finder) isSmallest(e ast.Expr) bool {
	v := f.info.Types[e].Value
	switch {
	case v == nil:
		return false
	case v.Kind() == constant.String:
		return constant.StringVal(v) == ""
	case v.Kind() == constant.Int || v.Kind() == constant.Float:
		return constant.Sign(v) == 0
	}
	return false
}

// atLeast reports whether x can never be below min, a constant 0 or "":
// every string is >= "", and unsigned values, len and cap are >= 0.
func (f *finder) atLeast(x, min ast.Expr) bool {
	if f.info.Types[min].Value.Kind() == constant.String {
		b, ok := f.basic(x)
		return ok && b.Info()&types.IsString != 0
	}
	return f.nonnegative(x)
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
	if f.blankAndSafe(s) {
		f.add(SDL, s, s, s.Pos(), s.End(), "").Equivalent = "blank assignment that cannot panic"
		return
	}
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

// blankAndSafe reports whether s assigns only to _ from expressions that
// neither call nor can panic: names, literals, and arrays indexed by a
// constant, whose bound the compiler checks. Deleting it changes nothing.
func (f *finder) blankAndSafe(s ast.Stmt) bool {
	a, ok := s.(*ast.AssignStmt)
	if !ok || a.Tok != token.ASSIGN {
		return false
	}
	for _, l := range a.Lhs {
		if id, ok := l.(*ast.Ident); !ok || id.Name != "_" {
			return false
		}
	}
	for _, r := range a.Rhs {
		if !f.safe(r) {
			return false
		}
	}
	return true
}

func (f *finder) safe(e ast.Expr) bool {
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident, *ast.BasicLit:
		return true
	case *ast.IndexExpr:
		t := f.info.TypeOf(e.X)
		if t == nil {
			return false
		}
		_, array := t.Underlying().(*types.Array)
		return array && f.info.Types[e.Index].Value != nil && f.safe(e.X)
	}
	return false
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
