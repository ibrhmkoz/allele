// Package gotest builds Go test binaries and runs their tests: it reports
// how every test did, subtests included, stops a run at a time limit, and
// leaves no process behind, even one a test leaked.
package gotest

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Build compiles the test binary of the package in dir into bin, with the
// files overlay (a go build -overlay file) names replacing the originals,
// if given. Vet is off: it rejects code that compiles, such as
// c != 'a' || c != 'b'. The output is go's, for a failed build.
func Build(ctx context.Context, dir, bin, overlay string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "go", buildArgs(bin, overlay)...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// buildArgs is Build's go command line. It is a function of its own so
// that a test can check it without a build: TestBuildOverlay checks the
// same through two builds, too slow to kill a mutant reliably under load.
func buildArgs(bin, overlay string) []string {
	args := []string{"test", "-c", "-vet=off", "-ldflags=-s -w", "-o", bin}
	if overlay != "" {
		args = append(args, "-overlay", overlay)
	}
	return args
}

// List returns the tests, examples and fuzz targets of a test binary run
// in dir. Benchmarks are left out: Run does not run them.
func List(ctx context.Context, dir, bin string) ([]string, error) {
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

// Options says what Run runs and how.
type Options struct {
	// Dir is the directory the binary runs in, as go test runs it in the
	// package's.
	Dir string
	// Tests are the top-level tests to run, in one process.
	Tests []string
	// Skip, if set, is a -test.skip pattern.
	Skip string
	// Limit stops the run once it has taken this long; 0 means no limit.
	Limit time.Duration
	// Env is added to the process's environment.
	Env []string
	// FailFast stops at the first test that fails.
	FailFast bool
}

// Run runs o.Tests of bin and reports how each test did.
func Run(ctx context.Context, bin string, o Options) Report {
	if o.Limit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Limit)
		defer cancel()
	}
	args := runArgs(o)
	cmd := command(ctx, bin, args...)
	cmd.Dir = o.Dir
	cmd.Env = append(os.Environ(), o.Env...)
	start := time.Now()
	out, err := output(cmd)
	// Only Run's own limit counts as a timeout, not a deadline ctx brought.
	r := Parse(out, err == nil, o.Limit > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded))
	r.Elapsed = time.Since(start)
	return r
}

// runArgs runs the tests o names once, reporting as test2json reads, with
// a deadline the test binary enforces itself: twice o.Limit, past the one
// Run enforces, or go test's 10 minutes when there is no limit. A binary
// orphaned with its runner, as when a mutation tool runs its own tests,
// then still stops.
func runArgs(o Options) []string {
	quoted := make([]string, len(o.Tests))
	for i, t := range o.Tests {
		quoted[i] = regexp.QuoteMeta(t)
	}
	deadline := 10 * time.Minute
	if o.Limit > 0 {
		deadline = 2*o.Limit + time.Second
	}
	args := []string{"-test.run", "^(" + strings.Join(quoted, "|") + ")$", "-test.count=1", "-test.v=test2json", "-test.timeout=" + deadline.String()}
	if o.Skip != "" {
		args = append(args, "-test.skip", o.Skip)
	}
	if o.FailFast {
		args = append(args, "-test.failfast")
	}
	return args
}

// Report is what one run showed.
type Report struct {
	// OK: the binary exited 0, every test it ran passed.
	OK bool
	// TimedOut: Run stopped it at its limit.
	TimedOut bool
	Elapsed  time.Duration
	// passed maps each test the run reported, at any depth, to whether it
	// passed. A skipped test passed: it did not fail.
	passed map[string]bool
	// started records every test name that printed "=== RUN".
	started map[string]bool
	// order lists the names in passed as the run reported them.
	order []string
}

// Passed reports whether the run reported name, at any depth, and whether
// it passed. A skipped test passed.
func (r Report) Passed(name string) (passed, reported bool) {
	passed, reported = r.passed[name]
	return passed, reported
}

// Started reports whether name began to run.
func (r Report) Started(name string) bool { return r.started[name] }

// Reported returns the names of the tests the run reported, in its order.
func (r Report) Reported() []string { return slices.Clone(r.order) }

// Outcome is how a test did.
type Outcome int

const (
	Pass Outcome = iota
	Fail
	// Timeout: the run stopped at its limit before the test reported.
	Timeout
)

func (o Outcome) String() string { return [...]string{"pass", "fail", "timeout"}[o] }

// Outcome is how leaf did in the run. A leaf the run did not report takes
// the outcome of the whole run: a panic in one subtest ends the binary
// before its siblings report, and a change that renames subtests makes
// the leaves it hid fail the run.
func (r Report) Outcome(leaf string) Outcome {
	if passed, ok := r.passed[leaf]; ok {
		if passed {
			return Pass
		}
		return Fail
	}
	switch {
	case r.TimedOut:
		return Timeout
	case r.OK:
		return Pass
	default:
		return Fail
	}
}

// Parse reads a run's -test.v=test2json output, the output Run asks for:
// ok is whether the binary exited 0, timedOut whether it was stopped at a
// limit. Only the result lines count; they start with ^V, which a test
// cannot fake by printing "--- FAIL".
func Parse(out []byte, ok, timedOut bool) Report {
	r := Report{OK: ok, TimedOut: timedOut, passed: map[string]bool{}, started: map[string]bool{}}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if name, ok := strings.CutPrefix(line, "\x16=== RUN   "); ok {
			if name = strings.TrimSpace(name); name != "" { //allele:ignore go test prints no RUN line without a name
				r.started[name] = true
			}
			continue
		}
		line, ok := strings.CutPrefix(line, "\x16--- ")
		if !ok {
			continue
		}
		verdict, rest, ok := strings.Cut(line, ": ")
		name, _, _ := strings.Cut(rest, " (")
		if !ok || name == "" { //allele:ignore go test prints no result line without a name
			continue
		}
		if verdict != "PASS" && verdict != "SKIP" && verdict != "FAIL" { //allele:ignore the other verdict, BENCH, needs benchmarks, which Run does not run
			continue
		}
		if _, seen := r.passed[name]; !seen { //allele:ignore with -test.count=1 a test reports once
			r.order = append(r.order, name)
		}
		r.passed[name] = verdict != "FAIL"
	}
	return r
}

// Leaves returns the names no other name extends with a subtest, in the
// order given.
func Leaves(names []string) []string {
	var leaves []string
	for _, n := range names {
		parent := false
		for _, o := range names {
			if strings.HasPrefix(o, n+"/") {
				parent = true
				break
			}
		}
		if !parent {
			leaves = append(leaves, n)
		}
	}
	return leaves
}

// Top is the top-level test a test name belongs to.
func Top(name string) string {
	top, _, _ := strings.Cut(name, "/")
	return top
}
