// Command bench scores builds of the mutant command on the corpus with one
// number, M, that weighs what they find against how long they take:
//
//	bench base=/tmp/mutant-main new=/tmp/mutant-branch
//
// M = D·F·((T0+Floor)/(T+Floor))^Gamma. D is the dominator score of tests
// written against the mutants a build reports, F one minus its weighted
// verdict errors against the exhaustive matrices in docs/data, and T its
// summed wall time on the six corpus libraries, cold verdict cache, -j 8.
// The naive exhaustive runner scores 1. Builds run in turns, rep by rep,
// so that load drifts on all of them alike; T takes each repository's
// median rep.
//
// Clone the corpus first with docs/corpus.sh; it reads CORPUS_CACHE.
package main

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
)

var repos = []string{"glob", "go-humanize", "go-version", "semver", "shlex", "uuid"}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()        //mut:ignore releasing the signal handler changes no result
	os.Exit(code) //mut:ignore the exit code is cli's, which the tests check
}

// cli runs bench with args and returns its exit code: 2 for bad usage, 1
// when a build fails or a table cannot be read or written.
func cli(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	home, _ := os.UserHomeDir()
	corpus := fs.String("corpus", cmp.Or(os.Getenv("CORPUS_CACHE"), filepath.Join(home, ".cache", "go-kit-mutant-corpus")), "directory holding the corpus clones")
	ref := fs.String("ref", "mutant/docs/data", "exhaustive kill matrices, one directory per repository")
	rerun := fs.String("rerun", "mutant/docs/data-rerun", "a second exhaustive run; only mutants both agree on count toward F")
	reps := fs.Int("reps", 3, "timed runs per build and repository")
	jobs := fs.Int("j", 8, "the -j each build runs with")
	seeds := fs.Int("seeds", 10, "simulations per repository for D")
	out := fs.String("o", "", "if set, a file to append one JSON row per run and per build to")
	only := fs.String("repos", strings.Join(repos, ","), "repositories to run")
	if fs.Parse(args) != nil {
		return 2
	}
	bins, err := parseBins(fs.Args())
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "bench:", err)
		return 2
	}
	b := bench{corpus: *corpus, ref: *ref, rerun: *rerun, reps: *reps, jobs: *jobs, seeds: *seeds, repos: strings.Split(*only, ","), stdout: stdout, stderr: stderr}
	if *out != "" {
		f, err := os.OpenFile(*out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "bench:", err)
			return 1
		}
		defer func() { _ = f.Close() }() //mut:ignore a leaked descriptor changes no result
		b.rows = json.NewEncoder(f)
	}
	if err := b.run(ctx, bins); err != nil {
		_, _ = fmt.Fprintln(stderr, "bench:", err)
		return 1
	}
	return 0
}

type bin struct{ name, path string }

func parseBins(args []string) ([]bin, error) {
	if len(args) == 0 {
		return nil, errors.New("usage: bench [flags] name=path-to-mutant ...: no build given")
	}
	var bins []bin
	for _, a := range args {
		name, path, ok := strings.Cut(a, "=")
		if !ok {
			name, path = filepath.Base(a), a
		}
		abs, _ := filepath.Abs(path) // fails only when the working directory is gone
		bins = append(bins, bin{name, abs})
	}
	return bins, nil
}

type bench struct {
	corpus, ref, rerun string
	reps, jobs, seeds  int
	repos              []string
	rows               *json.Encoder
	stdout, stderr     io.Writer
}

type timing struct {
	Bin  string  `json:"bin"`
	Repo string  `json:"repo"`
	Rep  int     `json:"rep"`
	Wall float64 `json:"wall_s"`
}

func (b bench) run(ctx context.Context, bins []bin) error {
	refs := map[string]*reference{}
	modules := map[string]string{}
	for _, repo := range b.repos {
		m, err := killmatrix.Read(filepath.Join(b.ref, repo))
		if err != nil {
			return err
		}
		// Without a rerun every mutant counts as stable.
		again, _ := killmatrix.Read(filepath.Join(b.rerun, repo))
		refs[repo] = newReference(m, again)
		if modules[repo], err = moduleDir(filepath.Join(b.corpus, repo)); err != nil {
			return err
		}
	}
	walls := map[[2]string][]float64{}
	last := map[[2]string][]verdict{}
	// Rep 0 warms the build cache and is not timed.
	for rep := range b.reps + 1 {
		for _, repo := range b.repos {
			for _, bn := range bins {
				start := time.Now()
				vs, err := b.mutant(ctx, bn, filepath.Join(b.corpus, repo))
				if err != nil {
					return fmt.Errorf("%s on %s: %w", bn.name, repo, err)
				}
				wall := time.Since(start).Seconds()
				if rep < 1 {
					continue
				}
				k := [2]string{bn.name, repo}
				walls[k] = append(walls[k], wall)
				last[k] = vs
				b.emit(timing{bn.name, repo, rep, wall})
				_, _ = fmt.Fprintf(b.stderr, "rep %d %-12s %-6s %6.2fs\n", rep, repo, bn.name, wall)
			}
		}
	}
	_, _ = fmt.Fprintf(b.stdout, "%-6s %-12s %5s %5s %6s %6s %5s %5s %5s %5s %7s\n", "bin", "repo", "rep", "unk", "D", "stable", "hide", "false", "tout", "build", "T")
	for _, bn := range bins {
		var scores []score
		total := 0.0
		for _, repo := range b.repos {
			k := [2]string{bn.name, repo}
			s := refs[repo].grade(repo, modules[repo], last[k], b.seeds)
			med := median(walls[k])
			total += med
			scores = append(scores, s)
			b.emit(s)
			_, _ = fmt.Fprintf(b.stdout, "%-6s %-12s %5d %5d %6.3f %6d %5d %5d %5d %5d %6.2fs\n", bn.name, repo, s.Reported, s.Unknown, s.D, s.Stable, s.Hidden, s.False, s.Timeout, s.Build, med)
		}
		m := combine(bn.name, scores, total)
		b.emit(m)
		_, _ = fmt.Fprintf(b.stdout, "%-6s M = %.4f  (D %.4f, F %.4f, T %.2fs)\n", bn.name, m.M, m.D, m.F, m.T)
	}
	return nil
}

func (b bench) emit(row any) {
	if b.rows != nil {
		_ = b.rows.Encode(row)
	}
}

// mutant runs one build on one repository and returns its verdicts.
func (b bench) mutant(ctx context.Context, bn bin, dir string) ([]verdict, error) {
	cmd := exec.CommandContext(ctx, bn.path, "-no-cache", "-json", "-j", fmt.Sprint(b.jobs), "./...")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.Bytes())
	}
	var vs []verdict
	s := bufio.NewScanner(bytes.NewReader(out))
	s.Buffer(nil, 1<<20)
	for s.Scan() {
		var v verdict
		if err := json.Unmarshal(s.Bytes(), &v); err != nil {
			return nil, err
		}
		vs = append(vs, v)
	}
	return vs, s.Err()
}

// moduleDir is the last element of the module path in dir's go.mod: the
// directory name the reference's import paths give the module's root.
func moduleDir(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	for line := range strings.Lines(string(data)) {
		if path, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			path = strings.Trim(strings.TrimSpace(path), `"`)
			return path[strings.LastIndex(path, "/")+1:], nil
		}
	}
	return "", cmp.Or(err, fmt.Errorf("%s: no module line", dir))
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Sorted(slices.Values(xs))
	if len(s)%2 == 1 {
		return s[len(s)/2]
	}
	return (s[len(s)/2-1] + s[len(s)/2]) / 2
}
