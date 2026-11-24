// Command bench scores builds of the mutant command on the corpus with one
// number, M, that weighs what they find against how long they take:
//
//	bench base=/tmp/mutant-main new=/tmp/mutant-branch
//
// M = D·F·((T0+Floor)/(T+Floor))^Gamma. D is the dominator score of tests
// written against the mutants a build reports, F one minus its weighted
// verdict errors against the exhaustive matrices in docs/data, and T its
// summed wall time on the ten corpus libraries, cold verdict cache, -j 8.
// The naive exhaustive runner scores 1. Builds run in turns, rep by rep,
// so that load drifts on all of them alike; T takes each repository's
// median rep.
//
// Clone the corpus first with docs/corpus.sh; it reads CORPUS_CACHE.
package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/modfile"

	"github.com/ibrhmkoz/allele/internal/killmatrix"
	"github.com/ibrhmkoz/allele/pkg/ndjson"
)

// repos is the corpus of M v2. M v1 (to 2026-11-24) ran the first six.
var repos = []string{"glob", "go-humanize", "go-version", "semver", "shlex", "uuid", "bloom", "sjson", "decimal", "xstrings"}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := cli(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()        //allele:ignore releasing the signal handler changes no result
	os.Exit(code) //allele:ignore the exit code is cli's, which the tests check
}

// cli runs bench with args and returns its exit code: 2 for bad usage, 1
// when a build fails or a table cannot be read or written.
func cli(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	home, _ := os.UserHomeDir()
	corpus := fs.String("corpus", cmp.Or(os.Getenv("CORPUS_CACHE"), filepath.Join(home, ".cache", "allele-corpus")), "directory holding the corpus clones")
	ref := fs.String("ref", "docs/data", "exhaustive kill matrices, one directory per repository")
	rerun := fs.String("rerun", "docs/data-rerun", "a second exhaustive run; only mutants both agree on count toward F")
	reps := fs.Int("reps", 3, "timed runs per build and repository")
	jobs := fs.Int("j", 8, "the -j each build runs with")
	seeds := fs.Int("seeds", 10, "simulations per repository for D")
	out := fs.String("o", "", "if set, a file to append one JSON row per run and per build to")
	only := fs.String("repos", strings.Join(repos, ","), "repositories to run")
	trees := fs.String("faults", "", "if set, real-fault trees (docs/faulttrees.sh output) to report R_5 on, a diagnostic M does not use")
	faultDetail := fs.String("fault-detail", "docs/data-faults/detail.ndjson.gz", "the coupled mutants of each fault (cmd/faults -detail)")
	if fs.Parse(args) != nil {
		return 2
	}
	if *reps < 1 || *seeds < 1 {
		_, _ = fmt.Fprintln(stderr, "bench: -reps and -seeds must be at least 1")
		return 2
	}
	bins, err := parseBins(fs.Args())
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "bench:", err)
		return 2
	}
	b := bench{corpus: *corpus, ref: *ref, rerun: *rerun, reps: *reps, jobs: *jobs, seeds: *seeds, repos: strings.Split(*only, ","), stdout: stdout, stderr: stderr}
	if *trees != "" {
		if b.faults, err = readFaults(*trees, *faultDetail); err != nil {
			_, _ = fmt.Fprintln(stderr, "bench:", err)
			return 1
		}
	}
	if *out != "" {
		f, err := os.OpenFile(*out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "bench:", err)
			return 1
		}
		defer func() { _ = f.Close() }() //allele:ignore a leaked descriptor changes no result
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
	faults             []fault
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
	for _, repo := range b.repos {
		m, err := killmatrix.Read(filepath.Join(b.ref, repo))
		if err != nil {
			return err
		}
		// Without a rerun every mutant counts as stable.
		var again *killmatrix.Matrix
		if _, err := os.Stat(filepath.Join(b.rerun, repo)); !errors.Is(err, fs.ErrNotExist) {
			if again, err = killmatrix.Read(filepath.Join(b.rerun, repo)); err != nil {
				return err
			}
		}
		module, err := modulePath(filepath.Join(b.corpus, repo))
		if err != nil {
			return err
		}
		refs[repo] = newReference(module, m, again)
	}
	walls := map[[2]string][]float64{}
	worst := map[[2]string]score{}
	// Rep 0 warms the build cache and is not timed. Builds take turns, in
	// reverse order every other rep, so none always runs on the caches the
	// one before it warmed.
	for rep := range b.reps + 1 {
		for _, repo := range b.repos {
			order := slices.Clone(bins)
			if rep%2 == 1 {
				slices.Reverse(order)
			}
			for _, bn := range order {
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
				// A rep's verdicts can differ under load; the worst rep counts.
				if s := refs[repo].grade(repo, vs, b.seeds); rep == 1 || s.worse(worst[k]) {
					worst[k] = s
				}
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
			s := worst[k]
			s.Bin = bn.name
			med := median(walls[k])
			total += med
			scores = append(scores, s)
			b.emit(s)
			_, _ = fmt.Fprintf(b.stdout, "%-6s %-12s %5d %5d %6.3f %6d %5d %5d %5d %5d %6.2fs\n", bn.name, repo, s.Reported, s.Unknown, s.D, s.Stable, s.Hidden, s.False, s.Timeout, s.Build, med)
		}
		m := combine(bn.name, scores, total)
		if len(b.faults) > 0 {
			r, err := b.faultR(ctx, bn)
			if err != nil {
				return err
			}
			m.R = r
		}
		b.emit(m)
		_, _ = fmt.Fprintf(b.stdout, "%-6s M = %.4f  (D %.4f, R %.4f, F %.4f, T %.2fs)\n", bn.name, m.M, m.D, m.R, m.F, m.T)
	}
	return nil
}

func (b bench) emit(row any) {
	if b.rows != nil {
		_ = b.rows.Encode(row)
	}
}

func (b bench) faultR(ctx context.Context, bn bin) (float64, error) {
	sum := 0.0
	for _, f := range b.faults {
		vs, err := b.mutantOn(ctx, bn, f.tree, "./"+f.pkg)
		if err != nil {
			return 0, fmt.Errorf("%s on fault %s: %w", bn.name, f, err)
		}
		fs := faultScore{Bin: bn.name, Fault: f.String()}
		for _, v := range vs {
			if v.Verdict == "survived" || v.Verdict == "not covered" {
				fs.Survivors++
				if f.coupled[toolKey(v)] {
					fs.Coupled++
				}
			}
		}
		fs.R = revealed(fs.Survivors, fs.Coupled, 5)
		b.emit(fs)
		sum += fs.R
	}
	return sum / float64(len(b.faults)), nil
}

func (b bench) mutant(ctx context.Context, bn bin, dir string) ([]verdict, error) {
	return b.mutantOn(ctx, bn, dir, "./...")
}

func (b bench) mutantOn(ctx context.Context, bn bin, dir, pattern string) ([]verdict, error) {
	cmd := exec.CommandContext(ctx, bn.path, "-no-cache", "-json", "-j", fmt.Sprint(b.jobs), pattern)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, stderr.Bytes())
	}
	return ndjson.Decode[verdict](bytes.NewReader(out))
}

func modulePath(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", err
	}
	if path := modfile.ModulePath(data); path != "" {
		return path, nil
	}
	return "", fmt.Errorf("%s: no module line", dir)
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
