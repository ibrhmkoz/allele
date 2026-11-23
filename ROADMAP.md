# mutant roadmap

What is open, with enough context to pick it up cold. Findings live in
[`docs/measurements.py`](docs/measurements.py); section numbers (§) refer to
it. The working rule so far: measure an idea on the corpus before it goes
into `cmd/mutant`, and keep the measurement in the notebook.

## Open

### Compare with other Go tools
Run [Gremlins](https://github.com/go-gremlins/gremlins) and
[go-mutesting](https://pkg.go.dev/github.com/jonbaldie/go-mutesting/v2) on
the six libraries of `docs/corpus.sh` and compare:

- **cost** (axis B): wall time and builds, against `mutant`'s default and
  `-all` runs at the same `-j`;
- **effectiveness** (axis C): map each tool's surviving mutants to lines and
  ask how many of `mutant`'s dominator mutants (§1) sit on lines the tool
  mutated at all, and how many real faults of `data-faults/` (§10) the
  tool's mutants are coupled to.

Neither tool records which test kills which mutant, so their kill matrices
cannot be compared cell by cell; verdicts per mutant and lines covered can.
Deferred on 2026-11-23 as too long for that session.

### Trivial Compiler Equivalence (TCE)
Compile each mutant and compare the machine code with the original's and
with other mutants' (Papadakis et al., ICSE 2015): identical code means an
equivalent or duplicate mutant, found without running a test. It needs a
build per mutant, which schemata removed, so the plan is to apply it only
where it can pay off:

1. Compile per function, not per package: `go build -gcflags=-S` on the
   package with one mutant, and compare the assembly of the mutant's
   function only.
2. Measure first on the 12 equivalents of §11 that no cheap rule catches,
   and on the 72 string-vs-`""` mutants the rule already tags: how many does
   TCE prove, at what build cost per mutant?
3. If it pays, run it as a pass that tags `Mutant.Equivalent`, like the
   rules, so `-all` runs stay honest.

### Publish the corpus as a benchmark
There is no public mutation-testing benchmark for Go. Everything is in
`docs/`: kill matrices of six libraries (`data/`, plus a rerun, schemata,
selection, `-j 1` and extreme runs), verdicts of three services
(`data-verdicts/`), 47 real faults (`data-faults/`) and 92 hand labels
(`labels/`). Missing:

- a `docs/data/README.md` describing each table's columns (they are the
  `json` tags of `killmatrix.Mutant`, `Test` and `Result`);
- the licenses and commits of the corpus repositories, with attribution:
  the data quotes their code in `original`/`mutated` and in the label
  contexts;
- a place to publish (Zenodo gives a DOI) and a citation.

### Wild-caught mutants: operators from the corpus' own bug fixes
Invert real fixes into operators (Beller et al., *What it would take to use
mutation testing in industry*, ICSE SEIP 2021, call them wild-caught
mutants): a fix that turned `x > n` into `x >= n` yields `>=` → `>`. This
is not learning from a large corpus; it needs only the fix pairs §10
already holds.

The 12 real faults of §10 that no mutant on the fix's lines stands for say
which operators to try. Their fixes (2026-11-23):

| Pattern | Faults | Example fix | Inverse operator |
|---|---:|---|---|
| String or regexp literal | 6 | `\s*-\s*` → `\s+-\s+`; a space added to a `Sprintf` format | change a character, class or quantifier inside a literal |
| Defensive copy | 2 | return a copy of `v.segments`; `new(big.Int).Set(b)` | return the original instead of the copy |
| Error instead of panic | 1 | `panic(err)` → `return nil, err` | `return ..., err` → `panic(err)` |
| Larger rewrite or refactor | 3 | | none |

None of these is in `Find` today, so literal mutation comes first.

The trap: operators derived from these 12 faults and measured on the same
12 prove nothing. Measure on held-out faults: derive from some
repositories and score the others, or mine more fixes with `cmd/faults`
from repositories outside the corpus. The question to answer is the one
§10 asks: does the share of faults coupled to a mutant on the fix's lines
rise above 74%, and at what cost in mutants (literals are everywhere, so
expect many, and many equivalent: a regexp's redundant character)?

### Arid rules: what they miss
§13: every tagged survivor was unproductive, but 1 of 21 untagged ones was
too: a mutant in an error message's arguments (`spec[eq+1:i]` quoted in
`fmt.Errorf`). A rule for "arguments of an error message" is the obvious
next one, but some projects test their error text, so measure before
adding: how often are such mutants killed in `data/` and `data-verdicts/`?
None of the three services uses metrics or tracing libraries, so those
rules are untested by data; a service using Prometheus or OpenTelemetry
would cover them.

### Finish the service verdicts
`SCHEMATA=1 SELECT=1 FAILFAST=1 CONFIRM=1 OUT=mutant/docs/data-verdicts
mutant/docs/corpus.sh yamux` takes over an hour (each kill is confirmed
twice, and the loaded ones rechecked alone). If `data-verdicts/yamux/` is
missing, run it; §13's table gains a row, nothing else changes.

## Known limits

- **Test granularity:** columns are leaf subtests. Table tests that loop
  without `t.Run` (go-version: 33 top-level tests, 36 leaves) give coarse
  kill sets and few dominators, which makes their scores coarse (§1–2).
- **Timing tests:** confirmed kills cost up to three extra runs per kill;
  yamux's verdicts take over an hour at `-j 8`. A cheaper confirmation
  (rerun only kills by tests seen flaky at least once) is untried.
- **Schema runtime:** it imports `os` and `strconv`, so a package they depend
  on cannot embed mutants; they build one by one.
- **Reproducibility:** two identical full runs differ on about 150 of
  608 000 cells, from timeouts and mutants that make code nondeterministic
  (§7). Any differential test against a stored matrix has to allow for it.
- **ci budget:** `make ci` takes 8–9 s uncached, close to the 10 s budget;
  most of it is `killmatrix`'s fixture runs.
- **Corpus clones** live outside the repository (`CORPUS_CACHE`); rerun
  `docs/corpus.sh` to get them back, and `git fetch --unshallow` before
  `cmd/faults`, which needs history.

## Measured and dropped

Kept so they are not retried without new evidence.

- **Extreme mutation as a prefilter** (§12): a quarter of the mutants in
  pseudo-tested functions are killed by some test; skipping them hides
  kills to save 3% of mutants.
- **Selecting tests by the line they reach** (§9): a clock-dependent branch
  ran in no baseline yet ran against the mutants. Selection is by function.
- **Confirming a kill by rerunning the mutant only**, or **by checking the
  original right away** (§13): the first keeps kills a loaded machine
  invents, the second throws away real ones. Kills the original also fails
  are rechecked alone at the end.
- **Single operators** (§2): ROR or AOR alone loses to as many random
  mutants; dropping any one operator is no better than dropping random
  ones. COR alone and one mutant per line beat random.
- **"Mutants on one line share their fate"** (§4): true on more than 90% of
  lines at Google; 22–56% here, near chance in some repositories. One
  mutant per line still beats random.

## Not planned

- Higher-order mutants, operators learned by a model (Facebook's Mutation
  Monkey, Google's context-based selection): they need far more data than
  one corpus. Operators inverted from real fixes are a different matter and
  are open above.
- A mutation score as the main output: scores correlate weakly with fault
  detection once test-suite size is controlled (Papadakis et al., ICSE
  2018); the survivors' diffs are the product.
