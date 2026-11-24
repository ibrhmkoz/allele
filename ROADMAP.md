# mutant roadmap

What is open, with enough context to pick it up cold. Findings live in
[`docs/measurements.py`](docs/measurements.py); section numbers (§) refer to
it. The working rule so far: measure an idea on the corpus before it goes
into `cmd/mutant`, and keep the measurement in the notebook.

## The metric

Changes to `cmd/mutant` and what it runs are judged by one number,
`M = D · F · ((T0 + 2) / (T + 2))^0.18`, from `cmd/bench`:

- **D**, detection: the dominator score (Kurtz, Ammann et al., FSE 2016)
  of tests written until every mutant the build reports is dead,
  simulated on the exhaustive matrices of `docs/data`; geometric mean
  over the ten libraries, so giving up on one gives up everything.
- **F**, verdict fidelity: one minus weighted errors per mutant the two
  exhaustive runs (`data`, `data-rerun`) agree on. A hidden gap costs 5,
  a false gap or a mutant that no longer builds 2, a kill reported as a
  timeout 0.5.
- **T**, cost: summed wall seconds on the ten libraries, `-j 8`, cold
  verdict cache, the median of 3 reps, builds run in turns.
- **0.18** prices speed: twice as fast makes up for 11.7% less `D · F`
  and no more. T0 = 876 s, the naive exhaustive runner, scores M = 1.

Merge a change when M rises beyond the noise of interleaved reps on a
quiet machine; keep the constants, the corpus commits and the reference
matrices frozen, or the numbers stop being comparable. Held out, never
tuned on: `data-faults-heldout/` (checked when operators or filters
change) and `data-verdicts/` (flaky services: dropping Confirm would look
free on the ten libraries).

**M v2** (from 2026-11-24) adds bloom, sjson, decimal and xstrings to the
six of v1, so that no one library's slow test sets the time: in v1 a
single 1 s test in uuid was a third of T. T0 stays 876 s, so v2 values
are comparable with each other, not with v1's; v1's log is in §18-19.

## Where M stands

**M v2** (ten libraries) is about 1.35-1.36 since R2b (2026-11-24): each
line's mutant is picked after the baseline, by what the old tests reach
and infect (§21), D 0.876 → 0.906. Checking reach marks inline (#40,
+0.75%) paid for recording every candidate. Against main with #40, R2b
measured +1.1% and +1.2% in two runs (`data-bench/v2-r2b.ndjson`,
`v2-r2b-final.ndjson`): T rises 79 → 85-88 s, since the baseline records
3.3× the mutants and the picks are harder to kill. Merged below the
+1.5% speed gate because it is a selection change: D +3.4% on the corpus
and J +0.086 on real faults, with F unchanged.

Before that it went from 1.258 to 1.333: reach
marks in an atomic table (#31, +1.3%), cloning each mutated function so
the rest run as written (#32, §20, +4.4%), and an ERR fidelity fix (#33).
T is now 80 s: sjson 26 s (a 2-million-iteration fuzz test reaches most
survivors) and decimal 26 s. What is left:

- **Overhead inside the active function**: the clone only spares the
  functions without the active mutant; the one with it still runs its
  helpers. Closure-free conditions did nothing (+0.08%,
  `data-bench/v2-closure-free.ndjson`), so it is not the closures; the
  next suspects are the generic helpers and `_mutPick`'s loop.
- **Timeouts**: decimal has ~20 mutants that one test kills fast and
  another hangs on; FailFast stops at the hang (0.5 each in F, ~0.26% of
  M). One mutant (decimal.go:300, + 9 → / 9) times out in some runs and
  survives in the reference: it slows a test past 9x its baseline.
  TimeoutFactor 2: +0.6%, inside the noise (`v2-factor2.ndjson`).

- **What T is now**: with functions cloned, the schema binary runs
  sjson's fuzz test 1.2x as long as the plain one, and 1.1-1.2x with a
  mutant of its hottest function active: the overhead is gone, and T is
  the tests' own time. sjson's 1 s fuzz test and decimal's 3 s
  TestDecimal_ExtremeValues run for every mutant they reach that faster
  tests do not kill. The baseline, which no mutant can start before, is
  the one serial step: 6-7 s in sjson, where reach recording makes the
  fuzz test 5x slower.
- Checked again under M v2: Infect still pays (off: -1.4%,
  `v2-infect-off.ndjson`); marking a site with one load instead of six
  saves sjson 1.2 s but moves M by +0.1%, inside the noise
  (`v2-mark-fast.ndjson`). decimal.go:300's timeout moves F by about
  0.002 from run to run.

### M v1 (six libraries)

M went from 1.567 to 1.733 on 2026-11-23 (timeout pad §18, weak mutation
§19, online ordering dropped). The last ideas each measure at or under 2%,
so the cheap levers are spent. What is left, by measured upper bound:

- **Skip Confirm where no test can be timing-dependent** (+2.2% at most,
  `data-bench/confirm-pure.ndjson`): packages whose code, tests and
  non-standard dependencies import none of time, net, context, math/rand,
  crypto/rand, os/exec and start no goroutine. Confirm off everywhere
  saves 32% of T but hides 3 gaps in uuid (`confirm-off.ndjson`), so it
  must stay where timing can matter. Risk: map-order flakes.
- **A deadline per test inside a batch** (+0.9% at most,
  `batch-limit.ndjson`): batches are limited by the sum of their tests'
  baselines, so a hang is found late; reading test2json as it streams
  could apply each test's own limit. A hanging mutant is the tail of
  its package.
- **Not worth it:** a fork server. After its first run, a test binary
  starts in under 10 ms; the first run of each new binary costs 0.1-0.3 s
  on macOS (code-signature check), once per package, paid in parallel.
- **Survivors** take three quarters of mutant run time: declaring one
  means running every selected test. What remains is the tests' own time
  (uuid: one 1 s test reaches 72 survivors).
- **Detection (D)** is at its plateau for one mutant per line: no per-line
  choice beats a hash held out by repository (§2; learned kind order 0.877
  vs 0.897), and more mutants per line cost more than they find under the
  metric's price of time. New operators need many more held-out faults.

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

### Arid rules: what they miss
§13: every tagged survivor was unproductive, but 1 of 21 untagged ones was
too: a mutant in an error message's arguments (`spec[eq+1:i]` quoted in
`fmt.Errorf`). A rule for "arguments of an error message" is the obvious
next one, but some projects test their error text, so measure before
adding: how often are such mutants killed in `data/` and `data-verdicts/`?
None of the three services uses metrics or tracing libraries, so those
rules are untested by data; a service using Prometheus or OpenTelemetry
would cover them.

## Known limits

- **`-diff` misses untracked files:** it reads `git diff`, so a new file
  is not mutated until it is added (`git add -N` is enough). `make ci` on
  a branch with an uncommitted new package passes without testing it.

- **Test granularity:** columns are leaf subtests. Table tests that loop
  without `t.Run` (go-version: 33 top-level tests, 36 leaves) give coarse
  kill sets and few dominators, which makes their scores coarse (§1–2).
- **Timing tests:** confirmed kills cost up to three extra runs per kill;
  yamux's verdicts take over an hour at `-j 8`, and a `killmatrix` diff of
  25 killed mutants in `make ci` takes 24 s instead of 14.
- **Schema runtime:** it imports `os` and `strconv`, so a package they depend
  on cannot embed mutants; they build one by one.
- **Reproducibility:** two identical full runs differ on about 150 of
  608 000 cells, from timeouts and mutants that make code nondeterministic
  (§7). Any differential test against a stored matrix has to allow for it.
- **ci budget:** `make ci` runs `mutant -strict -diff main` on the lines
  changed since `main`: 7.0–7.9 s uncached when no package has a mutant,
  and 11.9–13.1 s for a 1-mutant edit in `killmatrix` (down from 23.7–25.1 s
  after parallelizing packages in `Runner.Run`, `BaselineRuns: 1` in
  `cmd/mutant`, and speeding up `mutant`/`killmatrix` tests).
- **Corpus clones** live outside the repository (`CORPUS_CACHE`); rerun
  `docs/corpus.sh` to get them back, and `git fetch --unshallow` before
  `cmd/faults`, which needs history.

## Measured and dropped

Kept so they are not retried without new evidence.

- **A second, narrow schema for R2b's mutant runs** (only the kept
  mutants, after the baseline needed them all): M 1.3598 against 1.3618
  without it; it costs a build per package. Not merged.
- **M v3, D^0.7·R5^0.3·F·S(T)** (§21), with R5 the chance that reading 5
  random reported survivors of a real fault's tree finds one coupled to the
  fault. R5 rewards reporting fewer survivors whatever they are: the
  control rule "most infected" scored 0.432 against hash's 0.412. A
  single number must fix an exchange rate between faults found and noise
  read; Youden's J states it (1) and passed the control, but at 79 faults
  it resolves only 8–10 points, too coarse to gate PRs. M stays v2;
  `cmd/bench -faults` reports R5 on `docs/faulttrees.sh`'s trees beside M
  as a diagnostic.
- **Per-line selection by "fewest infected", "uninfected first" or
  "tiered"** (§21): each wins one one-sided measure (D, per-line hit) and
  loses J against hash on the held-out faults (−0.068, −0.118, −0.186).

- **Ordering FailFast's tests by online kill rates, and tests that reach
  the mutant's site first** (§15): by `cmd/bench`, with the 100 ms pad and
  batches in place, switching either off moved M by -0.45% and -0.33%,
  inside the noise (`data-bench/ablate.ndjson`; batching off: -1.3%, so it
  stays). Tests now run fastest first. Restore from the commit before the
  one that removed `killStats`.
- **Closure-free conditions in the schema** (`(_mutIs(t) || !_mutIs(f) &&
  _mutCond(cond, ...))`, De Morgan for && and ||): +0.08% in M v2, noise.
  Not merged; the closures cost nothing measurable once functions are
  cloned.
- **Selecting tests by the site, not the function, with the 100 ms pad**:
  +0.7% in M, inside the noise; uuid's slow test reaches the sites of the
  survivors it costs. Not merged.

- **Extreme mutation as a prefilter** (§12): a quarter of the mutants in
  pseudo-tested functions are killed by some test; skipping them hides
  kills to save 3% of mutants. Its code (`mutant.Extreme`, `killmatrix
  -extreme`, `EXTREME=1 corpus.sh`) was removed; §12 reads the stored
  `data-extreme/`. To rerun it, restore the code as of commit `ff8bb8e`:
  `git checkout ff8bb8e -- mutant/mutant.go mutant/killmatrix/killmatrix.go
  mutant/cmd/killmatrix/main.go mutant/docs/corpus.sh`.
- **Selecting tests by the line they reach** (§9): a clock-dependent branch
  ran in no baseline yet ran against the mutants. Selection is by function.
- **Confirming a kill by rerunning the mutant only**, or **by checking the
  original right away** (§13): the first keeps kills a loaded machine
  invents, the second throws away real ones. Kills the original also fails
  are rechecked alone at the end.
- **Wild-caught operators** (§14): LIT, CPY and PNC, inverted from the
  fixes of the 12 faults no old mutant stood for. They coupled 5 more of
  the corpus' faults, the ones they came from, and none of 45 held-out
  faults from five other repositories, for 15% more mutants. The code is
  at tag `experiment/wild-caught` (`git checkout experiment/wild-caught --
  mutant/mutant.go mutant/embed.go`). Worth retrying only with many more
  held-out faults, such as govalidator's 60 candidates, which were left
  unfinished.
- **Confirming only tests seen flaky** (§17): trusting a test after N clean
  confirmations in a row hides gaps long before it saves runs, since
  flakiness follows the load, not the test. At N = 10 it still confirms 77%
  of kills and calls 4 of 345 flaky-only mutants killed; at N = 5, 296.
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
