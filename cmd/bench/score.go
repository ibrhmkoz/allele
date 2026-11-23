package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"path"
	"slices"
	"strings"

	"github.com/ibrhmkoz/go-kit/mutant/adequacy"
	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
)

// The metric's frozen constants. T0 is the naive exhaustive runner's time
// on the corpus at -j 8 (§9), the anchor that scores M = 1; Floor is the go
// command's own overhead, so that no speedup is worth more than reaching
// it; Gamma prices speed against quality: twice as fast makes up for at
// most 1 - 2^-0.18 = 11.7% less of it.
const (
	T0    = 876.0
	Floor = 2.0
	Gamma = 0.18
)

// Weights of verdict errors, by what each costs a developer: a hidden gap
// is a test never written, a false gap an afternoon spent on a mutant some
// test already kills, a kill read as a timeout only a noisy report.
const (
	wHidden  = 5
	wFalse   = 2
	wTimeout = 0.5
	wBuild   = 2
	// maxHidden is the share of stable mutants a build may call killed
	// though no test kills them before M is 0: no speed buys back gaps.
	maxHidden = 0.01
)

// verdict is a mutant's verdict as the tool reports it, with the mutant.
type verdict struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Operator string `json:"operator"`
	Original string `json:"original"`
	Mutated  string `json:"mutated"`
	Verdict  string `json:"verdict"`
}

// key names a mutant across runs: its package's directory in the module,
// "/" at the root, its file, position and change.
func key(dir, file string, line, col int, op, original, mutated string) string {
	return fmt.Sprintf("%s %s:%d:%d %s %q→%q", path.Join("/", dir), file, line, col, op, original, mutated)
}

// reference is one repository's exhaustive kill matrix, read as the ground
// truth a run is scored against.
type reference struct {
	keys    map[string]int // key → index into compiled
	stable  map[int]bool   // compiled index → two exhaustive runs agree
	verdict []killmatrix.Verdict
	matrix  *adequacy.Matrix
}

// newReference reads m, the matrix of the module at path, and its rerun,
// if any.
func newReference(module string, m, rerun *killmatrix.Matrix) *reference {
	ref := &reference{keys: map[string]int{}, stable: map[int]bool{}}
	refKey := func(mu killmatrix.Mutant) string {
		return key(strings.TrimPrefix(mu.Pkg, module), mu.File, mu.Line, mu.Col, string(mu.Operator), mu.Original, mu.Mutated)
	}
	vs := m.Verdicts()
	again := map[string]killmatrix.Verdict{}
	if rerun != nil {
		rv := rerun.Verdicts()
		for _, mu := range rerun.Mutants {
			again[refKey(mu)] = rv[mu.ID]
		}
	}
	// Tests out of the matrix have no results, so their columns stay empty.
	testIndex := map[int]int{}
	for i, t := range m.Tests {
		testIndex[t.ID] = i
	}
	index := map[int]int{}
	for _, mu := range m.Mutants {
		if !mu.Compiled {
			continue
		}
		i := len(ref.verdict)
		index[mu.ID] = i
		k := refKey(mu)
		if _, dup := ref.keys[k]; dup {
			ref.keys[k] = -1 // ambiguous: score neither
		} else {
			ref.keys[k] = i
		}
		ref.verdict = append(ref.verdict, vs[mu.ID])
		v, ok := again[k]
		ref.stable[i] = rerun == nil || ok && class(v) == class(vs[mu.ID])
	}
	kills := make([]adequacy.Set, len(ref.verdict))
	// A matrix holds results only for built mutants and tests in it.
	for _, r := range m.Results {
		if r.Status == killmatrix.Killed {
			kills[index[r.MutantID]] = kills[index[r.MutantID]].With(testIndex[r.TestID])
		}
	}
	ref.matrix = adequacy.New(len(testIndex), kills)
	return ref
}

// toolKey is a reported mutant's key; mutant -json gives files relative
// to the module root.
func toolKey(v verdict) string {
	return key(path.Dir(v.File), path.Base(v.File), v.Line, v.Col, v.Operator, v.Original, v.Mutated)
}

// kind sorts verdicts by what they tell a developer.
type kind int

const (
	unknown kind = iota
	noBuild
	dead // killed
	hung // timed out: no gap, but no kill either
	gap  // survived or not covered: write a test
)

func class(v killmatrix.Verdict) kind {
	return map[killmatrix.Verdict]kind{
		killmatrix.VerdictSurvived:   gap,
		killmatrix.VerdictNotCovered: gap,
		killmatrix.VerdictKilled:     dead,
		killmatrix.VerdictTimeout:    hung,
		killmatrix.VerdictNoBuild:    noBuild,
	}[v]
}

// score is how one run did on one repository.
type score struct {
	Repo     string  `json:"repo"`
	Reported int     `json:"reported"`
	Unknown  int     `json:"unknown"` // reported mutants the reference lacks
	D        float64 `json:"d"`
	Stable   int     `json:"stable"`
	Hidden   int     `json:"hidden"`
	False    int     `json:"false"`
	Timeout  int     `json:"timeout"`
	Build    int     `json:"build"`
}

// counter is the error count a verdict of got adds to, where the
// reference says want, or nil if got is right. The reference holds built
// mutants only. A verdict the tool does not define hides a gap.
func (s *score) counter(want, got kind) *int {
	return map[[2]kind]*int{
		{gap, unknown}: &s.Hidden, {gap, noBuild}: &s.Build, {gap, dead}: &s.Hidden, {gap, hung}: &s.Hidden,
		{dead, unknown}: &s.Hidden, {dead, noBuild}: &s.Build, {dead, hung}: &s.Timeout, {dead, gap}: &s.False,
		{hung, unknown}: &s.Hidden, {hung, noBuild}: &s.Build, {hung, gap}: &s.False,
	}[[2]kind{want, got}]
}

// worse reports whether s did worse than t: more weighted errors, or as
// many and a lower D.
func (s score) worse(t score) bool {
	return s.errors() > t.errors() || s.errors() == t.errors() && s.D < t.D
}

// errors is the weighted count of verdict errors.
func (s score) errors() float64 {
	return wHidden*float64(s.Hidden) + wFalse*float64(s.False) + wTimeout*float64(s.Timeout) + wBuild*float64(s.Build)
}

// grade scores a run's verdicts against ref. D is the dominator score of
// tests written until every reported mutant is dead, averaged over seeds
// (Kurtz, Ammann et al., FSE 2016); the error counts compare verdicts
// where two exhaustive runs agreed. A mutant reported twice counts once,
// and a verdict the tool does not define counts as a hidden gap.
func (ref *reference) grade(repo string, vs []verdict, seeds int) score {
	s := score{Repo: repo, Reported: len(vs)}
	var picks []int
	seen := map[int]bool{}
	for _, v := range vs {
		i, ok := ref.keys[toolKey(v)]
		if !ok || i < 0 || seen[i] {
			s.Unknown++
			continue
		}
		seen[i] = true
		picks = append(picks, i)
		if !ref.stable[i] {
			continue
		}
		s.Stable++
		if c := s.counter(class(ref.verdict[i]), class(killmatrix.Verdict(v.Verdict))); c != nil {
			*c++
		}
	}
	slices.Sort(picks)
	for seed := range uint64(seeds) {
		s.D += ref.matrix.Simulate(picks, 200, rand.New(rand.NewPCG(seed+1, 0))).Score
	}
	s.D /= float64(seeds)
	return s
}

// metric is one tool's value of M over the corpus.
type metric struct {
	Bin string  `json:"bin"`
	D   float64 `json:"d"`
	F   float64 `json:"f"`
	T   float64 `json:"t"`
	M   float64 `json:"m"`
}

// combine is M = D·F·S(T): D the geometric mean of the repositories' D, so
// that giving up on one repository gives up everything; F one minus the
// weighted verdict errors per stable mutant; T the summed seconds. A build
// that hides more than 1% of the gaps scores 0, however fast it is.
func combine(bin string, scores []score, seconds float64) metric {
	logD, errs, stable, hidden := 0.0, 0.0, 0, 0
	for _, s := range scores {
		logD += math.Log(s.D)
		errs += s.errors()
		stable += s.Stable
		hidden += s.Hidden
	}
	m := metric{Bin: bin, T: seconds}
	if len(scores) > 0 {
		m.D = math.Exp(logD / float64(len(scores)))
	}
	if stable > 0 && float64(hidden) <= maxHidden*float64(stable) {
		m.F = max(0, 1-errs/float64(stable))
	}
	m.M = m.D * m.F * math.Pow((T0+Floor)/(seconds+Floor), Gamma)
	return m
}
