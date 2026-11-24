// Command faults measures how well mutants stand for real faults, on the
// bug fixes in a repository's history (roadmap C3):
//
//	faults -name uuid -o uuid.ndjson path/to/uuid
//
// A candidate is a commit whose message says fix or bug and that changes,
// in one package, both code and tests. Its revealing tests are the leaf
// tests that pass on the fix and fail when the package's changed code
// files are put back as they were before it. The fault is coupled when
// some mutant of the fixed package is killed by a revealing test and by no
// other test (adequacy.Matrix.Coupled). Each package of each candidate
// becomes one NDJSON row, with a status saying why it was dropped if it was.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/ibrhmkoz/go-kit/mutant/adequacy"
	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
)

var (
	only    []string // commit hash prefixes to measure; all when empty
	details *json.Encoder
)

func main() {
	name := flag.String("name", "", "repository name for the rows")
	out := flag.String("o", "faults.ndjson", "file to write the rows to")
	jobs := flag.Int("j", runtime.NumCPU(), "mutants to run at once")
	commits := flag.String("commits", "", "if set, measure only these commits (comma-separated hashes or prefixes)")
	detailPath := flag.String("detail", "", "if set, a file to append one JSON row per mutant of each measured fault to, with its old tests' reach and infection")
	flag.Parse()
	for c := range strings.SplitSeq(*commits, ",") {
		if c != "" { //mut:ignore flag wiring
			only = append(only, c) //mut:ignore flag wiring
		}
	}
	if *detailPath != "" { //mut:ignore flag wiring
		f, err := os.OpenFile(*detailPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil { //mut:ignore flag wiring
			fmt.Fprintln(os.Stderr, "faults:", err)
			os.Exit(1) //mut:ignore flag wiring
		}
		defer func() { _ = f.Close() }() //mut:ignore flag wiring
		details = json.NewEncoder(f)     //mut:ignore flag wiring
	}
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: faults -name NAME -o FILE REPO")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	repo, err := filepath.Abs(flag.Arg(0))
	if err == nil {
		err = run(ctx, repo, *name, *out, *jobs)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "faults:", err)
		os.Exit(1)
	}
}

// fault is one package of one candidate fix.
type fault struct {
	Repo    string `json:"repo"`
	Commit  string `json:"commit"`
	Subject string `json:"subject"`
	Pkg     string `json:"pkg"`
	// Status is "ok" or why the row was dropped.
	Status    string   `json:"status"`
	Revealing []string `json:"revealing"`
	Mutants   int      `json:"mutants"`
	Killable  int      `json:"killable"`
	// Coupled counts mutants killed by a revealing test and no other;
	// OnFix and CoupledOnFix count those on lines the fix added or changed.
	Coupled      int            `json:"coupled"`
	OnFix        int            `json:"on_fix"`
	CoupledOnFix int            `json:"coupled_on_fix"`
	Operators    map[string]int `json:"coupled_operators"`
	// OnFixOperators counts, by operator, the coupled mutants on the
	// fix's lines: which operators stand for the fault where it was.
	OnFixOperators map[string]int `json:"coupled_on_fix_operators"`
	// Chance and ChanceOnFix control for the size of the revealing test
	// set: the share of 200 random sets of as many other tests that would
	// make the fault look coupled. -1 when too few other tests exist.
	Chance      float64 `json:"chance"`
	ChanceOnFix float64 `json:"chance_on_fix"`
}

var fixMessage = `(^|[^a-z])(fix|fixes|fixed|bug|bugs)([^a-z]|$)`

// wanted reports whether commit is one of prefixes, or prefixes is empty.
func wanted(commit string, prefixes []string) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if strings.HasPrefix(commit, p) {
			return true
		}
	}
	return false
}

func run(ctx context.Context, repo, name, out string, jobs int) error {
	modfile, err := git(ctx, repo, "show", "HEAD:go.mod")
	if err != nil {
		return err
	}
	module := regexp.MustCompile(`(?m)^module\s+(\S+)`).FindSubmatch(modfile)
	if module == nil {
		return errors.New("no module line in go.mod at HEAD")
	}
	commits, err := git(ctx, repo, "log", "--no-merges", "--format=%H", "-i", "-E", "--grep="+fixMessage, "HEAD")
	if err != nil {
		return err
	}
	var rows []fault
	for c := range strings.FieldsSeq(string(commits)) {
		if !wanted(c, only) { //mut:ignore the driver runs only on the corpus; wanted is tested
			continue
		}
		fs, err := candidate(ctx, repo, name, c, string(module[1]), jobs)
		if err != nil {
			return fmt.Errorf("%s: %w", c[:12], err)
		}
		for _, f := range fs {
			fmt.Fprintf(os.Stderr, "%s %s %s: %s, %d coupled\n", name, c[:12], f.Pkg, f.Status, f.Coupled)
		}
		rows = append(rows, fs...)
	}
	return write(out, rows)
}

// change is a file a commit changed, with git's status letter.
type change struct{ status, path string }

func candidate(ctx context.Context, repo, name, commit, module string, jobs int) ([]fault, error) {
	meta, err := git(ctx, repo, "show", "-s", "--format=%P%x09%s", commit)
	if err != nil {
		return nil, err
	}
	parent, subject, _ := strings.Cut(strings.TrimSpace(string(meta)), "\t")
	diff, err := git(ctx, repo, "diff-tree", "-r", "--no-commit-id", "--name-status", "--no-renames", parent, commit)
	if err != nil {
		return nil, err
	}
	byDir := map[string][]change{}
	tests := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(diff)), "\n") {
		status, p, ok := strings.Cut(line, "\t")
		if !ok || !strings.HasSuffix(p, ".go") || strings.Contains(p, "testdata/") {
			continue
		}
		dir := path.Dir(p)
		if strings.HasSuffix(p, "_test.go") {
			tests[dir] = true
			continue
		}
		byDir[dir] = append(byDir[dir], change{status, p})
	}
	var dirs []string
	for dir := range byDir {
		if tests[dir] {
			dirs = append(dirs, dir)
		}
	}
	if len(dirs) == 0 {
		return nil, nil
	}
	slices.Sort(dirs)

	tree, err := os.MkdirTemp("", "faults")
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = git(context.Background(), repo, "worktree", "remove", "--force", tree)
		_ = os.RemoveAll(tree)
	}()
	if _, err := git(ctx, repo, "worktree", "add", "--detach", "--force", tree, commit); err != nil {
		return nil, err
	}
	// Commits from before go.mod: the module had no dependencies then
	// either, and imported itself without a major version suffix.
	if _, err := os.Stat(filepath.Join(tree, "go.mod")); errors.Is(err, os.ErrNotExist) {
		mod := regexp.MustCompile(`/v\d+$`).ReplaceAllString(module, "")
		if err := os.WriteFile(filepath.Join(tree, "go.mod"), []byte("module "+mod+"\n\ngo 1.16\n"), 0o600); err != nil {
			return nil, err
		}
	}

	var rows []fault
	for _, dir := range dirs {
		f := fault{Repo: name, Commit: commit, Subject: subject, Pkg: dir}
		f.Status, err = measure(ctx, repo, tree, parent, commit, dir, byDir[dir], jobs, &f)
		if err != nil {
			return nil, err
		}
		rows = append(rows, f)
	}
	return rows, nil
}

// measure fills f for one package of a fix and returns its status. Errors
// are for the tool itself; a commit that does not build is a status.
func measure(ctx context.Context, repo, tree, parent, commit, dir string, changes []change, jobs int, f *fault) (string, error) {
	fixed := goTest(ctx, tree, dir, "")
	if !fixed.built {
		return "fixed code does not build", nil
	}
	overlay, err := buggyOverlay(ctx, repo, tree, parent, changes)
	if err != nil {
		return "", err
	}
	buggy := goTest(ctx, tree, dir, overlay)
	if !buggy.built {
		return "buggy code does not build with the fix's tests", nil
	}
	f.Revealing = revealing(fixed, buggy)
	slices.Sort(f.Revealing)
	if len(f.Revealing) == 0 {
		return "no test reveals the fault", nil
	}

	r := killmatrix.Runner{Workers: jobs, Schemata: true, Select: true}
	m, err := r.Run(ctx, tree, "./"+dir)
	if err != nil {
		return "kill matrix: " + firstLine(err.Error()), nil
	}
	index := map[int]int{}     // test id → index among the matrix's ok tests
	revealed := adequacy.Set{} // revealing tests, by index
	for _, t := range m.Tests {
		if t.State != killmatrix.OK {
			continue
		}
		index[t.ID] = len(index)
		if slices.Contains(f.Revealing, t.Name) {
			revealed = revealed.With(index[t.ID])
		}
	}
	if revealed.Empty() {
		return "revealing tests are flaky in the kill matrix", nil
	}
	var compiled []killmatrix.Mutant
	pos := map[int]int{} // mutant id → index among compiled mutants
	for _, mu := range m.Mutants {
		if mu.Compiled {
			pos[mu.ID] = len(compiled)
			compiled = append(compiled, mu)
		}
	}
	kills := make([]adequacy.Set, len(compiled))
	for _, res := range m.Results {
		if i, ok := pos[res.MutantID]; ok && res.Status == killmatrix.Killed {
			kills[i] = kills[i].With(index[res.TestID])
		}
	}
	matrix := adequacy.New(len(index), kills)

	onFix := map[string]map[int]bool{}
	for _, c := range changes {
		if c.status == "D" {
			continue
		}
		d, err := git(ctx, repo, "diff", "-U0", parent, commit, "--", c.path)
		if err != nil {
			return "", err
		}
		onFix[path.Base(c.path)] = changedLines(d)
	}
	f.Mutants = len(compiled)
	for _, k := range kills {
		if !k.Empty() {
			f.Killable++
		}
	}
	for _, mu := range compiled {
		if onFix[mu.File][mu.Line] {
			f.OnFix++
		}
	}
	f.Chance, f.ChanceOnFix = chance(matrix, revealed, index, func(i int) bool { return onFix[compiled[i].File][compiled[i].Line] })
	f.Operators, f.OnFixOperators = map[string]int{}, map[string]int{}
	coupled := matrix.Coupled(revealed)
	if details != nil { //mut:ignore the driver runs only on the corpus; infections and countOf are tested
		counts, err := infections(ctx, filepath.Join(tree, dir), f.Revealing)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s %s: infections: %v\n", f.Repo, commit[:12], err)
		}
		for i, mu := range compiled {
			c := countOf(counts, mu)
			_ = details.Encode(detail{Repo: f.Repo, Commit: commit, Pkg: f.Pkg, File: mu.File, Line: mu.Line, Col: mu.Col, Operator: string(mu.Operator), Original: mu.Original, Mutated: mu.Mutated, Equivalent: mu.Equivalent, Arid: mu.Arid, Killable: !kills[i].Empty(), Coupled: slices.Contains(coupled, i), OnFix: onFix[mu.File][mu.Line], Reached: c[0], Infected: c[1]}) //mut:ignore the driver runs only on the corpus
		}
	}
	for _, i := range coupled {
		mu := compiled[i]
		f.Coupled++
		f.Operators[string(mu.Operator)]++
		if onFix[mu.File][mu.Line] {
			f.CoupledOnFix++
			f.OnFixOperators[string(mu.Operator)]++
		}
	}
	return "ok", nil
}

// chance draws 200 sets of tests, as many as revealed, from the tests not
// in it, and returns the share that would leave some mutant coupled, and
// some mutant on the fix's lines coupled.
func chance(m *adequacy.Matrix, revealed adequacy.Set, index map[int]int, onFix func(int) bool) (share, shareOnFix float64) {
	var pool []int
	for _, i := range index {
		if !revealed.Intersects(adequacy.NewSet(i)) {
			pool = append(pool, i)
		}
	}
	slices.Sort(pool)
	n := revealed.Len()
	if len(pool) < n {
		return -1, -1
	}
	r := rand.New(rand.NewPCG(1, uint64(len(pool))))
	const draws = 200
	hits, hitsOnFix := 0, 0
	for range draws {
		var set adequacy.Set
		for _, j := range r.Perm(len(pool))[:n] {
			set = set.With(pool[j])
		}
		coupled := m.Coupled(set)
		if len(coupled) > 0 {
			hits++
		}
		if slices.ContainsFunc(coupled, onFix) {
			hitsOnFix++
		}
	}
	return float64(hits) / draws, float64(hitsOnFix) / draws
}

// buggyOverlay writes an overlay that puts the package's changed code
// files back as they were in parent: a file the fix added is removed, one
// it deleted comes back.
func buggyOverlay(ctx context.Context, repo, tree, parent string, changes []change) (string, error) {
	work, err := os.MkdirTemp("", "faults-overlay")
	if err != nil {
		return "", err
	}
	replace := map[string]string{}
	for i, c := range changes {
		target := filepath.Join(tree, c.path)
		if c.status == "A" {
			replace[target] = ""
			continue
		}
		src, err := git(ctx, repo, "show", parent+":"+c.path)
		if err != nil {
			return "", err
		}
		file := filepath.Join(work, fmt.Sprintf("%d.go", i))
		if err := os.WriteFile(file, src, 0o600); err != nil {
			return "", err
		}
		replace[target] = file
	}
	data, err := json.Marshal(map[string]map[string]string{"Replace": replace})
	if err != nil {
		return "", err
	}
	overlay := filepath.Join(work, "overlay.json")
	return overlay, os.WriteFile(overlay, data, 0o600)
}

func goTest(ctx context.Context, tree, dir, overlay string) testRun {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	args := []string{"test", "-json", "-count=1", "-vet=off", "-timeout=2m"}
	if overlay != "" {
		args = append(args, "-overlay", overlay)
	}
	cmd := exec.CommandContext(ctx, "go", append(args, "./"+dir)...)
	cmd.Dir = tree
	out, _ := cmd.Output() // a failing test exits non-zero too
	return parseTestJSON(out)
}

func git(ctx context.Context, repo string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.Output()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, exit.Stderr)
	}
	return out, err
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func write(name string, rows []fault) error {
	f, err := os.Create(name)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
}
