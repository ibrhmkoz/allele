package main

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/ibrhmkoz/allele"
	"github.com/ibrhmkoz/allele/killmatrix"
)

// options picks the mutants of a run.
type options struct {
	// all runs every mutant on a line rather than one.
	all bool
	// arid runs mutants in code no one writes a test for: logging,
	// metrics, capacity hints, lines marked allele:ignore.
	arid bool
	// diff, if set, maps absolute paths to the lines a diff touched; only
	// mutants on those lines run.
	diff map[string]map[int]bool
	// noCache disables the per-package verdict cache.
	noCache bool
}

// choose returns, in order, the indices of the mutants of the file at path
// that may run: equivalent mutants never do, nor arid ones without o.arid,
// nor, with a diff, those off its lines. Without o.all, pick then keeps
// one per line.
func choose(o options, path string, ms []allele.Mutant) []int {
	var lines map[int]bool
	if o.diff != nil {
		var ok bool
		if lines, ok = o.diff[path]; !ok {
			return nil
		}
	}
	var keep []int
	for i, m := range ms {
		if m.Equivalent != "" || m.Arid != "" && !o.arid || lines != nil && !lines[m.Pos.Line] {
			continue
		}
		keep = append(keep, i)
	}
	return keep
}

// pick keeps one of a line's mutants, by what the baseline saw of them
// (measurements §21, rule R2b). Real faults hide where no test looks:
//  1. a mutant whose site no test reaches, while others on the line are
//     (the right side of a short-circuit);
//  2. an if's condition forced true or false that no test infects: a
//     branch no test takes one way, unless it checks nil, which mostly
//     guards defensive paths;
//  3. the one the fewest tests infect, so a weak test's mutant shows.
//
// On 79 real faults it reports a mutant coupled to the fault on 80% of
// the lines holding one, against a hash pick's 73%, with 2% fewer
// survivors; simulated on the corpus it raises D from 0.887 to 0.920. Without the baseline's counts, and
// to break ties, a hash of the file and line picks, so reruns pick the
// same.
func pick(cs []killmatrix.Candidate) int {
	h := fnv.New32a()
	_, _ = fmt.Fprintf(h, "%s:%d", filepath.Base(cs[0].Path), cs[0].Line) // a hash never fails to write
	hashed := int(h.Sum32() % uint32(len(cs)))
	if slices.ContainsFunc(cs, func(c killmatrix.Candidate) bool { return c.Reached < 0 }) {
		return hashed
	}
	if i := slices.IndexFunc(cs, func(c killmatrix.Candidate) bool { return c.Reached == 0 }); i >= 0 {
		return i
	}
	best := -1
	for i, c := range cs {
		if c.Infected == 0 && untakenBranch(c.Mutant) && (best < 0 || c.Reached < cs[best].Reached) {
			best = i
		}
	}
	if best >= 0 {
		return best
	}
	best = hashed
	for i, c := range cs {
		if c.Infected > 0 && (cs[best].Infected == 0 || c.Infected < cs[best].Infected) {
			best = i
		}
	}
	return best
}

// untakenBranch reports whether mu forces a condition true or false,
// other than a nil check.
func untakenBranch(mu killmatrix.Mutant) bool {
	forced := mu.Mutated == "true" || mu.Mutated == "false" || strings.HasSuffix(mu.Mutated, "return true }()") || strings.HasSuffix(mu.Mutated, "return false }()")
	return mu.Operator == allele.COR && forced && !strings.Contains(mu.Original, "nil")
}

var hunk = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// parseDiff reads git diff -U0 output into the lines it adds or changes,
// by absolute path under root. A hunk that only deletes marks the lines
// around the gap.
func parseDiff(out []byte, root string) map[string]map[int]bool {
	files := map[string]map[int]bool{}
	var cur map[int]bool
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "+++ ") {
			cur = nil // +++ /dev/null: the file was deleted
			if name, ok := strings.CutPrefix(line, "+++ b/"); ok {
				cur = map[int]bool{}
				files[filepath.Join(root, name)] = cur
			}
			continue
		}
		m := hunk.FindStringSubmatch(line)
		if m == nil || cur == nil {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		n := 1
		if m[2] != "" {
			n, _ = strconv.Atoi(m[2])
		}
		if n == 0 {
			cur[start], cur[start+1] = true, true
		}
		for l := start; l < start+n; l++ {
			cur[l] = true
		}
	}
	return files
}

// report lists the mutants no test killed, each as the line before and
// after, then counts every verdict. Paths are shown relative to root.
func report(m *killmatrix.Matrix, root string, read func(path string) ([]byte, error)) string {
	verdicts := m.Verdicts()
	rank := map[killmatrix.Verdict]int{killmatrix.VerdictSurvived: 0, killmatrix.VerdictNotCovered: 1, killmatrix.VerdictTimeout: 2}
	var shown []killmatrix.Mutant
	count := map[killmatrix.Verdict]int{}
	for _, mu := range m.Mutants {
		v := verdicts[mu.ID]
		count[v]++
		if _, ok := rank[v]; ok {
			shown = append(shown, mu)
		}
	}
	slices.SortStableFunc(shown, func(a, b killmatrix.Mutant) int {
		return cmp.Or(rank[verdicts[a.ID]]-rank[verdicts[b.ID]], cmp.Compare(a.Path, b.Path), a.Line-b.Line)
	})

	sources := map[string][]string{}
	var b strings.Builder
	for _, mu := range shown {
		lines, ok := sources[mu.Path]
		if !ok {
			src, err := read(mu.Path)
			if err == nil {
				lines = strings.Split(string(src), "\n")
			}
			sources[mu.Path] = lines
		}
		rel, err := filepath.Rel(root, mu.Path)
		if err != nil {
			rel = mu.Path
		}
		v := verdicts[mu.ID]
		fmt.Fprintf(&b, "%s  %s:%d  %s", v, rel, mu.Line, mu.Operator)
		if v == killmatrix.VerdictNotCovered {
			b.WriteString("  (no test runs this function)")
		}
		before, after := lineChange(lines, mu)
		fmt.Fprintf(&b, "\n  -%s\n  +%s\n\n", before, after)
	}
	fmt.Fprintf(&b, "%d mutants: %d killed, %d survived, %d not covered, %d timed out, %d did not build\n",
		len(m.Mutants), count[killmatrix.VerdictKilled], count[killmatrix.VerdictSurvived], count[killmatrix.VerdictNotCovered],
		count[killmatrix.VerdictTimeout], count[killmatrix.VerdictNoBuild])
	return b.String()
}

// jsonMutant is one line of -json output.
type jsonMutant struct {
	File     string             `json:"file"`
	Line     int                `json:"line"`
	Col      int                `json:"col"`
	Operator allele.Operator    `json:"operator"`
	Original string             `json:"original"`
	Mutated  string             `json:"mutated"`
	Verdict  killmatrix.Verdict `json:"verdict"`
}

// reportJSON writes one JSON object per mutant, every verdict included, for
// tools to filter. Paths are relative to root.
func reportJSON(m *killmatrix.Matrix, root string) string {
	verdicts := m.Verdicts()
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // keep < and > readable in operators
	for _, mu := range m.Mutants {
		rel, err := filepath.Rel(root, mu.Path)
		if err != nil {
			rel = mu.Path
		}
		_ = enc.Encode(jsonMutant{rel, mu.Line, mu.Col, mu.Operator, mu.Original, mu.Mutated, verdicts[mu.ID]}) // a Builder never fails to write
	}
	return b.String()
}

// lineChange returns the mutant's line before and after it. When the
// change spans lines or cannot be placed, it falls back to the expression
// before and after.
func lineChange(lines []string, mu killmatrix.Mutant) (before, after string) {
	mutated := mu.Mutated
	if mutated == "" {
		mutated = "(deleted)"
	}
	if mu.Line < 1 || mu.Line > len(lines) || strings.Contains(mu.Original, "\n") {
		return mu.Original, mutated
	}
	line, col := lines[mu.Line-1], mu.Col-1
	for at := 0; ; at++ {
		i := strings.Index(line[at:], mu.Original)
		if i < 0 {
			return mu.Original, mutated
		}
		i += at
		if i <= col && col <= i+len(mu.Original) {
			return line, line[:i] + mutated + line[i+len(mu.Original):]
		}
		at = i
	}
}
