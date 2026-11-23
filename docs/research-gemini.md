# Second Opinion on `mutant`: Research & Measurement Plan

This document reviews `mutant` (as of §1–§14 in `measurements.py`), compares it with the 2015–2026 mutation-testing literature and production tools across languages, and ranks concrete ideas to improve **speed** (cost per useful result) or **detection / usefulness** (fault coupling and survivor signal-to-noise).

All preliminary corpus numbers below were computed directly on the stored datasets (`data/`, `data-select/`, `data-verdicts/`, `labels/`, `data-faults/`). **Held-out fault files (`data-faults-heldout/`) were intentionally not inspected** so that any detection hypothesis remains genuinely blind to the held-out set.

---

## 1. Ranked Summary of Ideas

| Rank | Axis | Idea | Preliminary corpus signal | Expected gain | Effort | Recommended for Phase 2? |
|---:|---|---|---|---|---|---|
| **1** | Speed | **Batched fast-test execution (`-test.run "^(T1\|...\|Tk)$" -test.failfast`)** in `FailFast` mode | Median top-level test in all 6 libraries is **5.0–6.5 ms** (mostly OS `fork+exec` + Go runtime init); surviving mutants burn **49–94%** of FailFast time spawning 1 process per test | **3×–8× faster** test execution on libraries (and fast tests in services); identical verdicts | Small–Medium | **Yes (top pick)** |
| **2** | Speed | **Adaptive FailFast test ordering: site-reached first + online kill-rate/time (`p/t`)** (FaMT / Smith's rule) | Simulated on `data-select/`: online hierarchical `p/t` cuts test processes **-29% to -48%** (-34% overall) and test time **-10% to -27%** on 3/6 repos with zero peeking | **-20% to -40%** runs on killed mutants; composes cleanly with Rank 1 (waves) | Small | **Yes (pair with Rank 1)** |
| **3** | Speed | **Timeout-capped FailFast + adaptive `Confirm`** for services | In `yamux`, 2nd+ timeouts on already-timed-out mutants burn **10,602.6 s (51.4% of total CPU time)**; 100% of kills in `cron` and 81% in `retryablehttp` come from tests that never flake | **~2× faster** on `yamux`; **~2× cheaper** confirmation on non-flaky suites | Small | **Yes (strong 2nd pick)** |
| **4** | Detection / Noise | **Cheap static equivalence rules** (canonical `for i := 0; i < len(x); i++` → `!=`, post-guard `return err`) + **TCE (`-gcflags=-S`) benchmark** on the 12 unaddressed equivalents of §11 | `i < len(x)` → `i != len(x)` has **10/10 survived, 0 killed, 0 timeout** in `data/`; 3 of the 12 unaddressed §11 equivalents are loop-bound or known-nil `err` | Removes ~15–25% of remaining equivalent survivors at zero runtime cost; settles TCE ROI with data | Small (rules) / Medium (TCE) | Maybe (if detection axis preferred) |
| **5** | Speed (CI) | **Content-hashed incremental verdict cache** (Stryker `--incremental` / PIT incremental) | `cmd/mutant` already has `-diff`, but rerunning on unchanged packages or across commits re-executes everything | **5×–50× faster** on repeated local/CI runs; 0 gain on cold runs | Medium | Later (product feature) |
| **6** | Detection | **Go-specific operators** (`defer` deletion, `go` removal, `%w`→`%v`, `select` case deletion) on a **service/concurrency fault set** | 6 libraries are single-threaded pure functions; `data-verdicts/` services use goroutines/channels/defers | Unknown until mined on held-out concurrent Go repos | Medium–High | Only with new held-out concurrent faults |
| **7** | Usefulness | **Survivor ranking** (prioritize likely-useful survivors over likely-equivalent ones in `report`) | In §11, 50% of ROR survivors are equivalent vs 29–33% for SDL/COR/AOR; `report` currently sorts by file/line | Better developer UX (top-K precision) without hiding any survivor | Small | Low priority (50 labels too small to validate heavily) |
| **8** | Speed | **Predictive Mutation Testing (PMT)** via ML classifiers | Breaks exactness (false kills hide real gaps); needs large training set | High speed, unacceptable false-kill risk for `mutant`'s diff-first UX | High | **No** (use only for ordering, not skipping) |

---

## 2. What Other Tools Do That `mutant` Does Not

| Tool (Language) | Key techniques `mutant` lacks | Transferable to Go / `mutant`? |
|---|---|---|
| **Gremlins** (Go) | Coverage-guided filtering via `go test -coverprofile` before generating mutants; YAML config; quality-gate thresholds; increment/decrement (`++`/`--`), `break`/`continue`, invert-negatives (`-x`→`x`) | `mutant`'s schemata + `GOMUTANT_REACH` is already faster and more accurate than `-coverprofile` (no rebuild per mutant). Extra operators (`++`/`--`, `break`/`continue`) are trivial to test on held-out faults. |
| **go-mutesting** (Go) | Pluggable AST mutators including concurrency (`MutatorGoroutineRemove` stripping `go`); custom `--exec` hook | `go` removal and `defer` deletion are worth testing on concurrent Go repos; `go-mutesting` lacks schemata and is much slower. |
| **PIT / Pitest** (Java) | **Bytecode-level mutation**; **in-process test execution** (warm JVM workers); **test prioritization by call-graph distance & execution time**; **incremental analysis** (history file skipping unchanged classes/tests); **TCE-like bytecode equivalence** | Go has no JVM classloader isolation, so multi-mutant in-process execution risks global-state pollution. However, **single-mutant multi-test batching** (`-test.run "^(T1\|...\|Tk)$" -test.failfast`), **distance/kill-rate test ordering**, and **incremental caching** transfer directly. |
| **Stryker** (JS/TS, .NET, Scala) | **Schemata** (Stryker.NET/4s); **warm test-runner workers**; **`--incremental` mode** (diffs AST + tests against saved JSON report to reuse verdicts); **per-test coverage** | `mutant` already has schemata and per-test function reach. `--incremental` verdict caching transfers cleanly via AST/package content hashes. |
| **cargo-mutants** (Rust) | **`cargo-nextest` integration** (fast per-test scheduling & timeouts); **function-body / return-value mutations** (default values); **baseline-derived adaptive timeouts**; **sharding across CI machines** | `mutant` already measured and dropped extreme/body mutation as a prefilter (§12). Sharding (`-shard k/n`) is trivial if needed later. |
| **Mull** (C/C++ LLVM) | **LLVM IR mutation** + **fork-based execution** (forking a warm process after test-suite init, avoiding repeated startup) + **compiler-level TCE** | Go binaries cannot safely `fork()` after the Go runtime starts threads. However, Go's `-gcflags=-S` / SSA gives a path to TCE, and batching tests in one process removes most startup overhead without `fork()`. |
| **Mutmut** (Python) | **SQLite verdict cache** keyed by file/mutant hash; **trampoline/schemata** (v3) with `fork()` per mutant; interactive diff review | Content-hashed verdict caching transfers cleanly. |

---

## 3. Detailed Analysis of Ideas

### Idea 1 (Speed): Batched Fast-Test Execution in `FailFast` Mode

- **Claim & source:** Process creation and runtime initialization dominate mutation testing when unit tests are short (milliseconds). PIT, Stryker, and `cargo-nextest` amortize startup by running multiple tests per worker invocation rather than spawning a fresh OS process per `(mutant, test)` pair.
- **Why it holds for Go and this corpus:**
  In `killmatrix.go:783`, `run()` spawns one OS process (`exec.CommandContext(ctx, bin, "-test.run", "^"+top+"$", ...)`) for every `(mutant, top-level test)` pair. On `data-select/`:
  - Median `BaselineMS` of top-level tests is **5.0–6.5 ms** across all 6 libraries (`glob` 6.5 ms, `go-humanize` 5.0 ms, `go-version` 5.0 ms, `semver` 5.2 ms, `shlex` 5.3 ms, `uuid` 5.1 ms). Almost all of that 5 ms is macOS `fork+exec` + Go runtime startup, not test logic.
  - Surviving mutants must run *every* reaching top-level test and consume **49–94% of all FailFast test-run time** (`semver` 49%, `go-humanize` 58%, `glob` 77%, `uuid` 79%, `go-version` 82%, `shlex` 94%).
  - A surviving mutant in `semver` or `go-humanize` currently spawns up to 49 processes (~250 ms) for tests that execute in **~6 ms total** inside one `bin -test.run "^(T1|T2|...|Tk)$" -test.count=1 -test.failfast -test.v=test2json` invocation.
  - Crucially, `go test ./...` itself runs all top-level tests of a package in one process, so batching tests for a **single mutant** preserves Go's standard isolation model and introduces zero cross-mutant state leakage.
- **Design (tiered waves to protect against slow tests and timeouts):**
  Go's `testing` package executes matched tests in declaration order, not regex order. Putting a 5 s test and forty 5 ms tests in one batch would risk running the 5 s test first and inflating the batch timeout. Instead, partition reaching tests into **waves**:
  1. **Wave 1 (fast killers):** fast tests (`BaselineMS < 50 ms`) that reached the mutant's **exact site** (or top-ranked by online `p/t`), batched in chunks whose combined baseline is ≤ 100 ms (so batch timeout is still ~1.3 s). Run with `-test.failfast`.
  2. **Wave 2 (remaining fast tests):** remaining fast tests that reached the function, similarly chunked.
  3. **Wave 3 (slow tests):** tests with `BaselineMS >= 50 ms`, run individually in order of Idea 2 (`p/t` descending).
  4. If `Confirm` is enabled and a batch fails on leaf `T/sub`, confirm *only* `topOf(T/sub)` individually.
- **Expected gain:** **3×–8× reduction in test-run wall time** on the 6 libraries (e.g. `semver` 6,110 procs → ~1,300 procs; `go-humanize` 3,691 → ~1,150), cutting total corpus FailFast wall time substantially; moderate gain on services where many unit tests are fast.
- **Cost & risks:**
  - Low code complexity (~60–100 lines in `killmatrix.go`).
  - Risk 1: A test that mutates package-global state could affect a sibling test in the same batch (rare, because `go test` already runs them together).
  - Risk 2: A batch timeout doesn't isolate *which* test in the batch hung, though for `Verdict` (`FailFast`) any timeout marks the mutant as `Timeout` (and if needed, we can bisect or fall back on timeout).
  - Risk 3: `test2json` output for a batch must be parsed cleanly; `report.parse` already parses arbitrary `test2json` streams by test name.
- **How to measure:**
  - **Correctness (Axis A):** Compare `Verdicts()` of batched `FailFast` vs full `data/` and `data-select/` matrices across all 6 libraries (and `data-verdicts/` for services). Every disagreeing mutant must be within the §7 nondeterminism set (or explained and fixed).
  - **Speed (Axis B):** Run `cmd/mutant -all -j 8` (or `killmatrix -schemata -select -failfast -j 8`) **3–5 repeated runs** before vs after on the 6 libraries and 3 services at the same `-j 8`, reporting median, min–max, and noise.
  - **Win criterion:** ≥ 2× wall-clock speedup on the 6-library FailFast run with 0 verdict regressions outside §7 noise. Record as **§15** in `measurements.py`.

---

### Idea 2 (Speed): Adaptive FailFast Test Prioritization (Site-First + Online `p/t`)

- **Claim & source:** **FaMT** (Zhang, Marinov, Khurshid, ISSTA 2013) showed that prioritizing tests per mutant cuts test executions for killed mutants by up to ~47.5% with zero loss in mutation-score exactness. In scheduling theory, **Smith's rule** proves that minimizing expected time to first kill orders tests by descending `p_i / t_i` (kill probability divided by test duration).
- **Why it holds for Go and this corpus:**
  1. **Unused site-level reach:** `killmatrix.go:354-359` already collects exact mutant IDs reached by each test (`ids` from `GOMUTANT_REACH`), then collapses them to function-level reach (`reached[top][fn[id]] = true`) and discards site-level reach. While §9 proved we cannot *skip* tests that reached the function without reaching the site, we can **run site-reaching tests first**!
  2. **Online kill-rate learning (`p/t`):** Currently `FailFast` sorts strictly by `BaselineMS` ascending (`killmatrix.go:587`). Tests that never kill anything in a file still run before slightly slower tests that kill 80% of mutants in that file.
  3. **Simulation on `data-select/` (zero peeking, hierarchical Bayes backoff from `(file, test)` to `(test)`):**
     - `glob`: 3,730 → 2,972 procs (**-20.3%**), 217.8 s → 205.6 s (-5.6%)
     - `go-humanize`: 3,691 → 2,039 procs (**-44.8%**), 76.1 s → 60.3 s (**-20.7%**)
     - `go-version`: 2,008 → 1,504 procs (**-25.1%**), 74.2 s → 71.4 s (-3.7%)
     - `semver`: 6,110 → 4,428 procs (**-27.5%**), 57.0 s → 41.7 s (**-26.8%**)
     - `uuid`: 4,768 → 2,466 procs (**-48.3%**), 140.2 s → 122.4 s (**-12.7%**)
     - Overall: **-34% test processes** before even adding site-first reach!
- **Expected gain:** **-20% to -40% test runs on killed mutants**, especially impactful on **services** (`cron`, `retryablehttp`, `yamux`) where tests take 100 ms–5 s and cannot all be batched into one 5 ms process.
- **Cost & risks:**
  - Trivial (~40 lines, atomic/mutex-guarded per-function/per-test counters in `Runner`).
  - Zero accuracy risk: exact same set of tests runs if a mutant survives.
  - Note: with `-j 8`, concurrent workers update counters slightly asynchronously, which is fine (only affects ordering, never verdicts).
- **How to measure:**
  - **Offline simulation** on `data-select/` and `data-verdicts/` (compare `BaselineMS` vs site-first vs hierarchical `p/t` vs oracle).
  - **Live wall-clock** on 6 libraries + 3 services (3–5 repeated runs at `-j 8`), both standalone and combined with Idea 1.
  - **Win criterion:** ≥ 15% reduction in test-run time on slow-test suites (services) or ≥ 25% fewer test invocations on libraries, with 0 verdict changes.

---

### Idea 3 (Speed — Services): Timeout-Capped FailFast + Adaptive `Confirm`

- **Claim & source:** Timeouts dominate wall time in concurrent/networked suites (`measurements.py` §6: <0.5% of runs, 30%+ of time at `-j 1`; `ROADMAP.md` Known Limits: `yamux` takes >1 hour at `-j 8` and suggests adaptive confirmation).
- **Why it holds for Go and this corpus:**
  Querying `data-verdicts/` reveals **two** distinct bottlenecks on the 3 services:
  1. **Repeated timeouts on the same mutant (`yamux`):**
     - `yamux` spent **20,620.1 CPU-s** total, of which **12,657.3 s (61.4%)** was timeouts!
     - Because `killmatrix.go:635` only stops `FailFast` on `status == Killed` (not `Timeout`), a mutant that deadlocks a protocol runs *every* remaining test and times out repeatedly: **226 of 347 timed-out mutants in `yamux` timed out multiple times**, burning **10,602.6 s (51.4% of `yamux`'s ENTIRE runtime!)** on 2nd+ timeouts.
     - Note: in `Verdicts()` (`killmatrix.go:137-146`), once a mutant times out, it can **never** become `VerdictSurvived` (it is either `VerdictKilled` or `VerdictTimeout`, both of which pass `-strict`). Stopping on the 1st timeout (or after `K=2` timeouts) changes **0 survivors**, though 73 of those 347 mutants in `yamux` (18 in `retryablehttp`, 2 in `cron`) were eventually killed by a later test and would instead report `timeout`.
  2. **Over-confirming non-flaky tests (`Confirm`):**
     - Currently `Confirm` runs `again` (mutant) + `original` = **3× runs for every single kill**.
     - In `data-verdicts/`:
       - `cron`: 497 kills, only **1** flaky kill (1 of 48 tests: `TestAddWhileRunning`); **100%** of other kills came from tests that never flaked.
       - `retryablehttp`: 252 kills, 103 flaky kills — **all 103** from a single test (`TestClient_Do`, 1 of 32 tests); **81%** of kills came from never-flaky tests.
       - `yamux`: 524 kills, 560 flaky kills across **19 of 35 tests** (16% from never-flaky tests).
     - **Important caveat:** You cannot know a test is flaky *before* its first false kill unless you either (a) run `Confirm` once on a test's first kill, or (b) detect flakiness during baseline/load (e.g., run baseline under `-j` workers instead of serially! Look at `killmatrix.go:348`: baseline runs **serially**, so timing tests pass 3/3 in baseline and only flake under `-j 8` load!).
     - Even better: **two-stage confirmation** — on a kill, re-run the mutant **once** (cost: 2× instead of 3×); only run `original` if the test is known-flaky or fails again under load, OR run baseline under load to tag timing-sensitive tests up front. (Wait: §13 showed rerunning *only* the mutant keeps kills invented by sustained load — so skipping `original` is only safe for tests that have *never* failed on `original` or whose baseline is fast and non-timing, e.g. `< 50 ms`! In `yamux`, let's check if fast tests ever flake.)
- **Expected gain:**
  - Stopping `FailFast` on 1st (or 2nd) timeout: **~50% wall-time cut on `yamux`** (~60 min → ~28 min) with **0 change to survivors**.
  - Adaptive `Confirm` (or enabling `Confirm` by default in `cmd/mutant` only for slow/flaky tests): makes `Confirm` cheap enough to turn on by default in `cmd/mutant` (currently `Confirm` is **off** in `cmd/mutant/main.go:80`!).
- **Cost & risks:**
  - Stopping on first timeout trades `killed` → `timeout` for mutants that deadlock early tests but fail later ones (73 in `yamux`, 18 in `retryablehttp`, 2 in `cron`). In `cmd/mutant`, `timeout` lines are printed in the report, so printing 73 extra `timeout` lines could add noise unless `report` suppresses or summarizes timeouts (or we stop after 2 timeouts).
  - Adaptive `Confirm` risks missing a test's *first* load-induced false kill if it skips confirmation for untested tests; must be verified on `data-verdicts/` services under `-j 8` load.
- **How to measure:**
  - Measure on `retryablehttp`, `cron`, and `yamux` at `-j 8` (repeated runs): wall time, survivor set diff against `data-verdicts/`, and count of false kills vs `data-verdicts/`.
  - **Win criterion:** ≥ 35% wall-time reduction on `yamux` with 0 missed survivors (0 false kills relative to full `Confirm`).

---

### Idea 4 (Detection / Noise): Cheap Static Equivalence Rules + TCE (`-gcflags=-S`) Benchmark

- **Claim & source:** **Trivial Compiler Equivalence (TCE)** (Papadakis et al., ICSE 2015; Kintis et al., TSE 2018) detects ~30% of equivalent mutants in C/Java by comparing compiled machine code / bytecode of original vs mutant. Meanwhile, targeted AST/dataflow equivalence rules cost 0 builds and catch common language idioms.
- **Why it holds for Go and this corpus:**
  Inspecting the 19 equivalent mutants in §11's 50-survivor sample (`labels/sample.ndjson` + `adjudicated.ndjson`):
  - 7 are already caught by existing cheap rules (unsigned/len vs 0, string vs `""`, blank array bounds check `_ = uuid[15]`).
  - Of the **12 remaining equivalents**, **3** match simple, sound static patterns:
    1. **Canonical `for` loop bound (`i < n` → `i != n`):** `go-version#263` (`i < hS` → `i != hS`), `go-humanize#433` (`pos < len(parts[0])` → `pos != len(parts[0])`). When a `for` loop initializes `i := 0`, increments strictly by `i++` (step 1, no other writes to `i` in the body), and compares `i < n` where `n` is nonnegative (`len`, `cap`, unsigned, or positive const), `i` can never skip over `n`, so `i < n` and `i != n` are provably identical. On the full corpus (`data/`), `< len(...)` → `!= len(...)` occurs **10 times: 0 killed, 0 timeout, 10 survived**.
    2. **Post-guard `return ..., err` → `return ..., nil`:** `shlex#52` (`return token, err` immediately after `if err != nil { return ... }`). If every path to `return ..., err` passes through `if err != nil { return/break/continue/panic }` without reassigning `err`, then `err` is provably `nil`, so mutating `err` → `nil` is equivalent.
  - What about **Go TCE (`go build -gcflags=-S`)** on the other 9 equivalents (`glob#867`, `glob#905`, `glob#955`, `glob#734`, `go-humanize#527`, `go-humanize#42`, `go-humanize#386`, `semver#501`, `semver#193`)?
    - Go's compiler does **not** optimize away `i < len(x)` vs `i != len(x)` (different branch instruction) or performance caches (`glob#867`, `glob#905`), nor `v.Sign() < 0` vs `<= 0`. It *might* fold `m == nil` → `false` (`semver#501`) or `return token, err` (`shlex#52`) via SSA prove/nilcheck passes.
    - Importantly, TCE requires a separate compilation per surviving mutant (schemata cannot be used for TCE because all mutants share one function body). Running TCE **only on surviving mutants** (post-filter, not pre-filter!) avoids paying the build cost on the ~75% of mutants that tests already kill!
- **Expected gain:**
  - The 2 cheap AST rules: remove ~15–25% of remaining equivalent survivors at **0 ms** runtime cost.
  - Post-hoc TCE on survivors only: unknown until measured — could prove 1–5 of the 12 remaining equivalents at ~100–300 ms/survivor, or 0 (in which case we drop TCE and document it in ROADMAP).
- **Cost & risks:**
  - Cheap rules: ~80 lines in `mutant.go`; must be verified for **0 false positives** (0 killed or timed-out mutants tagged across the entire 6-library + 3-service corpus!).
  - Post-hoc TCE: ~100 lines to compile a survivor with `-gcflags=-S` and diff the normalized assembly of its enclosing function against the original.
- **How to measure:**
  - **Cheap rules:** Run against all 4,889 mutants in `data/` and all mutants in `data-verdicts/`. Win criterion: **0 killed/timeout counterexamples** and ≥ 15 tagged survivors across the corpus (plus ≥ 2–3 of the 12 §11 equivalents).
  - **TCE experiment:** Run on the 19 equivalents + 27 useful survivors of §11 (`labels/sample.ndjson`). Measure: how many of the 12 unaddressed equivalents have identical normalized assembly, how many useful survivors (must be 0!), and wall time per survivor. If it proves < 2 of the 12, tag `experiment/tce` and drop.

---

### Idea 5 (Speed — CI / Local Iteration): Content-Hashed Incremental Verdict Cache

- **Claim & source:** **Stryker** (`--incremental`) and **PIT** (incremental analysis) persist verdicts from prior runs and reuse them when neither the mutated function/package nor the covering tests have changed. In CI and local edit-test loops, most packages/functions are untouched between runs.
- **Why it holds for Go and this corpus:**
  `cmd/mutant` has `-diff main`, which restricts *which lines* are mutated, but if a developer runs `mutant ./...` locally twice while fixing survivors (the primary workflow!), `mutant` rebuilds the schema and re-runs every mutant from scratch.
  In Go, `packages.Load` + `go test -c` already rely on Go's build cache; a lightweight cache keyed by `(hash(normalized package AST), hash(test files + deps), mutant key)` — or per-mutant `(mutant fingerprint, hash of killing test)` — can skip already-killed mutants immediately when only a new test was added to kill a survivor.
- **Expected gain:** Near-instant (sub-second) re-runs when iterating on survivors, and 5×–50× faster CI runs across commits when combined with cache persistence. (0 gain on a cold single run.)
- **Cost & risks:**
  - Cache invalidation bugs (subtle transitive dependency changes, testdata files, env vars) can produce stale verdicts — must hash full package closure or use Go's own `ActionID` / `go list -export` build IDs!
  - Orthogonal to cold-run corpus measurements, so harder to showcase in `measurements.py` without replaying a git history of commits.
- **How to measure:**
  - Replay 20 consecutive commits of `semver` or `go-humanize` with and without cache; measure total wall time and verify 100% verdict agreement with cold runs on each commit.

---

### Idea 6 (Detection): Go-Specific Concurrency & Error-Wrapping Operators

- **Claim & source:** Concurrency mutation operators (Bradbury et al.; `go-mutesting`'s `MutatorGoroutineRemove`; Gligoric et al. for concurrent Java) and Go-specific idioms (`defer` deletion, `%w`→`%v` in `fmt.Errorf`, `select` case deletion, `sync.Mutex` `Unlock` deletion) target bug classes absent from classical ROR/COR/AOR/SDL.
- **Why it might hold for Go (and why it did NOT show up in §10/§14):**
  - The 6 libraries in `data-faults/` (`glob`, `go-humanize`, `go-version`, `semver`, `shlex`, `uuid`) and 5 held-out libraries (`blang/semver`, `cast`, `mapstructure`, `mux`, `strcase`) are overwhelmingly **synchronous, single-threaded parsing/formatting libraries**.
  - On service/concurrent Go code (`yamux`, `cron`, `retryablehttp`, `etcd`, `nats`, `grpc-go`), `defer` (resource leaks/unlocks), `go` (async execution), `select` (cancellation/timeouts), and `fmt.Errorf("%w", err)` (error wrapping for `errors.Is`/`errors.As`) are core control flow.
  - Note: `SDL` in `mutant.go:706` currently deletes `*ast.ExprStmt`, `*ast.IncDecStmt`, and non-`:=` `*ast.AssignStmt` — it does **NOT** delete `*ast.DeferStmt` or `*ast.GoStmt` or `*ast.SendStmt`!
- **Expected gain:** Higher coupling on **concurrent / service** real faults, with minimal mutant count increase (few `defer`/`go`/`%w` sites per package).
- **Cost & risks:**
  - High risk of timeouts/deadlocks/flakiness (e.g., deleting `defer mu.Unlock()` deadlocks subsequent tests; removing `go` blocks forever on unbuffered channel sends) and equivalent mutants (`defer f.Close()` in short tests).
  - **Strict requirement:** Must NOT be justified on the 6 libraries; must mine a new held-out fault set from concurrent Go repos (e.g. `yamux`, `cron`, `retryablehttp`, `errgroup`, `singleflight`, `hashicorp/memberlist`) using `cmd/faults` and check coupling on held-out faults + survivor usefulness via blind labeling.
- **How to measure:**
  - Mine faults from 4–6 concurrent/service Go repos (split into dev + held-out, or evaluate directly on held-out without deriving from them).
  - Measure: (a) additional held-out faults coupled on fix lines, (b) % of new mutants that time out or survive as unproductive/equivalent.
  - **Win criterion:** ≥ 3 additional held-out faults coupled with < 30% timeout/unproductive rate on survivors.

---

### Idea 7 (Usefulness): Survivor Ranking (MuRS / Kaufman et al.)

- **Claim & source:** Kaufman et al. (ICSE 2022, *Prioritizing Mutants to Guide Mutation Testing*) and **MuRS** (ESEC/FSE 2023, Google A/B test) show that ranking surviving mutants by predicted usefulness / non-equivalence dramatically improves developer action rate when a suite has dozens of survivors.
- **Why it holds for Go and this corpus:**
  - `cmd/mutant` currently sorts survivors by `(verdict, file, line)` (`report.go:121`).
  - In §11 (`labels/sample.ndjson`), **46% of ROR survivors are equivalent** vs 29–33% for SDL, COR, and AOR; and boundary ROR (`>` → `>=`) is far more often useful than non-boundary ROR (`<` → `!=`).
  - Furthermore, if a line has multiple mutants (in `-all` mode) and *none* were killed, that line is a much stronger test gap than a line where 4 of 5 mutants were killed and only 1 boundary mutant survived (correlating with §4).
- **Expected gain:** Better top-10 precision for developers triaging a large survivor list, at zero execution cost.
- **Cost & risks:**
  - Very small (~30 lines in `report.go`), but 50 labeled survivors is a small validation set; hard to prove statistical significance without labeling more survivors.
- **How to measure:**
  - Compute Precision@10 and NDCG on the 50 library survivors (§11) and 21 untagged service survivors (§13) using a fixed, prior-motivated heuristic (no fitting to the 50 labels).

---

### Idea 8 (Considered & Not Recommended): Predictive Mutation Testing (PMT)

- **Claim & source:** Zhang et al. (2018), Mao et al. (ICST 2019), and MutationBERT (2023–2024) predict mutant kills using ML classifiers on static/dynamic features for 15×–150× speedups.
- **Why not for `mutant`:**
  1. `mutant`'s core design (`ROADMAP.md` "Not planned") is that **surviving diffs are the product**, not an approximate mutation score. A false-kill prediction silently hides a real test gap; a false-survivor prediction wastes developer time.
  2. Cross-project PMT accuracy degrades significantly on unseen projects (Mao et al. 2019) and requires training data far larger than this 6-library corpus.
  3. Exact speedups (Ideas 1–3: batching + adaptive ordering + timeout capping) can deliver 3×–10× speedups **without** sacrificing exactness.

---

## 4. Recommended Plan for Phase 2

I recommend building and measuring **two high-signal, orthogonal improvements** (each in its own small PR / commit sequence on a branch, tests first):

1. **PR 1 (Speed — Libraries & Services): Batched fast-test execution + Site-first online `p/t` test ordering in `FailFast` mode (Ideas 1 + 2).**
   - Attacks the #1 remaining bottleneck after schemata + selection (process spawn overhead on fast tests + suboptimal test ordering on killed mutants).
   - 100% exact verdicts (zero approximation), clean differential verification against the stored `data/` and `data-select/` kill matrices, and measurable with repeated runs at `-j 8` in **§15**.
2. **PR 2 (Choose one based on your priority):**
   - **Option A (Speed — Services):** Timeout-capped FailFast + Adaptive `Confirm` (Idea 3), targeting `yamux`'s 1+ hour runtime (where 51.4% of CPU time is 2nd+ timeouts) and making `Confirm` cheap enough for `cmd/mutant` by default.
   - **Option B (Detection / Noise):** Cheap static equivalence rules (`for i := 0; i < len(x); i++` → `!=`, post-guard `return err`) + Go TCE (`-gcflags=-S`) benchmark on the 12 unaddressed §11 equivalents (Idea 4).

Ready for your review and choice before starting Phase 2.
