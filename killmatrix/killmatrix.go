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
	"github.com/ibrhmkoz/go-kit/mutant/fingerprint"
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
	// the runs made: enough for Verdict, not for adequacy. It also stops at
	// a mutant's first timeout, so a mutant that one test kills and another
	// hangs on may read as a timeout; neither is a gap.
	FailFast bool
	// Infect, with Select and FailFast, does not run a test against a
	// mutant whose site the test ran in the baseline without the mutant's
	// value ever differing from the original's there: a test the mutant
	// never infects cannot kill it (weak mutation, Howden 1982). The test
	// counts as run and survived. A wrong skip can only miss a kill.
	Infect bool
	// Confirm runs a top-level test again when it fails against a mutant
	// and, in a schema binary, once against the original code. A leaf
	// counts as killed only if it fails against the mutant both times and
	// passes against the original: timing tests fail under load, often
	// several times in a row, and a kill they invent hides a mutant no
	// test catches.
	Confirm bool
	// CacheDir, with FailFast, persists per-package verdicts keyed by content
	// hashes of production code, dependencies, testdata, and tests. Exact hits
	// skip building and running; test-only additions reuse kills from unchanged
	// test functions and re-run only survivors.
	CacheDir string

	// sem, when set, caps total concurrent build/test subprocesses across
	// packages to Workers.
	sem chan struct{}
}

func (r Runner) acquire() { r.sem <- struct{}{} } //mut:ignore

func (r Runner) release() { <-r.sem } //mut:ignore

// Run computes the kill matrix of the packages that patterns match in dir.
func (r Runner) Run(ctx context.Context, dir string, patterns ...string) (*Matrix, error) {
	r = r.withDefaults()
	// Cost-only skip when CacheDir is empty (loadCache/saveCache no-op on "").
	useCache := r.CacheDir != "" && r.FailFast //mut:ignore
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

	var prints map[string]fingerprint.Print
	if useCache { //mut:ignore loading prints the cache does not use costs time, not verdicts
		// The packages loaded above already failed on what fails here.
		if prints, err = fingerprint.Load(ctx, dir, patterns...); err != nil { //mut:ignore
			return nil, err //mut:ignore
		}
	}

	tmp, err := os.MkdirTemp("", "killmatrix")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			return nil, fmt.Errorf("%s: %w", pkg.PkgPath, pkg.Errors[0])
		}
	}

	// Throttle total concurrent build/test subprocesses across packages.
	r.sem = make(chan struct{}, r.Workers)  //mut:ignore
	pkgWorkers := min(len(pkgs), r.Workers) //mut:ignore
	subs := make([]*Matrix, len(pkgs))
	rechecks := make([]func() error, len(pkgs))
	errs := make([]error, len(pkgs))
	pkgJobs := make(chan int)
	var wg sync.WaitGroup
	for range max(pkgWorkers, 1) {
		wg.Go(func() {
			for i := range pkgJobs {
				sub := &Matrix{}
				if err := r.runPackage(ctx, sub, pkgs[i], prints[pkgs[i].PkgPath], tmp, &rechecks[i]); err != nil {
					errs[i] = fmt.Errorf("%s: %w", pkgs[i].PkgPath, err)
					continue
				}
				subs[i] = sub
			}
		})
	}
	for i := range pkgs {
		pkgJobs <- i
	}
	close(pkgJobs)
	wg.Wait()
	// Recheck runs after all packages finish so nothing else competes for CPU.
	for i, fn := range rechecks {
		if fn != nil {
			errs[i] = fn()
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	m := &Matrix{}
	for _, sub := range subs {
		mOff, tOff := len(m.Mutants), len(m.Tests)
		for _, mu := range sub.Mutants {
			mu.ID += mOff
			m.Mutants = append(m.Mutants, mu)
		}
		for _, t := range sub.Tests {
			t.ID += tOff
			m.Tests = append(m.Tests, t)
		}
		for _, res := range sub.Results {
			res.MutantID += mOff
			res.TestID += tOff
			m.Results = append(m.Results, res)
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

func (r Runner) runPackage(ctx context.Context, m *Matrix, pkg *packages.Package, fp fingerprint.Print, tmp string, deferRecheck *func() error) error {
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
	if len(mutants) == 0 {
		// Without mutants its tests could kill nothing; skipping the build and
		// baseline is what makes mutant -diff cheap in packages it misses.
		return nil
	}

	var snap pkgSnapshot
	useCache := r.CacheDir != "" && r.FailFast
	reused := make([]bool, len(mutants))
	reusedKillers := map[int]string{}
	reusedResults := make([][]Result, len(mutants))
	if useCache { //mut:ignore
		snap = r.snapshot(fp, mutants)
		if entry, ok := loadCache(r.CacheDir, pkg.PkgPath); ok { //mut:ignore
			byKey := make(map[string]cachedMutant, len(entry.Mutants))
			for _, cm := range entry.Mutants {
				byKey[cm.Key] = cm
			}
			if entry.FullKey == snap.fullKey {
				for i := range mutants {
					cm := byKey[mutantKey(mutants[i])]
					compiled, rs := cachedResult(mutants[i].ID, cm.Verdict)
					mutants[i].Compiled = compiled
					m.Results = append(m.Results, rs...)
				}
				return nil
			}
			if entry.ProdKey == snap.prodKey {
				for i := range mutants {
					cm := byKey[mutantKey(mutants[i])]
					// Only killed mutants record KillerTop; empty KillerTop never matches testDecls.
					if h, exists := snap.testDecls[cm.KillerTop]; exists && h == entry.TestDecls[cm.KillerTop] {
						reused[i] = true
						reusedKillers[mutants[i].ID] = cm.KillerTop
						mutants[i].Compiled = true
						reusedResults[i] = []Result{{MutantID: mutants[i].ID, Status: Killed}}
					}
				}
				if !slices.Contains(reused, false) {
					for _, rs := range reusedResults {
						m.Results = append(m.Results, rs...)
					}
					saveCache(r.CacheDir, pkg.PkgPath, buildCacheEntry(snap, m, reusedKillers))
					return nil
				}
			}
		}
	}

	fn := functions(pkg.Fset, sources, mutants)

	bin := filepath.Join(tmp, strings.ReplaceAll(pkg.PkgPath, "/", "_")+".test")
	schema := ""
	if r.Schemata && len(mutants) > 0 {
		var err error
		// buildSchema returns an error only on temp-dir FS errors or ctx cancellation.
		if schema, err = r.buildSchema(ctx, pkg, dir, tmp, sources, mutants, reused); err != nil { //mut:ignore
			return err
		}
	}
	if schema != "" {
		bin = schema // the baseline runs in it too, with no mutant on
	} else if out, err := r.build(ctx, dir, bin, ""); err != nil {
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
	var reached, reachedSite map[string]map[int]bool // top-level test → functions / mutant ids it reached
	if schema != "" && r.Select {
		reached = map[string]map[int]bool{}
		reachedSite = map[string]map[int]bool{}
	}
	baseTests := make([][]Test, len(tops))
	baseIDs := make([]map[int]bool, len(tops))
	baseJobs := make(chan int)
	var baseWG sync.WaitGroup
	for range min(r.Workers, max(len(tops), 1)) {
		baseWG.Go(func() {
			for i := range baseJobs {
				reach := ""
				if reached != nil {
					reach = filepath.Join(tmp, fmt.Sprintf("%s.reach%d", strings.ReplaceAll(pkg.PkgPath, "/", "_"), i))
				}
				baseTests[i], baseIDs[i] = r.baseline(ctx, dir, bin, pkg.PkgPath, tops[i], reach)
			}
		})
	}
	for i := range tops {
		baseJobs <- i
	}
	close(baseJobs)
	baseWG.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// A latency-sensitive test can fail under parallel baseline load and look
	// flaky; recheck any non-OK top-level test alone before excluding it.
	if r.Workers > 1 {
		for i, top := range tops {
			if slices.ContainsFunc(baseTests[i], func(t Test) bool { return t.State != OK }) {
				reach := ""
				if reached != nil {
					reach = filepath.Join(tmp, fmt.Sprintf("%s.reach%d", strings.ReplaceAll(pkg.PkgPath, "/", "_"), i))
				}
				baseTests[i], baseIDs[i] = r.baseline(ctx, dir, bin, pkg.PkgPath, top, reach)
				if ctx.Err() != nil {
					return ctx.Err()
				}
			}
		}
	}
	for i, top := range tops {
		if reached != nil {
			reached[top] = map[int]bool{}
			reachedSite[top] = baseIDs[i]
			for id := range baseIDs[i] {
				if id > 0 { // -id marks an infection
					reached[top][fn[id]] = true
				}
			}
		}
		for _, t := range baseTests[i] {
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
					results[i], errs[i] = r.runTests(ctx, dir, schema, &mutants[i], tests, reached, reachedSite, fn)
					continue
				}
				results[i], errs[i] = r.runMutant(ctx, dir, tmp, &mutants[i], tests)
			}
		})
	}
	// Dispatch non-schema mutants first so their individual builds overlap
	// with fast schema runs instead of sitting at the tail.
	for i := range mutants {
		if reused[i] {
			results[i] = reusedResults[i]
			continue
		}
		if !mutants[i].Schema {
			jobs <- i
		}
	}
	for i := range mutants {
		if mutants[i].Schema {
			jobs <- i
		}
	}
	close(jobs)
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return err
	}
	*deferRecheck = func() error {
		err := r.recheck(ctx, dir, schema, tests, results, mutants)
		for _, rs := range results {
			m.Results = append(m.Results, rs...)
		}
		if err == nil && useCache {
			saveCache(r.CacheDir, pkg.PkgPath, buildCacheEntry(snap, m, reusedKillers))
		}
		// recheck only returns ctx.Err() on cancellation.
		return err //mut:ignore
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
				limit := r.limit(t.BaselineMS)
				p.original = run(ctx, dir, bin, top, limit, 0, "")
				p.mutant = run(ctx, dir, bin, top, limit, mutants[i].ID, "")
				if ctx.Err() != nil {
					return ctx.Err()
				}
				ran[top] = p //mut:ignore caching recheck per top-level test changes cost, not verdicts
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
func (r Runner) buildSchema(ctx context.Context, pkg *packages.Package, dir, tmp string, sources []source, mutants []Mutant, skip []bool) (string, error) {
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
				if k >= 0 && !dropped[k] && !skip[k] {
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
		out, err := r.build(ctx, dir, bin, overlayFile)
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
func (r Runner) runTests(ctx context.Context, dir, bin string, mu *Mutant, tests []Test, reached, reachedSite map[string]map[int]bool, fn map[int]int) ([]Result, error) {
	if r.FailFast {
		return r.runTestsFailFast(ctx, dir, bin, mu, tests, reached, reachedSite, fn)
	}
	var results []Result
	var rep, again, original report
	ran, ranAgain := "", "" // the top-level tests rep, and again and original, are from
	for _, t := range tests {
		if t.State != OK {
			continue
		}
		if reached != nil && fn[mu.ID] >= 0 && !reached[topOf(t.Name)][fn[mu.ID]] {
			results = append(results, Result{MutantID: mu.ID, TestID: t.ID, Status: Survived, Skipped: true})
			continue
		}
		if top := topOf(t.Name); top != ran { //mut:ignore caching top-level runs across subtests changes cost, not verdicts
			limit := r.limit(t.BaselineMS)
			rep, ran = r.run(ctx, dir, bin, top, limit, mu.ID, ""), top
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
		status := rep.status(t.Name)
		if status == Killed && r.Confirm {
			if ranAgain != ran { //mut:ignore caching confirm runs across subtests changes cost, not verdicts
				limit := r.limit(t.BaselineMS)
				again, ranAgain = r.run(ctx, dir, bin, ran, limit, mu.ID, ""), ran
				original = report{ok: true}
				// Cost-only skip: when again passed, every leaf becomes FlakyKill
				// and original is never read, so mutating this guard cannot change verdicts.
				if mu.Schema && !again.ok { //mut:ignore
					original = r.run(ctx, dir, bin, ran, limit, 0, "")
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
		results = append(results, Result{MutantID: mu.ID, TestID: t.ID, Status: status, MS: ms(rep.elapsed)})
	}
	r.logf("M%d done\n", mu.ID)
	return results, nil
}

// Fast-test batching bounds: tests below fastTestMS are grouped into one
// process invocation (up to maxBatchLen tests) to amortize process startup.
const (
	fastTestMS  = 50.0
	maxBatchLen = 32
)

type topGroup struct {
	name   string
	baseMS float64
	leaves []Test
}

func (r Runner) runTestsFailFast(ctx context.Context, dir, bin string, mu *Mutant, tests []Test, reached, reachedSite map[string]map[int]bool, fn map[int]int) ([]Result, error) {
	fnID := -1
	if fn != nil {
		if id, ok := fn[mu.ID]; ok {
			fnID = id
		}
	}
	byTop := map[string]int{}
	var groups []topGroup
	var uninfected []Result
	for _, t := range tests {
		if t.State != OK {
			continue
		}
		top := topOf(t.Name)
		if reached != nil && fnID >= 0 && !reached[top][fnID] {
			continue
		}
		// A test that ran the mutant's site and never saw its value differ
		// cannot kill it: it survives without running.
		if r.Infect && reachedSite != nil && reachedSite[top][mu.ID] && !reachedSite[top][-mu.ID] {
			uninfected = append(uninfected, Result{MutantID: mu.ID, TestID: t.ID, Status: Survived})
			continue
		}
		idx, ok := byTop[top]
		if !ok {
			idx = len(groups)
			byTop[top] = idx
			groups = append(groups, topGroup{name: top, baseMS: t.BaselineMS})
		}
		groups[idx].leaves = append(groups[idx].leaves, t)
	}
	// Fastest first: the order changes how many tests run, not verdicts.
	slices.SortStableFunc(groups, func(a, b topGroup) int { //mut:ignore
		return cmp.Or(cmp.Compare(a.baseMS, b.baseMS), cmp.Compare(a.name, b.name))
	})

	queue := makeBatches(groups)
	results := uninfected
	for len(queue) > 0 {
		batch := queue[0]
		queue = queue[1:]

		var sumMS float64
		names := make([]string, len(batch))
		for i, g := range batch {
			sumMS += g.baseMS
			names[i] = g.name
		}
		// The tests of a batch run one after another; each baseline also
		// counts a process start the batch pays once, which only loosens it.
		limit := r.limit(sumMS)
		rep := r.runBatch(ctx, dir, bin, names, limit, mu.ID)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if len(batch) == 1 {
			stop, err := r.recordSingle(ctx, dir, bin, mu, batch[0], rep, &results)
			if err != nil {
				return nil, err
			}
			if stop {
				break
			}
			continue
		}

		// Multi-test batch: all passed.
		if rep.ok {
			for _, g := range batch {
				for _, t := range g.leaves {
					results = append(results, Result{MutantID: mu.ID, TestID: t.ID, Status: Survived, MS: ms(rep.elapsed)})
				}
			}
			continue
		}

		// Multi-test batch failed or timed out: separate finished tests from
		// unfinished ones so unrun tests are never misattributed.
		var failedIdx = -1
		var unfinished []topGroup
		for i, g := range batch {
			if topFailed(g, rep) {
				if failedIdx < 0 {
					failedIdx = i
				}
				continue
			}
			if passed, done := rep.passed[g.name]; done && passed {
				for _, t := range g.leaves {
					results = append(results, Result{MutantID: mu.ID, TestID: t.ID, Status: Survived, MS: ms(rep.elapsed)})
				}
				continue
			}
			unfinished = append(unfinished, g)
		}

		if failedIdx >= 0 {
			killed, err := r.recordSingle(ctx, dir, bin, mu, batch[failedIdx], rep, &results)
			if err != nil {
				return nil, err
			}
			if killed {
				break
			}
			if len(unfinished) > 0 {
				queue = append([][]topGroup{unfinished}, queue...)
			}
			continue
		}

		// No explicit FAIL line: either a timeout or a crash/panic.
		var startedUnfinished []topGroup
		var rest []topGroup
		for _, g := range unfinished {
			if rep.started[g.name] { //mut:ignore falling back to single runs changes cost, not verdicts
				startedUnfinished = append(startedUnfinished, g)
			} else {
				rest = append(rest, g)
			}
		}

		if len(startedUnfinished) == 1 { //mut:ignore falling back to single runs changes cost, not verdicts
			g := startedUnfinished[0]
			if rep.timedOut {
				if r.Confirm {
					// Under load a fast test can hit its deadline; recheck alone
					// before letting an unconfirmed timeout stop FailFast.
					limit := r.recheckLimit(g.baseMS)
					again := r.run(ctx, dir, bin, g.name, limit, mu.ID, "")
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					if !again.timedOut { //mut:ignore delegating a confirmed timeout to recordSingle changes cost, not verdicts
						stop, err := r.recordSingle(ctx, dir, bin, mu, g, again, &results)
						if err != nil {
							return nil, err
						}
						if stop {
							break
						}
						if len(rest) > 0 {
							queue = append([][]topGroup{rest}, queue...)
						}
						continue
					}
				}
				for _, t := range g.leaves {
					results = append(results, Result{MutantID: mu.ID, TestID: t.ID, Status: Timeout, MS: ms(rep.elapsed)})
				}
				// A timed-out mutant cannot survive; stop FailFast rather
				// than waiting out repeated timeouts on the same loop.
				break
			}
			stop, err := r.recordSingle(ctx, dir, bin, mu, g, rep, &results)
			if err != nil {
				return nil, err
			}
			if stop {
				break
			}
			if len(rest) > 0 {
				queue = append([][]topGroup{rest}, queue...)
			}
			continue
		}

		// Ambiguous attribution (e.g. t.Parallel() tests crashing/timing out
		// together): fall back to running unfinished tests individually.
		singles := make([][]topGroup, len(unfinished))
		for i, g := range unfinished {
			singles[i] = []topGroup{g}
		}
		queue = append(singles, queue...)
	}
	r.logf("M%d done\n", mu.ID)
	return results, nil
}

// limit is how long a run of tests whose baselines add up to baseMS may
// take before it times out.
func (r Runner) limit(baseMS float64) time.Duration {
	return time.Duration(baseMS*r.TimeoutFactor*float64(time.Millisecond)) + r.TimeoutPad
}

// recheckLimit is the limit of the run that confirms a timeout. Thrice the
// first: a run that hit a stall on a busy machine must not time out again
// and hide a mutant that survives.
func (r Runner) recheckLimit(baseMS float64) time.Duration {
	return 3 * r.limit(baseMS)
}

func makeBatches(groups []topGroup) [][]topGroup {
	var batches [][]topGroup
	var cur []topGroup
	flush := func() {
		if len(cur) > 0 { //mut:ignore empty batches are no-ops and change cost, not verdicts
			batches = append(batches, cur)
			cur = nil
		}
	}
	for _, g := range groups {
		if g.baseMS >= fastTestMS { //mut:ignore batching threshold changes cost, not verdicts
			flush() //mut:ignore batch boundary order changes cost, not verdicts
			batches = append(batches, []topGroup{g})
			continue
		}
		if len(cur) >= maxBatchLen { //mut:ignore a batch's length changes its cost, not verdicts
			flush() //mut:ignore a batch's length changes its cost, not verdicts
		}
		cur = append(cur, g)
	}
	flush()
	return batches
}

func topFailed(g topGroup, rep report) bool {
	if passed, ok := rep.passed[g.name]; ok && !passed {
		return true
	}
	for _, t := range g.leaves {
		if passed, ok := rep.passed[t.Name]; ok && !passed {
			return true
		}
	}
	return false
}

func (r Runner) recordSingle(ctx context.Context, dir, bin string, mu *Mutant, g topGroup, rep report, results *[]Result) (bool, error) {
	if rep.timedOut && r.Confirm {
		limit := r.recheckLimit(g.baseMS)
		again := r.run(ctx, dir, bin, g.name, limit, mu.ID, "")
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		rep = again
	}
	var again, original report
	confirmed := false
	killed := false
	timedOut := false
	for _, t := range g.leaves {
		status := rep.status(t.Name)
		if status == Killed && r.Confirm {
			if !confirmed { //mut:ignore caching confirm runs across subtests changes cost, not verdicts
				limit := r.limit(g.baseMS)
				again = r.run(ctx, dir, bin, g.name, limit, mu.ID, "")
				original = report{ok: true}
				// Cost-only skip: when again passed, every leaf becomes FlakyKill
				// and original is never read, so mutating this guard cannot change verdicts.
				if mu.Schema && !again.ok { //mut:ignore
					original = r.run(ctx, dir, bin, g.name, limit, 0, "")
				}
				if ctx.Err() != nil {
					return false, ctx.Err()
				}
				confirmed = true //mut:ignore caching confirm runs across subtests changes cost, not verdicts
			}
			switch {
			case again.status(t.Name) != Killed:
				status = FlakyKill
			case original.status(t.Name) != Survived:
				status = recheck
			}
		}
		if status == Killed {
			killed = true
		}
		if status == Timeout {
			timedOut = true
		}
		*results = append(*results, Result{MutantID: mu.ID, TestID: t.ID, Status: status, MS: ms(rep.elapsed)})
	}
	return killed || timedOut, nil
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
		rep := r.run(ctx, dir, bin, top, 0, 0, reach)
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
	_, buildErr := r.build(ctx, dir, bin, overlayFile)
	mu.BuildMS = ms(time.Since(start))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	mu.Compiled = buildErr == nil
	if !mu.Compiled {
		r.logf("M%d build error\n", mu.ID)
		return nil, nil
	}
	return r.runTests(ctx, dir, bin, mu, tests, nil, nil, nil)
}

func (r Runner) build(ctx context.Context, dir, bin, overlay string) ([]byte, error) {
	r.acquire()
	defer r.release()
	return build(ctx, dir, bin, overlay)
}

func (r Runner) run(ctx context.Context, dir, bin, top string, limit time.Duration, id int, reach string) report {
	r.acquire()
	defer r.release()
	return run(ctx, dir, bin, top, limit, id, reach)
}

func (r Runner) runBatch(ctx context.Context, dir, bin string, tops []string, limit time.Duration, id int) report {
	r.acquire()
	defer r.release()
	return runBatch(ctx, dir, bin, tops, limit, id)
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

// runBatch runs one or more top-level tests of bin in one process, stopping
// at the first failing test when more than one is given.
func runBatch(ctx context.Context, dir, bin string, tops []string, limit time.Duration, id int) report {
	if len(tops) == 1 {
		return run(ctx, dir, bin, tops[0], limit, id, "")
	}
	if limit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limit)
		defer cancel()
	}
	quoted := make([]string, len(tops))
	for i, top := range tops {
		quoted[i] = regexp.QuoteMeta(top)
	}
	pattern := "^(" + strings.Join(quoted, "|") + ")$"
	cmd := command(ctx, bin, append(testArgs(pattern, limit), "-test.failfast")...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%d", mutant.EnvVar, id))
	start := time.Now()
	out, err := output(cmd)
	return (&report{
		ok:       err == nil,
		timedOut: limit > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded),
		elapsed:  time.Since(start),
	}).parse(out)
}

// testArgs runs the tests that pattern matches once, reporting as
// test2json reads, with a deadline the test binary enforces itself: twice
// limit, past the one killmatrix enforces, or go test's 10 minutes when
// there is no limit. A binary orphaned with its killer, as when mutating
// killmatrix runs killmatrix's own tests, then still stops.
func testArgs(pattern string, limit time.Duration) []string {
	deadline := 10 * time.Minute
	if limit > 0 {
		deadline = 2*limit + time.Second
	}
	return []string{"-test.run", pattern, "-test.count=1", "-test.v=test2json", "-test.timeout=" + deadline.String()}
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
	cmd := command(ctx, bin, testArgs("^"+top+"$", limit)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%d", mutant.EnvVar, id))
	if reach != "" {
		cmd.Env = append(cmd.Env, mutant.ReachEnvVar+"="+reach)
	}
	start := time.Now()
	out, err := output(cmd)
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
