# mutant

Mutation testing for Go. `mutant` makes small, deliberate changes to your
code (`>` to `>=`, `&&` to `||`, a statement deleted, `return err` to
`return nil`) and runs your tests against each. A change no test notices is
a gap in your tests. The output lists those gaps as the line before and
after, not as a score.

```
$ go install github.com/ibrhmkoz/go-kit/mutant/cmd/mutant@latest
$ mutant ./...
survived  xtesting.go:73  SDL
  -	EqualText(t, v.String(), want)
  +	(deleted)

survived  version.go:183  ROR
  -	if len(v) > MaxVersionLen {
  +	if len(v) >= MaxVersionLen {

381 mutants: 295 killed, 86 survived, 0 not covered, 0 timed out, 0 did not build
in 4.2s
```

(Excerpts from two runs: this repository's `xtesting`, before its tests were
fixed, and Masterminds/semver.) The first gap was real: deleting the call
that does `xtesting.Prints`'s work passed every test, because no test
checked that `Prints` fails on a mismatch.

## Usage

| Command | What it runs |
|---|---|
| `mutant ./...` | every package, one mutant per line |
| `mutant -diff main ./...` | only lines changed since `main`, working tree included |
| `mutant -all ./...` | every mutant, not one per line |
| `mutant -arid ./...` | also mutants in logging, metrics and capacity hints |
| `mutant -strict ./...` | exit 1 when a mutant survives or no test reaches it (CI) |
| `mutant -json ./...` | one JSON object per mutant, every verdict included |
| `mutant -j 4 -v ./...` | 4 mutants at once, progress on stderr |

Each mutant gets a verdict: **killed** (a test failed), **survived** (all
tests passed: a gap), **not covered** (no test runs its function),
**timeout** (a test ran 3× its usual time plus a second), or **build
error**.

Mark a line no test should be asked to check with `//mut:ignore`, at its end
or alone on the line above.

## What it does, and why

Every default below was measured before it was chosen; the numbers come from
[`docs/measurements.py`](docs/measurements.py) (`make mutant-measurements`),
on six libraries and three services at pinned commits.

- **Operators:** ROR (relational), COR (`&&`/`||`, an `if` condition to
  `true`/`false`), AOR (arithmetic), SDL (statement deletion), ERR (`err !=
  nil`, `return err`). ROR, COR and statement deletion are the ones most
  tied to real faults in the literature (Just et al., FSE 2014); ERR found
  no fault the others missed but reads well (§10). On the corpus' own history, 74% of real bug fixes are
  coupled to a mutant on the lines they changed, against 2% for random tests
  of the same size (§10).
- **One build per package (schemata):** every mutant is built into one
  binary and switched on at run time by `GOMUTANT`. Builds went from 47
  minutes to 0.9 over the corpus, and the kill matrix stayed the same as a
  build per mutant, within the noise two such builds show against each other
  (§8).
- **Only the tests that reach a mutant:** a baseline run records which
  functions each top-level test reaches; a mutant runs only those tests. It
  skips 54% of runs with the same matrix (§9). Selection is by function, not
  by line: a branch that runs only when two calls share a clock tick ran in
  no baseline yet ran against the mutants.
- **Fail fast:** a mutant stops at its first kill, cheapest tests first.
- **One mutant per line by default:** 0.88 of the score at 36% of the cost,
  and better than as many random mutants (§2–3).
- **Equivalent mutants skipped:** `len(s) == 0` → `len(s) <= 0` and
  `s != ""` → `s > ""` cannot change behavior; no test killed one of the 72
  in the corpus (§5, §11).
- **Arid mutants skipped:** mutants in logging, metrics, tracing, capacity
  hints and `//mut:ignore` lines. Two blind raters called all 21 surviving
  ones unproductive (§13).
- **Kills confirmed:** a test counts as killing a mutant only if it fails
  against it twice and passes on the original code. Timing tests fail under
  load and would otherwise invent kills that hide real gaps (§13).
- **Mutants that would not compile are repaired:** deleting a statement can
  leave a variable or import unused, which Go rejects; the mutant keeps it
  used with `_ = x`. That cut non-compiling mutants from 249 to 39 (§5).

## Packages

| Package | Provides |
|---|---|
| `mutant` | `Find`: the mutants of a type-checked file, with equivalence and arid tags. `Embed`: all of them in one program (schemata). `Extreme`: one mutant per function, its body removed |
| `mutant/killmatrix` | Runs tests against mutants and records which test kills which: the full kill matrix, or verdicts only |
| `mutant/adequacy` | Dominator mutants, the dominator score of a test set, the selective mutation protocol, and the mutants coupled to a real fault |
| `cmd/mutant` | The command above |
| `cmd/killmatrix`, `cmd/simulate`, `cmd/faults` | The measurement tools behind `docs/` |

## Limits

- The command needs Go 1.27, this module's version. A module under test
  older than Go 1.21 has its mutated files set to 1.21 by build constraint,
  which keeps its loop semantics.
- A package that `os` or `strconv` depend on cannot hold the schema runtime;
  its mutants build one by one.
- Mutants in constant expressions (`const`, array sizes) cannot call a
  function and build one by one.

[ROADMAP.md](ROADMAP.md) lists what is still open.
