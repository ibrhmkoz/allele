# Data

Everything `docs/measurements.py` shows is computed from the files here.
They are newline-delimited JSON, some gzipped; DuckDB reads them as they
are (`select * from read_json_auto('docs/data/glob/mutants.ndjson.gz')`).
The data is under [CC BY 4.0](LICENSE-DATA). It quotes code from the
projects below (in `original`, `mutated` and the labels' `context`); that
code stays under its own license, in [`licenses/`](licenses).

## Sources

The corpus (`corpus.sh` clones each at the commit shown):

| Name | Repository | Commit | License |
|---|---|---|---|
| glob | github.com/gobwas/glob | ff0540d668f8 | MIT |
| go-humanize | github.com/dustin/go-humanize | f5b54ae13923 | MIT |
| go-version | github.com/hashicorp/go-version | 5ca63ba49818 | MPL-2.0 |
| semver | github.com/Masterminds/semver | dd2b995c61c3 | MIT |
| shlex | github.com/google/shlex | e7afc7fbc510 | Apache-2.0 |
| uuid | github.com/google/uuid | 2d3c2a9cc518 | BSD-3-Clause |
| bloom | github.com/bits-and-blooms/bloom | c4c57f87f860 | BSD-2-Clause |
| sjson | github.com/tidwall/sjson | 752368dfbbb1 | MIT |
| decimal | github.com/shopspring/decimal | a5eee101f66d | MIT |
| xstrings | github.com/huandu/xstrings | 01bf78f57442 | MIT |
| retryablehttp | github.com/hashicorp/go-retryablehttp | 7e8e6f42169e | MPL-2.0 |
| cron | github.com/robfig/cron | bc59245fe10e | MIT |
| yamux | github.com/hashicorp/yamux | 98ba9c9ac60f | MPL-2.0 |

Held out from every choice made on the corpus, for real faults only
(`data-faults-heldout/`, history up to the commit shown):

| Name | Repository | Commit | License |
|---|---|---|---|
| blang-semver | github.com/blang/semver | 4487282d7812 | MIT |
| cast | github.com/spf13/cast | d8e892d16cab | MIT |
| mapstructure | github.com/mitchellh/mapstructure | 8508981c8b6c | MIT |
| mux | github.com/gorilla/mux | db9d1d0073d2 | BSD-3-Clause |
| strcase | github.com/iancoleman/strcase | 531aaa44de12 | MIT |

## Directories

| Directory | What | Notebook |
|---|---|---|
| `data/` | Exhaustive kill matrices of the ten libraries: every mutant against every test, built one per mutant | §1–7, the reference of `cmd/bench` |
| `data-rerun/` | The same, run again: how much two identical runs differ | §7, `cmd/bench` |
| `data-j1/` | Two repositories with one worker, against `data/`'s eight | §6 |
| `data-schemata/` | With every mutant in one schema binary per package | §8 |
| `data-select/` | Schemata, and only the tests that reach a mutant | §9, the reference of §15–16 |
| `data-fast-failfast/` | FailFast with fast tests batched and ordered online | §15 |
| `data-speed/` | Parallel baseline, and FailFast stopping at a timeout | §16 |
| `data-extreme/` | Extreme mutation: a whole function body removed | §12 |
| `data-verdicts/` | Verdicts only (FailFast and Confirm) of the three services | §13, §17 |
| `data-faults/`, `data-faults-heldout/` | One row per real bug fix in each repository's history (`cmd/faults`) | §10, §14 |
| `data-faults/detail.ndjson.gz` | One row per mutant of each measured fix: coupled to the fault or not, and how many old tests reach and infect it | §21 |
| `data-faults-wild/` | Faults with operators inverted from real fixes | §14 |
| `data-selection/` | Per-line selection rules scored on the real faults (`faultsel.py`) | §21 |
| `data-bench/` | `cmd/bench` runs: one row per timed run, per repository score and per build's M | §15–20, ROADMAP |
| `data-compare/` | Other Go mutation tools, run by `compare.py` | ROADMAP |
| `labels/` | 92 survivors labelled useful, equivalent or unproductive by two blind raters, then adjudicated | §11, §13 |

## Kill matrices

Each matrix directory has, per repository, three tables (the `json` tags
of `killmatrix.Mutant`, `Test` and `Result`).

`mutants.ndjson.gz`, one row per mutant:

| Field | Meaning |
|---|---|
| `id` | unique within the repository's matrix; results refer to it |
| `pkg`, `file`, `line`, `col` | where: import path, file base name, position of the change |
| `operator` | `ROR`, `COR`, `AOR`, `SDL` or `ERR` |
| `node` | the Go syntax node changed, as `go/ast` names it |
| `original`, `mutated` | the source before and after; `mutated` is empty for a deleted statement |
| `equivalent` | why the mutant cannot change behavior, empty if it can |
| `repaired` | the mutant was rewritten to compile (`_ = x` for a now unused variable) |
| `arid` | why the mutant is in code no test checks (logging, a capacity hint, an ignore comment), or empty |
| `compiled` | the mutant built |
| `schema`, `build_ms` | it ran in the package's schema binary; its build time, or its share of the schema build |

`tests.ndjson.gz`, one row per test, subtests included:

| Field | Meaning |
|---|---|
| `id`, `pkg`, `name` | the test, as `go test -run` names it |
| `baseline_ms` | its time on the unmutated code |
| `state` | `ok`, `flaky` (passed and failed across baseline runs) or `failing`; only `ok` tests run against mutants |

`results.ndjson.gz`, one row per test run against a mutant:

| Field | Meaning |
|---|---|
| `mutant_id`, `test_id` | which pair |
| `status` | `killed`, `survived`, `timeout` or `flaky` (failed against the mutant only some of the time) |
| `ms` | the run's time; 0 when the test did not run |
| `skipped` | the test did not run because, in the baseline, it never reached the mutant's site |

A mutant's verdict follows from its results: killed if any test killed it,
else timeout, else survived; not covered with no result; build error when
it did not compile.

## Real faults

`data-faults*/<repo>.ndjson`, one row per commit whose message says it
fixes a bug and that changed a test:

| Field | Meaning |
|---|---|
| `repo`, `commit`, `subject`, `pkg` | the fix |
| `status` | `ok`, or why the row was dropped |
| `revealing` | the tests that fail before the fix and pass after |
| `mutants`, `killable` | the package's mutants, and those some test kills |
| `coupled` | mutants killed by a revealing test and no other: coupled to the fault |
| `on_fix`, `coupled_on_fix` | of those, on lines the fix added or changed |
| `coupled_operators`, `coupled_on_fix_operators` | the coupled ones by operator |
| `chance`, `chance_on_fix` | the share of 200 random test sets of the same size that would look as coupled; -1 when too few tests exist |

`data-faults/detail.ndjson.gz`, one row per mutant of each fix in the two
sets above: `repo`, `commit`, `pkg`, `file`, `line`, `col`, `operator`,
`original`, `mutated`, `equivalent`, `arid` as in the matrices, and

| Field | Meaning |
|---|---|
| `killable` | some test of the fixed code kills it |
| `coupled` | only the fix's revealing tests kill it |
| `on_fix` | it is on a line the fix changed |
| `reached`, `infected` | how many top-level tests from before the fix reach its site and change a value there; -1 when not measured |

## Labels

`labels/sample.ndjson` and `arid-sample.ndjson` are the survivors sampled
for labelling, with `key` (`repo#mutant id`), the mutant's fields and the
source `context` around it. `claude.ndjson`, `gemini.ndjson` and the
`arid-` files are each rater's labels; `adjudicated.ndjson` and
`arid-adjudicated.ndjson` the final ones, with `how` the label was settled:
agreement, or a run that showed the mutant changes behavior or does not.
`prompt.md` and `arid-prompt.md` are what the raters were given.

## Reproduce

The commands are in the notebook's last cell. They need the corpus clones
(`corpus.sh` makes them under `$CORPUS_CACHE`, by default
`~/.cache/allele-corpus`) and, for the real faults, the repositories' full
history (`git fetch --unshallow`).
