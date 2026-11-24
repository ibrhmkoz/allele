package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/ibrhmkoz/allele/internal/killmatrix"
	"github.com/ibrhmkoz/allele/internal/mutant"
	"github.com/ibrhmkoz/allele/pkg/gotest"
)

// infections returns, for each mutant of the package in dir, keyed by
// mutantKey, how many top-level tests of the old suite reach its site and how
// many infect it, from a schema binary run once per test with
// mutant.ReachEnvVar set.
func infections(ctx context.Context, dir string, revealing []string) (map[string][2]int, error) {
	// go's -overlay matches absolute paths only.
	dir, err := filepath.Abs(dir)
	if err != nil { //allele:ignore Abs fails only without a working directory
		return nil, err //allele:ignore Abs fails only without a working directory
	}
	cfg := &packages.Config{Context: ctx, Dir: dir, Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil { //allele:ignore go list failing outright; a bad package shows as no package below
		return nil, err //allele:ignore go list failing outright
	}
	if len(pkgs) != 1 || len(pkgs[0].Syntax) == 0 {
		return nil, fmt.Errorf("%s: %d packages", dir, len(pkgs))
	}
	p := pkgs[0]
	tmp, err := os.MkdirTemp("", "faults-infect")
	if err != nil { //allele:ignore the OS refusing a temp dir
		return nil, err //allele:ignore the OS refusing a temp dir
	}
	defer func() { _ = os.RemoveAll(tmp) }() //allele:ignore a leaked temp dir changes no count
	keys := map[int]string{}
	counts := map[string][2]int{}
	var files [][2]string // name, embedded source path
	srcs := map[string][]byte{}
	id := 1
	for i, f := range p.Syntax {
		name := p.CompiledGoFiles[i]
		src, _ := os.ReadFile(name)
		ms := mutant.Find(p.Fset, f, src, p.TypesInfo)
		ids := make([]int, len(ms))
		for k := range ms {
			ids[k] = id
			id++
		}
		emb, embedded := mutant.Embed(p.Fset, f, src, p.TypesInfo, ids)
		for k, m := range ms {
			if embedded[k] { //allele:ignore a site Embed leaves out would only add a count of zero
				key := mutantKey(filepath.Base(name), m.Pos.Line, m.Pos.Column, string(m.Operator), m.Original, m.Mutated)
				keys[ids[k]] = key
				counts[key] = [2]int{}
			}
		}
		srcs[name] = emb
		files = append(files, [2]string{name, filepath.Join(tmp, filepath.Base(name))})
	}
	bin := filepath.Join(tmp, "p.test")
	build := func(go121 bool) error {
		replace := map[string]string{}
		for _, f := range files {
			src := srcs[f[0]]
			if go121 { //allele:ignore the Go 1.21 shim matters only for modules older than the toolchain
				src = mutant.WithGo121(src, f[0]) //allele:ignore the Go 1.21 shim matters only for modules older than the toolchain
			}
			if err := os.WriteFile(f[1], src, 0o600); err != nil {
				return err //allele:ignore the OS refusing a temp file
			}
			replace[f[0]] = f[1]
		}
		rt := filepath.Join(tmp, "zz_mutant_runtime.go")
		if err := os.WriteFile(rt, mutant.Runtime(p.Name), 0o600); err != nil { //allele:ignore the OS refusing a temp file
			return err //allele:ignore the OS refusing a temp file
		}
		replace[filepath.Join(dir, "zz_mutant_runtime.go")] = rt
		ov, _ := json.Marshal(map[string]any{"Replace": replace})
		ovPath := filepath.Join(tmp, "overlay.json")
		if err := os.WriteFile(ovPath, ov, 0o600); err != nil {
			return err //allele:ignore the OS refusing a temp file
		}
		if out, err := gotest.Build(ctx, dir, bin, ovPath); err != nil { //allele:ignore a failed build fails the -test.list run after it
			return fmt.Errorf("%w: %s", err, out) //allele:ignore a failed build fails the -test.list run after it
		}
		return nil
	}
	if err := build(true); err != nil { //allele:ignore only a module the Go 1.21 shim breaks takes the fallback
		if err := build(false); err != nil { //allele:ignore a failed build fails the -test.list run after it
			return nil, err
		}
	}
	list, err := gotest.List(ctx, dir, bin)
	if err != nil { //allele:ignore a binary that built lists its tests
		return nil, err //allele:ignore a binary that built lists its tests
	}
	for _, test := range list {
		run, skip := oldSuite(test, revealing)
		if !runnable(test) || !run {
			continue
		}
		reach := filepath.Join(tmp, "reach")
		_ = os.Remove(reach)
		gotest.Run(ctx, bin, gotest.Options{Dir: dir, Tests: []string{test}, Skip: skip, Limit: 2 * time.Minute, Env: []string{mutant.ReachEnvVar + "=" + reach}}) //allele:ignore the limit only stops a test that hangs
		data, _ := os.ReadFile(reach)
		s := bufio.NewScanner(bytes.NewReader(data))
		for s.Scan() {
			n, err := strconv.Atoi(s.Text())
			if err != nil { //allele:ignore a line that is no number parses as 0, which is no id
				continue
			}
			i := 0
			if n < 0 {
				n, i = -n, 1
			}
			// Every id the binary writes is one of keys; ok guards a stale file.
			if key, ok := keys[n]; ok { //allele:ignore every id written is in keys
				c := counts[key]
				c[i]++
				counts[key] = c
			}
		}
	}
	return counts, nil
}

// runnable reports whether -test.list's line names a test, not the
// benchmark or the "ok" line it may also print.
func runnable(name string) bool {
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Example") || strings.HasPrefix(name, "Fuzz")
}

// oldSuite says how to run top-level test top as part of the old suite:
// not at all when it is itself revealing, else with -test.skip skip, which
// leaves out its revealing subtests (cmd/faults counts a mutant coupled at
// the subtest level, so their siblings stay). A revealing subtest deeper
// than one level skips the whole first-level subtest holding it.
func oldSuite(top string, revealing []string) (run bool, skip string) {
	var subs []string
	for _, t := range revealing {
		first, rest, nested := strings.Cut(t, "/")
		if first != top {
			continue
		}
		if !nested {
			return false, ""
		}
		sub, _, _ := strings.Cut(rest, "/")
		if q := "^" + regexp.QuoteMeta(sub) + "$"; !slices.Contains(subs, q) {
			subs = append(subs, q)
		}
	}
	if len(subs) == 0 {
		return true, ""
	}
	return true, "^" + regexp.QuoteMeta(top) + "$/" + strings.Join(subs, "|")
}

func countOf(counts map[string][2]int, mu killmatrix.Mutant) [2]int {
	if c, ok := counts[mutantKey(mu.File, mu.Line, mu.Col, string(mu.Operator), mu.Original, mu.Mutated)]; ok {
		return c
	}
	return [2]int{-1, -1}
}

func mutantKey(file string, line, col int, op, original, mutated string) string {
	return fmt.Sprintf("%s:%d:%d %s %q→%q", file, line, col, op, original, mutated)
}

type detail struct {
	Repo       string `json:"repo"`
	Commit     string `json:"commit"`
	Pkg        string `json:"pkg"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Col        int    `json:"col"`
	Operator   string `json:"operator"`
	Original   string `json:"original"`
	Mutated    string `json:"mutated"`
	Equivalent string `json:"equivalent"`
	Arid       string `json:"arid"`
	Killable   bool   `json:"killable"`
	Coupled    bool   `json:"coupled"`
	OnFix      bool   `json:"on_fix"`
	Reached    int    `json:"reached"`  // old tests reaching the site, -1 unknown
	Infected   int    `json:"infected"` // old tests infecting the mutant, -1 unknown
}
