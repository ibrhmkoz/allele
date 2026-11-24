# allele

Mutation testing for Go. `allele` makes small, deliberate changes to your
code (`>` to `>=`, `&&` to `||`, a statement deleted, `return err` to
`return nil`) and runs your tests against each. A change no test notices is
a gap in your tests. The output lists those gaps as the line before and
after, not as a score.

```
$ allele ./...
survived  xtesting.go:73  SDL
  -	EqualText(t, v.String(), want)
  +	(deleted)

survived  version.go:183  ROR
  -	if len(v) > MaxVersionLen {
  +	if len(v) >= MaxVersionLen {

381 mutants: 295 killed, 86 survived, 0 not covered, 0 timed out, 0 did not build
in 4.2s
```

(Excerpts from two runs: this repository's `internal/xtesting`, before its
tests were fixed, and Masterminds/semver.) The first gap was real: deleting
the call that does `xtesting.Prints`'s work passed every test, because no
test checked that `Prints` fails on a mismatch.

The name: each line's mutants are its alleles, variants of the same code,
and `allele` picks the one most likely to show a gap.

## Install

With Homebrew (macOS, Linux):

```
brew install ibrhmkoz/tap/allele
```

With Go 1.27 or later:

```
go install github.com/ibrhmkoz/allele/cmd/allele@latest
```

Or download a binary for macOS, Linux or Windows from the
[releases](https://github.com/ibrhmkoz/allele/releases). Running it needs
Go 1.27 or later on the machine either way: it builds and runs your tests.
`allele -version` prints the version.

## Usage

| Command | What it runs |
|---|---|
| `allele ./...` | every package, one mutant per line |
| `allele -diff main ./...` | only lines changed since `main`, working tree included |
| `allele -all ./...` | every mutant, not one per line |
| `allele -arid ./...` | also mutants in logging, metrics and capacity hints |
| `allele -strict ./...` | exit 1 when a mutant survives or no test reaches it (CI) |
| `allele -json ./...` | one JSON object per mutant, every verdict included |
| `allele -no-cache ./...` | ignore and do not update the per-package verdict cache |
| `allele -j 4 -v ./...` | 4 mutants at once, progress on stderr |

Each mutant gets a verdict: **killed** (a test failed), **survived** (all
tests passed: a gap), **not covered** (no test runs its function),
**timeout** (a test ran 3× its usual time plus 100 ms), or **build error**.

Mark a line no test should be asked to check with `//allele:ignore`, at its
end or alone on the line above.

## How it compares

On 61 real bug fixes from the corpus' history, each run on the fixed code
with the tests from before the fix (the suite that missed the bug), a tool
"finds" the fault when it reports a surviving mutant on a line the fix
changed. Same machine, `-j 8`:

| Tool | Faults found | Survivors on the fix's lines, per fault | Seconds |
|---|---|---|---|
| `allele` | 43 of 61 | 2.2 | 181 |
| `allele -all` | 48 of 61 | 6.2 | 384 |
| Gremlins 0.6.0 | 26 of 61 | 1.2 | 1349 |
| Gremlins, every operator | 21 of 61 | 1.2 | 1445 |

On eight corpus libraries `allele` ran 1.6 to 14 times faster than Gremlins.
go-mutesting runs one mutant at a time (shlex: 86 s against 1 s). The
details and what was not measured are in [ROADMAP.md](ROADMAP.md).

## What it does, and why

Every default below was measured before it was chosen; the numbers come from
[`docs/measurements.py`](docs/measurements.py) (`make measurements`), on ten
libraries and three services at pinned commits. One number, M, weighs the
gaps a build finds against the time it takes; `cmd/bench` computes it, and
ROADMAP.md tells how each change moved it.

- **Operators:** ROR (relational), COR (`&&`/`||`, an `if` condition to
  `true`/`false`), AOR (arithmetic), SDL (statement deletion), ERR (`err !=
  nil`, `return err`). ROR, COR and statement deletion are the ones most
  tied to real faults in the literature (Just et al., FSE 2014). On the
  corpus' own history, 74% of real bug fixes are coupled to a mutant on the
  lines they changed, against 2% for random tests of the same size (§10).
- **One build per package (schemata):** every mutant is built into one
  binary and switched on at run time by `ALLELE`. Builds went from 47
  minutes to 0.9 over the corpus, with the same kill matrix as a build per
  mutant (§8). Functions without the active mutant run as written (§20).
- **Only the tests that can kill:** a baseline run records which functions
  each top-level test reaches and which mutants it infects, that is, makes
  the mutated code compute a different value. A mutant runs only the tests
  that reach its function and infect it (§9, §19).
- **Fail fast:** a mutant stops at its first kill, cheapest tests first.
- **One mutant per line, picked by what the tests miss:** after the
  baseline, each line keeps a mutant whose site no test reaches, else an
  `if` forced true or false that no test infects, else the one the fewest
  tests infect. On real faults this picks the mutant coupled to the fault
  more often than a fixed pick, with fewer survivors (§21).
- **Equivalent mutants skipped:** `len(s) == 0` → `len(s) <= 0` and
  `s != ""` → `s > ""` cannot change behavior; no test killed one of the 72
  in the corpus (§5, §11).
- **Arid mutants skipped:** mutants in logging, metrics, tracing, capacity
  hints and `//allele:ignore` lines. Two blind raters called all 21 surviving
  ones unproductive (§13).
- **Kills confirmed:** a test counts as killing a mutant only if it fails
  against it twice and passes on the original code. Timing tests fail under
  load and would otherwise invent kills that hide real gaps (§13).
- **Mutants that would not compile are repaired:** deleting a statement can
  leave a variable or import unused, which Go rejects; the mutant keeps it
  used with `_ = x`. That cut non-compiling mutants from 249 to 39 (§5).

## Packages

`pkg/` holds what makes sense without allele; `internal/` is allele itself.

| Package | Provides |
|---|---|
| `pkg/gotest` | Builds Go test binaries and runs their tests: every test's outcome, subtests included, a time limit, and no process left behind |
| `pkg/adequacy` | Dominator mutants, the dominator score of a test set, the selective mutation protocol, and the mutants coupled to a real fault, over any kill matrix |
| `pkg/fingerprint` | A hash of what a package's test results depend on (its files, dependencies, embeds, testdata), with each test hashed apart: a test cache's key |
| `pkg/xtesting` | Test helpers that compare text and print a readable diff |
| `internal/mutant` | `Find`: the mutants of a type-checked file, with equivalence and arid tags. `Embed`: all of them in one program (schemata) |
| `internal/killmatrix` | Runs tests against mutants and records which test kills which: the full kill matrix, or verdicts only |
| `cmd/allele` | The command above |
| `cmd/bench` | Scores builds of `allele` with M (see ROADMAP) |
| `cmd/killmatrix`, `cmd/simulate`, `cmd/faults` | The measurement tools behind `docs/` |

## Data

`docs/` holds everything the measurements rest on: exhaustive kill matrices
of ten libraries, verdicts of three services, 79 real faults with the
mutants coupled to each, and 92 hand-labelled survivors. There is no other
public mutation-testing dataset for Go that we know of.
[`docs/DATA.md`](docs/DATA.md) describes each table.

## Limits

- The command needs Go 1.27, this module's version. A module under test
  older than Go 1.21 has its mutated files set to 1.21 by build constraint,
  which keeps its loop semantics.
- A package that `os` or `strconv` depend on cannot hold the schema runtime;
  its mutants build one by one.
- Mutants in constant expressions (`const`, array sizes) cannot call a
  function and build one by one.
- **Verdict cache:** `allele` caches verdicts per package under
  `os.UserCacheDir()/allele`, keyed by `go env GOVERSION`/`GOOS`/`GOARCH`,
  runner flags, package and `//go:embed` files, `testdata/`, test files,
  and transitive non-stdlib dependencies (including test-only
  dependencies). It does not track environment variables, external services
  (such as a database), or files read outside the package's `testdata/`;
  pass `-no-cache` when those matter (e.g. `FLAGS=-no-cache make ci`).

## License

The code is under the [MIT License](LICENSE). The data in `docs/` is under
[CC BY 4.0](docs/LICENSE-DATA); it quotes code from the corpus projects,
whose licenses are in [`docs/licenses/`](docs/licenses).

[ROADMAP.md](ROADMAP.md) lists what is still open.
