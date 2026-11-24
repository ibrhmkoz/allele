// Command simulate measures mutant selection strategies on kill matrices
// computed by killmatrix, without building anything:
//
//	simulate -o docs/data docs/data/*/
//
// For each repository it writes a row to summary.ndjson (raw and dominator
// counts) and, for each strategy, a row to strategies.ndjson: the share of
// the cost it keeps and the dominator score of tests written against it.
// Every strategy that drops mutants is paired with a control that picks as
// many mutants at random, since selective mutation is only worth its
// complexity if it beats that.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ibrhmkoz/allele"
	"github.com/ibrhmkoz/allele/adequacy"
	"github.com/ibrhmkoz/allele/killmatrix"
)

func main() {
	out := flag.String("o", ".", "directory to write summary.ndjson and strategies.ndjson to")
	seeds := flag.Int("seeds", 30, "independent simulations per strategy, seeded 1 to n, to show the simulation's own noise")
	flag.Parse()
	if err := run(*out, *seeds, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "simulate:", err)
		os.Exit(1)
	}
}

type summary struct {
	Repo       string `json:"repo"`
	Mutants    int    `json:"mutants"`
	Compiled   int    `json:"compiled"`
	Killable   int    `json:"killable"`
	Dominators int    `json:"dominators"`
	// DominatorsUnrepaired counts dominators when repaired mutants are
	// dropped instead, as a tool that skips them would.
	DominatorsUnrepaired int     `json:"dominators_unrepaired"`
	Repaired             int     `json:"repaired"`
	Equivalent           int     `json:"equivalent"`
	Tests                int     `json:"tests"`
	RawScore             float64 `json:"raw_score"`
}

type outcome struct {
	Repo     string  `json:"repo"`
	Seed     uint64  `json:"seed"`
	Strategy string  `json:"strategy"`
	Control  bool    `json:"control"`
	Mutants  float64 `json:"mutants"`
	Total    int     `json:"total"`
	Cost     float64 `json:"cost"`
	Tests    float64 `json:"tests"`
	Score    float64 `json:"score"`
	Random   float64 `json:"random"`
}

func run(out string, seeds int, dirs []string) error {
	var sums []summary
	var outs []outcome
	for _, dir := range dirs {
		repo, err := load(dir)
		if err != nil {
			return fmt.Errorf("%s: %w", dir, err)
		}
		sums = append(sums, repo.summary())
		for seed := range uint64(seeds) {
			outs = append(outs, repo.simulate(seed+1)...)
		}
	}
	if err := write(filepath.Join(out, "summary.ndjson"), sums); err != nil {
		return err
	}
	return write(filepath.Join(out, "strategies.ndjson"), outs)
}

// repo is one repository's kill matrix with the mutants that did not
// compile left out: a mutation tool would report nothing for them.
type repo struct {
	name     string
	mutants  []killmatrix.Mutant // compiled only; index i is mutant i of matrix
	all      int
	cost     []float64 // build plus test time of mutant i
	killable int
	kills    []adequacy.Set
	matrix   *adequacy.Matrix
}

func load(dir string) (*repo, error) {
	m, err := killmatrix.Read(dir)
	if err != nil {
		return nil, err
	}
	ms, ts, rs := m.Mutants, m.Tests, m.Results

	testIndex := map[int]int{}
	top := map[int]string{}
	for _, t := range ts {
		top[t.ID], _, _ = strings.Cut(t.Name, "/")
		if t.State == killmatrix.OK {
			testIndex[t.ID] = len(testIndex)
		}
	}
	rp := &repo{name: filepath.Base(filepath.Clean(dir)), all: len(ms)}
	mutantIndex := map[int]int{}
	for _, m := range ms {
		if m.Compiled {
			mutantIndex[m.ID] = len(rp.mutants)
			rp.mutants = append(rp.mutants, m)
			rp.cost = append(rp.cost, m.BuildMS)
		}
	}
	kills := make([]adequacy.Set, len(rp.mutants))
	// Leaves of one top-level test share its process and its time.
	timed := map[[2]string]bool{}
	for _, r := range rs {
		i, ok := mutantIndex[r.MutantID]
		if !ok {
			continue
		}
		if run := [2]string{fmt.Sprint(r.MutantID), top[r.TestID]}; !timed[run] {
			timed[run] = true
			rp.cost[i] += r.MS
		}
		if t, ok := testIndex[r.TestID]; ok && r.Status == killmatrix.Killed {
			kills[i] = kills[i].With(t)
		}
	}
	for _, k := range kills {
		if !k.Empty() {
			rp.killable++
		}
	}
	rp.kills = kills
	rp.matrix = adequacy.New(len(testIndex), kills)
	return rp, nil
}

func (rp *repo) summary() summary {
	repaired, equivalent := 0, 0
	var kept []adequacy.Set
	for i, m := range rp.mutants {
		if m.Equivalent != "" {
			equivalent++
		}
		if m.Repaired {
			repaired++
			continue
		}
		kept = append(kept, rp.kills[i])
	}
	return summary{
		DominatorsUnrepaired: len(adequacy.New(rp.matrix.Tests(), kept).Dominators()),
		Repaired:             repaired,
		Equivalent:           equivalent,
		Repo:                 rp.name,
		Mutants:              rp.all,
		Compiled:             len(rp.mutants),
		Killable:             rp.killable,
		Dominators:           len(rp.matrix.Dominators()),
		Tests:                rp.matrix.Tests(),
		RawScore:             float64(rp.killable) / float64(len(rp.mutants)),
	}
}

// A strategy picks the mutants a cheaper tool would run.
type strategy struct {
	name string
	pick func(r *rand.Rand) []int
	// random strategies draw a new pick each time; fixed ones draw once.
	random bool
}

func (rp *repo) strategies() []strategy {
	ops := []allele.Operator{allele.ROR, allele.COR, allele.AOR, allele.SDL, allele.ERR}
	where := func(keep func(killmatrix.Mutant) bool) func(*rand.Rand) []int {
		return func(*rand.Rand) []int {
			var picked []int
			for i, m := range rp.mutants {
				if keep(m) {
					picked = append(picked, i)
				}
			}
			return picked
		}
	}
	ss := []strategy{{name: "all", pick: where(func(killmatrix.Mutant) bool { return true })}}
	for _, op := range ops {
		ss = append(ss, strategy{name: string(op) + " only", pick: where(func(m killmatrix.Mutant) bool { return m.Operator == op })})
	}
	for _, op := range ops {
		ss = append(ss, strategy{name: "all but " + string(op), pick: where(func(m killmatrix.Mutant) bool { return m.Operator != op })})
	}
	ss = append(ss,
		strategy{name: "all but repaired", pick: where(func(m killmatrix.Mutant) bool { return !m.Repaired })},
		strategy{name: "all but equivalent", pick: where(func(m killmatrix.Mutant) bool { return m.Equivalent == "" })},
	)
	return append(ss, strategy{name: "one per line", pick: rp.onePerLine, random: true})
}

// onePerLine keeps one random mutant of each line, as Google's tool does.
func (rp *repo) onePerLine(r *rand.Rand) []int {
	lines := map[string][]int{}
	var keys []string
	for i, m := range rp.mutants {
		k := fmt.Sprintf("%s/%s:%d", m.Pkg, m.File, m.Line)
		if _, ok := lines[k]; !ok {
			keys = append(keys, k)
		}
		lines[k] = append(lines[k], i)
	}
	picked := make([]int, 0, len(keys))
	for _, k := range keys {
		picked = append(picked, lines[k][r.IntN(len(lines[k]))])
	}
	slices.Sort(picked)
	return picked
}

const (
	draws = 40  // picks of a random strategy
	runs  = 200 // adequate test sets per pick, split over the draws
)

func (rp *repo) simulate(seed uint64) []outcome {
	r := rand.New(rand.NewPCG(seed, 0))
	var outs []outcome
	for _, s := range rp.strategies() {
		n := 1
		if s.random {
			n = draws
		}
		picks := make([][]int, n)
		for i := range picks {
			picks[i] = s.pick(r)
		}
		outs = append(outs, rp.measure(seed, s.name, false, picks, r))
		if s.name == "all" || len(picks[0]) == 0 {
			continue
		}
		controls := make([][]int, draws)
		for i := range controls {
			controls[i] = r.Perm(len(rp.mutants))[:len(picks[i%n])]
			slices.Sort(controls[i])
		}
		outs = append(outs, rp.measure(seed, s.name, true, controls, r))
	}
	return outs
}

func (rp *repo) measure(seed uint64, name string, control bool, picks [][]int, r *rand.Rand) outcome {
	total := 0.0
	for _, c := range rp.cost {
		total += c
	}
	o := outcome{Repo: rp.name, Seed: seed, Strategy: name, Control: control, Total: len(rp.mutants)}
	for _, p := range picks {
		res := rp.matrix.Simulate(p, runs/len(picks), r)
		o.Mutants += float64(len(p))
		o.Tests += res.Tests
		o.Score += res.Score
		o.Random += res.Random
		for _, i := range p {
			o.Cost += rp.cost[i] / total
		}
	}
	n := float64(len(picks))
	o.Mutants /= n
	o.Tests /= n
	o.Score /= n
	o.Random /= n
	o.Cost /= n
	return o
}

func write[T any](name string, rows []T) error {
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
