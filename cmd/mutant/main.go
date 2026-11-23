// Command mutant runs mutation testing on Go packages and lists the
// mutants no test kills, each as the line before and after:
//
//	mutant ./...               every package, one mutant per line
//	mutant -diff main ./...    only lines changed since main
//	mutant -json ./...         one JSON object per mutant, for tools
//
// It builds each package once with its mutants embedded (schemata), runs
// only the tests that reach a mutant's function, and stops at a mutant's
// first kill. Equivalent mutants it can recognize never run, nor, without
// -arid, mutants in code no one writes a test for: logging, metrics,
// capacity hints, and lines marked //mut:ignore.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ibrhmkoz/go-kit/mutant"
	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
)

func main() {
	var o options
	flag.BoolVar(&o.all, "all", false, "run every mutant on a line, not one")
	flag.BoolVar(&o.arid, "arid", false, "also run mutants in logging, metrics, capacity hints and lines marked mut:ignore")
	base := flag.String("diff", "", "only mutate lines changed since this git revision, working tree included")
	jobs := flag.Int("j", runtime.NumCPU(), "mutants to run at once")
	strict := flag.Bool("strict", false, "exit 1 when a mutant survives or is not covered")
	// CLI flag wiring in main() is not exercised by package unit tests.
	flag.BoolVar(&o.noCache, "no-cache", false, "ignore and do not update the verdict cache") //mut:ignore
	verbose := flag.Bool("v", false, "log progress to stderr")
	asJSON := flag.Bool("json", false, "print one JSON object per mutant, every verdict included, instead of the report")
	flag.Parse()
	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ok, err := run(ctx, o, *base, *jobs, *verbose, *asJSON, patterns)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mutant:", err)
		os.Exit(2)
	}
	if *strict && !ok {
		os.Exit(1)
	}
}

// run reports and returns whether every mutant run was killed or timed out.
func run(ctx context.Context, o options, base string, jobs int, verbose, asJSON bool, patterns []string) (bool, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return false, err
	}
	if base != "" {
		top, err := git(ctx, cwd, "rev-parse", "--show-toplevel")
		if err != nil {
			return false, err
		}
		root := strings.TrimSpace(string(top))
		diff, err := git(ctx, root, "diff", "-U0", "--no-color", "--no-ext-diff", base, "--", "*.go")
		if err != nil {
			return false, err
		}
		o.diff = parseDiff(diff, root)
	}
	var log io.Writer
	if verbose {
		log = os.Stderr
	}
	r := runner(o, jobs, log)
	start := time.Now()
	m, err := r.Run(ctx, cwd, patterns...)
	if err != nil {
		return false, err
	}
	if asJSON {
		fmt.Print(reportJSON(m, cwd))
	} else {
		fmt.Print(report(m, cwd, os.ReadFile))
		fmt.Printf("in %s\n", time.Since(start).Round(100*time.Millisecond))
	}
	return strictOK(m.Verdicts()), nil
}

// strictOK reports whether -strict passes: no mutant survived or went uncovered.
func strictOK(verdicts map[int]killmatrix.Verdict) bool {
	for _, v := range verdicts {
		if v == killmatrix.VerdictSurvived || v == killmatrix.VerdictNotCovered {
			return false
		}
	}
	return true
}

func runner(o options, jobs int, log io.Writer) killmatrix.Runner {
	cacheDir := ""
	if !o.noCache {
		// os.UserCacheDir fails only when the environment lacks a cache dir.
		if dir, err := os.UserCacheDir(); err == nil { //mut:ignore
			cacheDir = filepath.Join(dir, "mutant")
		}
	}
	return killmatrix.Runner{
		Workers:      jobs,
		BaselineRuns: 1,
		// A test's baseline time already holds its process start; a 100 ms
		// pad over 3× that times out no run that would finish, on a loaded
		// machine or on flaky services, and halves the corpus' time (§18).
		TimeoutPad: 100 * time.Millisecond,
		Log:        log,
		Schemata:   true,
		Select:     true,
		FailFast:   true,
		Infect:     true, // §19
		Confirm:    true,
		CacheDir:   cacheDir,
		Choose:     func(path string, ms []mutant.Mutant) []int { return choose(o, path, ms) },
	}
}

func git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exit.Stderr)))
	}
	return out, err
}
