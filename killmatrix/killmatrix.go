// Package killmatrix computes the full kill matrix of Go packages: which
// test kills which mutant.
//
// It is the slow, simple reference that faster mutation testing is checked
// against. Each mutant gets its own build, each test runs alone, and no
// mutant or test is skipped, so the matrix can answer what any cheaper
// strategy would have found.
package killmatrix

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/version"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/ibrhmkoz/go-kit/mutant"
)

// Matrix holds one row per mutant, one per test and one per mutant and test
// that ran. Its three slices map to three tables, ready for SQL.
type Matrix struct {
	Mutants []Mutant
	Tests   []Test
	Results []Result
}

// Mutant is a mutant of one package. Compiled is false when the mutant did
// not build; it then has no results.
type Mutant struct {
	ID       int             `json:"id"`
	Pkg      string          `json:"pkg"`
	File     string          `json:"file"`
	Line     int             `json:"line"`
	Col      int             `json:"col"`
	Operator mutant.Operator `json:"operator"`
	Node     string          `json:"node"`
	Original string          `json:"original"`
	Mutated  string          `json:"mutated"`
	// Equivalent, Repaired and Arid are as in mutant.Mutant.
	Equivalent string `json:"equivalent"`
	Repaired   bool   `json:"repaired"`
	Arid       string `json:"arid"`
	Compiled   bool   `json:"compiled"`
	// Schema is set when the mutant ran in its package's schema binary
	// rather than in a build of its own; BuildMS is then its share of the
	// schema build.
	Schema  bool    `json:"schema"`
	BuildMS float64 `json:"build_ms"`
	// Path is the file's absolute path; it stays out of the tables, which
	// are meant to be shared.
	Path string `json:"-"`

	m mutant.Mutant
}

// TestState says whether a test is in the matrix. Flaky and failing tests
// are not: a mutant they fail on says nothing about the mutant.
type TestState string

const (
	OK      TestState = "ok"
	Flaky   TestState = "flaky"
	Failing TestState = "failing"
)

// Test is a leaf test: a subtest with no subtests of its own, or a
// top-level test, example or fuzz target with none. Table-driven tests put
// many cases in one top-level test, so leaves keep the matrix from
// collapsing them into one column.
//
// Leaves of one top-level test run in one process. BaselineMS is the median
// time of that process on the unmutated package.
type Test struct {
	ID         int       `json:"id"`
	Pkg        string    `json:"pkg"`
	Name       string    `json:"name"`
	BaselineMS float64   `json:"baseline_ms"`
	State      TestState `json:"state"`
}

// Status is how a test did against a mutant.
type Status string

const (
	Killed   Status = "killed"
	Survived Status = "survived"
	// Timeout is not a kill: a mutant that only slows a test down may
	// still be equivalent.
	Timeout Status = "timeout"
	// FlakyKill, with Confirm, is a test that failed against the mutant
	// but passed when run against it again, or passed against it, or
	// failed against the original code, once run alone. It is not a kill.
	FlakyKill Status = "flaky"
	// recheck marks a kill that Confirm could not tell from load, until
	// the runner checks it alone.
	recheck Status = "recheck"
)

// Verdict sums up how the tests did against one mutant.
type Verdict string

const (
	VerdictKilled     Verdict = "killed"
	VerdictSurvived   Verdict = "survived"
	VerdictTimeout    Verdict = "timeout"
	VerdictNotCovered Verdict = "not covered"
	VerdictNoBuild    Verdict = "build error"
)

// Verdicts returns each mutant's verdict, by id: killed if a test killed
// it, else timeout if a run timed out, else survived, or not covered when
// no test reached it in the baseline (every run skipped).
func (m *Matrix) Verdicts() map[int]Verdict {
	v := map[int]Verdict{}
	for _, mu := range m.Mutants {
		v[mu.ID] = VerdictNotCovered
		if !mu.Compiled {
			v[mu.ID] = VerdictNoBuild
		}
	}
	for _, r := range m.Results {
		switch {
		case r.Status == Killed:
			v[r.MutantID] = VerdictKilled
		case r.Status == Timeout && v[r.MutantID] != VerdictKilled:
			v[r.MutantID] = VerdictTimeout
		case !r.Skipped && v[r.MutantID] == VerdictNotCovered:
			v[r.MutantID] = VerdictSurvived
		}
	}
	return v
}

// Result is how one leaf test did against one mutant. MS is the time of
// the process that ran its top-level test, shared by its sibling leaves.
type Result struct {
	MutantID int     `json:"mutant_id"`
	TestID   int     `json:"test_id"`
	Status   Status  `json:"status"`
	MS       float64 `json:"ms"`
	// Skipped is set when the test did not run: in the baseline, its
	// top-level test never reached the mutant's site, so it survives.
	Skipped bool `json:"skipped,omitempty"`
}

// Runner computes kill matrices. The zero value is ready to use.
type Runner struct {
	// BaselineRuns is how often each test runs on the unmutated package to
	// find flaky tests. Default 3.
	BaselineRuns int
	// A test run against a mutant times out after its baseline time times
	// TimeoutFactor plus TimeoutPad. Defaults 3 and 1s.
	TimeoutFactor float64
	TimeoutPad    time.Duration
	// Workers is how many mutants build and run at once. Default 1, which
	// keeps the times in the matrix free of contention.
	Workers int
	// Log, if set, receives a line per package and per mutant.
	Log io.Writer
	// Schemata builds each package once with every mutant it can embed
	// (mutant.Embed) and picks one per run; the rest build on their own.
	Schemata bool
	// Select, with Schemata, runs a top-level test against a mutant only
	// if, in the baseline, the test reached a site in the function that
	// holds the mutant. A function, not the site itself: a branch that
	// runs only when two calls share a clock tick may run in one run and
	// not in the next, and the function around it reaches some site
	// every time it runs.
	Select bool
	// Choose, if set, picks by index which of a file's mutants to run;
	// path is the file's absolute path. The rest are left out entirely.
	Choose func(path string, ms []mutant.Mutant) []int
	// FailFast stops running tests against a mutant once one kills it,
	// trying the fastest top-level tests first. The matrix then holds only
	// the runs made: enough for Verdict, not for adequacy.
	FailFast bool
	// Confirm runs a top-level test again when it fails against a mutant
	// and, in a schema binary, once against the original code. A leaf
	// counts as killed only if it fails against the mutant both times and
	// passes against the original: timing tests fail under load, often
	// several times in a row, and a kill they invent hides a mutant no
	// test catches.
	Confirm bool
	// Extreme takes the mutants from mutant.Extreme, one per function,
	// instead of mutant.Find. Embed takes only Find's, so each builds on
	// its own and Schemata and Select do nothing.
	Extreme bool
}

// Run computes the kill matrix of the packages that patterns match in dir.
func (r Runner) Run(ctx context.Context, dir string, patterns ...string) (*Matrix, error) {
	r = r.withDefaults()
	cfg := &packages.Config{
		Context: ctx,
		Dir:     dir,
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedModule,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(pkgs, func(a, b *packages.Package) int { return cmp.Compare(a.PkgPath, b.PkgPath) })

	tmp, err := os.MkdirTemp("", "killmatrix")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	m := &Matrix{}
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return nil, fmt.Errorf("%s: %w", pkg.PkgPath, pkg.Errors[0])
		}
		if err := r.runPackage(ctx, m, pkg, tmp); err != nil {
			return nil, fmt.Errorf("%s: %w", pkg.PkgPath, err)
		}
	}
	return m, nil
}

func (r Runner) withDefaults() Runner {
	if r.BaselineRuns == 0 {
		r.BaselineRuns = 3
	}
	if r.TimeoutFactor == 0 {
		r.TimeoutFactor = 3
	}
	if r.TimeoutPad == 0 {
		r.TimeoutPad = time.Second
	}
	if r.Workers == 0 {
		r.Workers = 1
	}
	if r.Log == nil {
		r.Log = io.Discard
	}
	return r
}

// source is a Go file of a package and the mutants Find returned for it.
type source struct {
	name    string
	src     []byte
	file    *ast.File
	mutants []int // indices into the package's mutants
	find    []int // for each of Find's mutants, its index among them, -1 if not chosen
}

func (r Runner) runPackage(ctx context.Context, m *Matrix, pkg *packages.Package, tmp string) error {
	if len(pkg.GoFiles) == 0 {
		return nil
	}
	dir := filepath.Dir(pkg.GoFiles[0])

	firstMutant := len(m.Mutants)
	var sources []source
	for _, f := range pkg.Syntax {
		name := pkg.Fset.File(f.Pos()).Name()
		if ast.IsGenerated(f) || !slices.Contains(pkg.GoFiles, name) {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		s := source{name: name, src: src, file: f}
		found := mutant.Find(pkg.Fset, f, src, pkg.TypesInfo)
		if r.Extreme {
			found = mutant.Extreme(pkg.Fset, f, src, pkg.TypesInfo)
		}
		s.find = make([]int, len(found))
		chosen := make([]bool, len(found))
		if r.Choose == nil {
			for i := range chosen {
				chosen[i] = true
			}
		} else {
			for _, i := range r.Choose(name, found) {
				chosen[i] = true
			}
		}
		for i, mu := range found {
			s.find[i] = -1
			if !chosen[i] {
				continue
			}
			s.find[i] = len(m.Mutants) - firstMutant
			s.mutants = append(s.mutants, len(m.Mutants)-firstMutant)
			m.Mutants = append(m.Mutants, Mutant{
				Path:       name,
				ID:         len(m.Mutants) + 1,
				Pkg:        pkg.PkgPath,
				File:       filepath.Base(name),
				Line:       mu.Pos.Line,
				Col:        mu.Pos.Column,
				Operator:   mu.Operator,
				Node:       mu.Node,
				Original:   mu.Original,
				Mutated:    mu.Mutated,
				Equivalent: mu.Equivalent,
				Repaired:   mu.Repaired,
				Arid:       mu.Arid,
				m:          mu,
			})
		}
		sources = append(sources, s)
	}
	mutants := m.Mutants[firstMutant:]
	fn := functions(pkg.Fset, sources, mutants)

	bin := filepath.Join(tmp, strings.ReplaceAll(pkg.PkgPath, "/", "_")+".test")
	schema := ""
	if r.Schemata && !r.Extreme && len(mutants) > 0 {
		var err error
		if schema, err = r.buildSchema(ctx, pkg, dir, tmp, sources, mutants); err != nil {
			return err
		}
	}
	if schema != "" {
		bin = schema // the baseline runs in it too, with no mutant on
	} else if out, err := build(ctx, dir, bin, ""); err != nil {
		return fmt.Errorf("baseline build: %w\n%s", err, out)
	}
	if _, err := os.Stat(bin); errors.Is(err, os.ErrNotExist) {
		m.Mutants = m.Mutants[:firstMutant]
		return nil // no test files
	}

	tops, err := list(ctx, dir, bin)
	if err != nil {
		return err
	}
	firstTest := len(m.Tests)
	var reached map[string]map[int]bool // top-level test → functions it reached
	if schema != "" && r.Select {
		reached = map[string]map[int]bool{}
	}
	for i, top := range tops {
		reach := ""
		if reached != nil {
			reach = filepath.Join(tmp, fmt.Sprintf("%s.reach%d", strings.ReplaceAll(pkg.PkgPath, "/", "_"), i))
		}
		tests, ids := r.baseline(ctx, dir, bin, pkg.PkgPath, top, reach)
		if reached != nil {
			reached[top] = map[int]bool{}
			for id := range ids {
				reached[top][fn[id]] = true
			}
		}
		for _, t := range tests {
			t.ID = len(m.Tests) + 1
			m.Tests = append(m.Tests, t)
		}
	}
	tests := m.Tests[firstTest:]
	r.logf("%s: %d top-level tests, %d leaves, %d mutants\n", pkg.PkgPath, len(tops), len(tests), len(mutants))

	results := make([][]Result, len(mutants))
	errs := make([]error, len(mutants))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range r.Workers {
		wg.Go(func() {
			for i := range jobs {
				if mutants[i].Schema {
					results[i], errs[i] = r.runTests(ctx, dir, schema, &mutants[i], tests, reached, fn)
					continue
				}
				results[i], errs[i] = r.runMutant(ctx, dir, tmp, &mutants[i], tests)
			}
		})
	}
	for i := range mutants {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return err
	}
	if err := r.recheck(ctx, dir, schema, tests, results, mutants); err != nil {
		return err
	}
	for _, rs := range results {
		m.Results = append(m.Results, rs...)
	}
	return nil
}

// recheck settles, one at a time with nothing else running, the kills
// Confirm left open: the original runs first and must pass, then the
// mutant must fail. Both run once per top-level test.
func (r Runner) recheck(ctx context.Context, dir, bin string, tests []Test, results [][]Result, mutants []Mutant) error {
	byID := map[int]Test{}
	for _, t := range tests {
		byID[t.ID] = t
	}
	for i := range results {
		type pair struct{ original, mutant report }
		ran := map[string]pair{}
		for j := range results[i] {
			res := &results[i][j]
			if res.Status != recheck {
				continue
			}
			t := byID[res.TestID]
			top := topOf(t.Name)
			p, ok := ran[top]
			if !ok {
				limit := time.Duration(t.BaselineMS*r.TimeoutFactor*float64(time.Millisecond)) + r.TimeoutPad
				p.original = run(ctx, dir, bin, top, limit, 0, "")
				p.mutant = run(ctx, dir, bin, top, limit, mutants[i].ID, "")
				if ctx.Err() != nil {
					return ctx.Err()
				}
				ran[top] = p
			}
			res.Status = FlakyKill
			if p.original.status(t.Name) == Survived && p.mutant.status(t.Name) == Killed {
				res.Status = Killed
			}
		}
	}
	return nil
}

// buildSchema builds pkg's test binary with every mutant it can embed and
// marks those mutants Schema. A compile error drops the mutants on the
// lines it names and the build is tried again; when that stops helping,
// it returns "" and every mutant builds on its own.
func (r Runner) buildSchema(ctx context.Context, pkg *packages.Package, dir, tmp string, sources []source, mutants []Mutant) (string, error) {
	work := filepath.Join(tmp, strings.ReplaceAll(pkg.PkgPath, "/", "_")+".schema")
	if err := os.MkdirAll(work, 0o700); err != nil {
		return "", err
	}
	// Embed's helpers are generic; a file older than Go 1.18 cannot call them.
	old := pkg.Module == nil || pkg.Module.GoVersion == "" || version.Compare("go"+pkg.Module.GoVersion, "go1.21") < 0
	dropped := map[int]bool{}
	start := time.Now()
	for attempt := 1; ; attempt++ {
		replace := map[string]string{}
		for _, s := range sources {
			ids := make([]int, len(s.find))
			for j, k := range s.find {
				if k >= 0 && !dropped[k] {
					ids[j] = mutants[k].ID
				}
			}
			out, in := mutant.Embed(pkg.Fset, s.file, s.src, pkg.TypesInfo, ids)
			changed := false
			for j, k := range s.find {
				if k >= 0 {
					mutants[k].Schema = in[j]
					changed = changed || in[j]
				}
			}
			if !changed {
				continue
			}
			if old {
				out = mutant.WithGo121(out, s.name)
			}
			path := filepath.Join(work, filepath.Base(s.name))
			if err := os.WriteFile(path, out, 0o600); err != nil {
				return "", err
			}
			replace[s.name] = path
		}
		rt := filepath.Join(work, "zz_mutant_runtime.go")
		if err := os.WriteFile(rt, mutant.Runtime(pkg.Name), 0o600); err != nil {
			return "", err
		}
		replace[filepath.Join(dir, "zz_mutant_runtime.go")] = rt
		overlay, err := json.Marshal(map[string]map[string]string{"Replace": replace})
		if err != nil {
			return "", err
		}
		overlayFile := filepath.Join(work, "overlay.json")
		if err := os.WriteFile(overlayFile, overlay, 0o600); err != nil {
			return "", err
		}
		bin := filepath.Join(work, "schema.test")
		out, err := build(ctx, dir, bin, overlayFile)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err == nil {
			n := 0
			for _, mu := range mutants {
				if mu.Schema {
					n++
				}
			}
			share := ms(time.Since(start)) / float64(max(n, 1))
			for i := range mutants {
				if mutants[i].Schema {
					mutants[i].Compiled, mutants[i].BuildMS = true, share
				}
			}
			r.logf("%s: schema of %d mutants built in %d attempt(s)\n", pkg.PkgPath, n, attempt)
			return bin, nil
		}
		more := false
		for _, at := range compileErrors(out) {
			for k, mu := range mutants {
				if mu.Schema && mu.File == at.file && mu.Line == at.line && !dropped[k] {
					dropped[k], more = true, true
				}
			}
		}
		if !more || attempt == 5 {
			r.logf("%s: schema build failed, building each mutant on its own:\n%s\n", pkg.PkgPath, out)
			for i := range mutants {
				mutants[i].Schema = false
			}
			return "", nil
		}
	}
}

// functions maps each mutant's id to the function declaration it lies in,
// numbered across the package, or -1 outside any: a package variable's
// initializer runs in every test.
func functions(fset *token.FileSet, sources []source, mutants []Mutant) map[int]int {
	fn := map[int]int{}
	n := 0
	for _, s := range sources {
		type decl struct{ from, to, id int }
		var decls []decl
		for _, d := range s.file.Decls {
			if f, ok := d.(*ast.FuncDecl); ok {
				decls = append(decls, decl{fset.Position(f.Pos()).Offset, fset.Position(f.End()).Offset, n})
				n++
			}
		}
		for _, k := range s.mutants {
			mu := mutants[k]
			fn[mu.ID] = -1
			for _, d := range decls {
				if off := mu.m.Pos.Offset; off >= d.from && off < d.to {
					fn[mu.ID] = d.id
				}
			}
		}
	}
	return fn
}

type position struct {
	file string
	line int
}

var compileError = regexp.MustCompile(`(?m)^(?:.*/)?([^/:\s]+\.go):(\d+):\d+: `)

// compileErrors returns the files and lines go build reports errors at.
func compileErrors(out []byte) []position {
	var at []position
	for _, m := range compileError.FindAllSubmatch(out, -1) {
		line, _ := strconv.Atoi(string(m[2]))
		at = append(at, position{string(m[1]), line})
	}
	return at
}

// runTests runs every test against mu in bin: a build of mu alone, or a
// schema binary that switches mu on by its id.
//
// With reached, a top-level test that never reached mu's function in the
// baseline does not run; its leaves survive, since code that never runs
// cannot fail them.
func (r Runner) runTests(ctx context.Context, dir, bin string, mu *Mutant, tests []Test, reached map[string]map[int]bool, fn map[int]int) ([]Result, error) {
	if r.FailFast {
		// Leaves of one top-level test share its baseline time, so a
		// stable sort keeps them together.
		tests = slices.Clone(tests)
		slices.SortStableFunc(tests, func(a, b Test) int { return cmp.Compare(a.BaselineMS, b.BaselineMS) })
	}
	var results []Result
	var rep, again, original report
	ran, ranAgain := "", "" // the top-level tests rep, and again and original, are from
	killed := false
	for _, t := range tests {
		if t.State != OK {
			continue
		}
		if reached != nil && fn[mu.ID] >= 0 && !reached[topOf(t.Name)][fn[mu.ID]] {
			if !r.FailFast {
				results = append(results, Result{MutantID: mu.ID, TestID: t.ID, Status: Survived, Skipped: true})
			}
			continue
		}
		if top := topOf(t.Name); top != ran {
			if killed {
				break
			}
			limit := time.Duration(t.BaselineMS*r.TimeoutFactor*float64(time.Millisecond)) + r.TimeoutPad
			rep, ran = run(ctx, dir, bin, top, limit, mu.ID, ""), top
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		status := rep.status(t.Name)
		if status == Killed && r.Confirm {
			if ranAgain != ran {
				limit := time.Duration(t.BaselineMS*r.TimeoutFactor*float64(time.Millisecond)) + r.TimeoutPad
				again, ranAgain = run(ctx, dir, bin, ran, limit, mu.ID, ""), ran
				original = report{ok: true}
				if mu.Schema {
					original = run(ctx, dir, bin, ran, limit, 0, "")
				}
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
			}
			switch {
			case again.status(t.Name) != Killed:
				status = FlakyKill
			case original.status(t.Name) != Survived:
				// The original fails too: the machine may be too loaded
				// to tell. Decided after every mutant has run, alone.
				status = recheck
			}
		}
		killed = killed || r.FailFast && status == Killed
		results = append(results, Result{MutantID: mu.ID, TestID: t.ID, Status: status, MS: ms(rep.elapsed)})
	}
	r.logf("M%d done\n", mu.ID)
	return results, nil
}

// baseline runs a top-level test BaselineRuns times on the unmutated
// package and returns its leaves. A leaf that fails or is missing in some
// runs is flaky; one that never passes is failing.
//
// With reach, a file for a schema binary to record into, it also returns
// the ids of the mutants whose sites the test reached in any run.
func (r Runner) baseline(ctx context.Context, dir, bin, pkg, top, reach string) ([]Test, map[int]bool) {
	var times []time.Duration
	var reports []report
	var names []string
	for range r.BaselineRuns {
		rep := run(ctx, dir, bin, top, 0, 0, reach)
		times = append(times, rep.elapsed)
		reports = append(reports, rep)
		for _, name := range rep.order {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	slices.Sort(times)
	leaves := leavesOf(names)
	if len(leaves) == 0 {
		leaves = []string{top} // the binary crashed before reporting
	}
	var tests []Test
	for _, leaf := range leaves {
		passed := 0
		for _, rep := range reports {
			// Unlike against a mutant, a leaf a baseline run did not
			// report did not pass: its name may change from run to run.
			// The exception is the top-level test standing in for a run
			// that crashed before reporting anything.
			p, seen := rep.passed[leaf]
			if seen && p || !seen && leaf == top && len(rep.order) == 0 && rep.ok {
				passed++
			}
		}
		state := OK
		switch passed {
		case 0:
			state = Failing
		case r.BaselineRuns:
		default:
			state = Flaky
		}
		tests = append(tests, Test{Pkg: pkg, Name: leaf, BaselineMS: ms(times[len(times)/2]), State: state})
	}
	if reach == "" {
		return tests, nil
	}
	ids := map[int]bool{}
	data, _ := os.ReadFile(reach) // absent when the test reached no site
	for f := range strings.FieldsSeq(string(data)) {
		if id, err := strconv.Atoi(f); err == nil {
			ids[id] = true
		}
	}
	return tests, ids
}

func (r Runner) runMutant(ctx context.Context, dir, tmp string, mu *Mutant, tests []Test) ([]Result, error) {
	work, err := os.MkdirTemp(tmp, fmt.Sprintf("m%d-", mu.ID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(work) }()

	file := filepath.Join(dir, mu.File)
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	mutated := filepath.Join(work, mu.File)
	if err := os.WriteFile(mutated, mu.m.Apply(src), 0o600); err != nil {
		return nil, err
	}
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": {file: mutated}})
	if err != nil {
		return nil, err
	}
	overlayFile := filepath.Join(work, "overlay.json")
	if err := os.WriteFile(overlayFile, overlay, 0o600); err != nil {
		return nil, err
	}

	bin := filepath.Join(work, "m.test")
	start := time.Now()
	_, buildErr := build(ctx, dir, bin, overlayFile)
	mu.BuildMS = ms(time.Since(start))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	mu.Compiled = buildErr == nil
	if !mu.Compiled {
		r.logf("M%d build error\n", mu.ID)
		return nil, nil
	}
	return r.runTests(ctx, dir, bin, mu, tests, nil, nil)
}

// build compiles the test binary of the package in dir into bin, with the
// files in overlay, if given, replacing the originals. Vet is off: it
// rejects mutants that compile, such as c != 'a' || c != 'b'.
func build(ctx context.Context, dir, bin, overlay string) ([]byte, error) {
	args := []string{"test", "-c", "-vet=off", "-ldflags=-s -w", "-o", bin}
	if overlay != "" {
		args = append(args, "-overlay", overlay)
	}
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// list returns the tests, examples and fuzz targets of a test binary.
// Benchmarks are left out: -test.run does not run them.
func list(ctx context.Context, dir, bin string) ([]string, error) {
	cmd := exec.CommandContext(ctx, bin, "-test.list", ".")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list tests: %w", err)
	}
	var names []string
	for name := range strings.FieldsSeq(string(out)) {
		if !strings.HasPrefix(name, "Benchmark") {
			names = append(names, name)
		}
	}
	return names, nil
}

// run runs one top-level test of bin in dir with mutant id switched on, if
// bin is a schema binary, recording the sites it reaches into reach if
// set. A limit of 0 means no limit.
func run(ctx context.Context, dir, bin, top string, limit time.Duration, id int, reach string) report {
	if limit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limit)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, bin, "-test.run", "^"+top+"$", "-test.count=1", "-test.v=test2json")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%d", mutant.EnvVar, id))
	if reach != "" {
		cmd.Env = append(cmd.Env, mutant.ReachEnvVar+"="+reach)
	}
	start := time.Now()
	out, err := cmd.Output()
	return (&report{
		ok:       err == nil,
		timedOut: limit > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded),
		elapsed:  time.Since(start),
	}).parse(out)
}

// Log is progress only; a failed write must not stop a run.
func (r Runner) logf(format string, args ...any) { _, _ = fmt.Fprintf(r.Log, format, args...) }

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// String draws the matrix a package at a time: K killed, . survived,
// T timeout, F flaky, B build error. Flaky and failing tests get no column.
func (m *Matrix) String() string {
	status := map[[2]int]Status{}
	for _, r := range m.Results {
		status[[2]int{r.MutantID, r.TestID}] = r.Status
	}
	glyph := map[Status]string{Killed: "K", Survived: ".", Timeout: "T", FlakyKill: "F"}

	var b strings.Builder
	var pkgs []string
	for _, t := range m.Tests {
		if !slices.Contains(pkgs, t.Pkg) {
			pkgs = append(pkgs, t.Pkg)
		}
	}
	for i, pkg := range pkgs {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintln(&b, pkg)
		var cols []Test
		for _, t := range m.Tests {
			if t.Pkg != pkg {
				continue
			}
			if t.State != OK {
				fmt.Fprintf(&b, "  T%d %s (%s)\n", t.ID, t.Name, t.State)
				continue
			}
			fmt.Fprintf(&b, "  T%d %s\n", t.ID, t.Name)
			cols = append(cols, t)
		}

		var rows []Mutant
		width := 0
		for _, mu := range m.Mutants {
			if mu.Pkg == pkg {
				rows = append(rows, mu)
				width = max(width, len(fmt.Sprint("M", mu.ID)))
			}
		}
		if len(rows) == 0 {
			continue
		}
		labels := make([]string, len(cols))
		for j, t := range cols {
			labels[j] = fmt.Sprint("T", t.ID)
		}
		fmt.Fprintf(&b, "\n  %*s %s\n", width, "", strings.Join(labels, " "))
		for _, mu := range rows {
			fmt.Fprintf(&b, "  %-*s ", width, fmt.Sprint("M", mu.ID))
			for j, t := range cols {
				if j > 0 {
					b.WriteString(" ")
				}
				g := "B"
				if mu.Compiled {
					g = glyph[status[[2]int{mu.ID, t.ID}]]
				}
				fmt.Fprintf(&b, "%-*s", len(fmt.Sprint("T", t.ID)), g)
			}
			fmt.Fprintf(&b, "  %s\n", mu.m)
		}
	}
	return b.String()
}
