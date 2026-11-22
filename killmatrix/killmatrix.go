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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	Compiled bool            `json:"compiled"`
	BuildMS  float64         `json:"build_ms"`

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

// Test is a top-level test, example or fuzz target. BaselineMS is its median
// time on the unmutated package.
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
)

// Result is one test run against one mutant.
type Result struct {
	MutantID int     `json:"mutant_id"`
	TestID   int     `json:"test_id"`
	Status   Status  `json:"status"`
	MS       float64 `json:"ms"`
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
}

// Run computes the kill matrix of the packages that patterns match in dir.
func (r Runner) Run(ctx context.Context, dir string, patterns ...string) (*Matrix, error) {
	r = r.withDefaults()
	cfg := &packages.Config{
		Context: ctx,
		Dir:     dir,
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
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

func (r Runner) runPackage(ctx context.Context, m *Matrix, pkg *packages.Package, tmp string) error {
	if len(pkg.GoFiles) == 0 {
		return nil
	}
	dir := filepath.Dir(pkg.GoFiles[0])
	bin := filepath.Join(tmp, strings.ReplaceAll(pkg.PkgPath, "/", "_")+".test")
	if out, err := build(ctx, dir, bin, ""); err != nil {
		return fmt.Errorf("baseline build: %w\n%s", err, out)
	}
	if _, err := os.Stat(bin); errors.Is(err, os.ErrNotExist) {
		return nil // no test files
	}

	names, err := list(ctx, dir, bin)
	if err != nil {
		return err
	}
	firstTest := len(m.Tests)
	for _, name := range names {
		m.Tests = append(m.Tests, r.baseline(ctx, dir, bin, len(m.Tests)+1, pkg.PkgPath, name))
	}
	tests := m.Tests[firstTest:]
	r.logf("%s: %d tests\n", pkg.PkgPath, len(tests))

	firstMutant := len(m.Mutants)
	for _, f := range pkg.Syntax {
		name := pkg.Fset.File(f.Pos()).Name()
		if ast.IsGenerated(f) || !slices.Contains(pkg.GoFiles, name) {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		for _, mu := range mutant.Find(pkg.Fset, f, src, pkg.TypesInfo) {
			m.Mutants = append(m.Mutants, Mutant{
				ID:       len(m.Mutants) + 1,
				Pkg:      pkg.PkgPath,
				File:     filepath.Base(name),
				Line:     mu.Pos.Line,
				Col:      mu.Pos.Column,
				Operator: mu.Operator,
				Node:     mu.Node,
				Original: mu.Original,
				Mutated:  mu.Mutated,
				m:        mu,
			})
		}
	}
	mutants := m.Mutants[firstMutant:]
	r.logf("%s: %d mutants\n", pkg.PkgPath, len(mutants))

	results := make([][]Result, len(mutants))
	errs := make([]error, len(mutants))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range r.Workers {
		wg.Go(func() {
			for i := range jobs {
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
	for _, rs := range results {
		m.Results = append(m.Results, rs...)
	}
	return nil
}

// baseline runs a test BaselineRuns times on the unmutated package.
func (r Runner) baseline(ctx context.Context, dir, bin string, id int, pkg, name string) Test {
	var times []time.Duration
	passed := 0
	for range r.BaselineRuns {
		ok, elapsed, _ := run(ctx, dir, bin, name, 0)
		times = append(times, elapsed)
		if ok {
			passed++
		}
	}
	slices.Sort(times)
	state := OK
	switch passed {
	case 0:
		state = Failing
	case r.BaselineRuns:
	default:
		state = Flaky
	}
	return Test{ID: id, Pkg: pkg, Name: name, BaselineMS: ms(times[len(times)/2]), State: state}
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

	var results []Result
	for _, t := range tests {
		if t.State != OK {
			continue
		}
		limit := time.Duration(float64(t.BaselineMS)*r.TimeoutFactor*float64(time.Millisecond)) + r.TimeoutPad
		ok, elapsed, timedOut := run(ctx, dir, bin, t.Name, limit)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		status := Killed
		switch {
		case timedOut:
			status = Timeout
		case ok:
			status = Survived
		}
		results = append(results, Result{MutantID: mu.ID, TestID: t.ID, Status: status, MS: ms(elapsed)})
	}
	r.logf("M%d done\n", mu.ID)
	return results, nil
}

// build compiles the test binary of the package in dir into bin, with the
// files in overlay, if given, replacing the originals.
func build(ctx context.Context, dir, bin, overlay string) ([]byte, error) {
	args := []string{"test", "-c", "-o", bin}
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

// run runs one test of bin in dir. A limit of 0 means no time limit.
func run(ctx context.Context, dir, bin, name string, limit time.Duration) (ok bool, elapsed time.Duration, timedOut bool) {
	if limit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limit)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, bin, "-test.run", "^"+name+"$", "-test.count=1")
	cmd.Dir = dir
	start := time.Now()
	err := cmd.Run()
	elapsed = time.Since(start)
	return err == nil, elapsed, limit > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded)
}

// Log is progress only; a failed write must not stop a run.
func (r Runner) logf(format string, args ...any) { _, _ = fmt.Fprintf(r.Log, format, args...) }

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// String draws the matrix a package at a time: K killed, . survived,
// T timeout, B build error. Flaky and failing tests get no column.
func (m *Matrix) String() string {
	status := map[[2]int]Status{}
	for _, r := range m.Results {
		status[[2]int{r.MutantID, r.TestID}] = r.Status
	}
	glyph := map[Status]string{Killed: "K", Survived: ".", Timeout: "T"}

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
