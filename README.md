# allele

Mutation testing for Go. `allele` makes small changes to your code (`>` to `>=`, `&&` to `||`, deleting a line, `return err` to `return nil`) and runs your tests. If tests still pass, you have a test gap.

Output shows each gap as a diff, not a score:

```
$ allele ./pkg/xtesting ./pkg/unidiff ./pkg/adequacy ./pkg/ndjson
survived  pkg/xtesting/xtesting.go:112  ROR
  -		if strings.TrimSpace(l) == "" {
  +		if strings.TrimSpace(l) < "" {

survived  pkg/xtesting/xtesting.go:113  SDL
  -			lines[i] = ""
  +			(deleted)

114 mutants: 105 killed, 5 survived, 0 not covered, 4 timed out, 0 did not build
in 8.7s
```

In the gap above, deleting `lines[i] = ""` in `dedent` passed every test. No test passes a whitespace-only blank line to `dedent`.

## Install

Requires Go 1.27 or later on your machine to build and run tests.

```sh
# Homebrew (macOS, Linux)
brew install ibrhmkoz/tap/allele

# Go
go install github.com/ibrhmkoz/allele/cmd/allele@latest
```

Prebuilt binaries for macOS, Linux, and Windows are on [Releases](https://github.com/ibrhmkoz/allele/releases). Run `allele -version` to verify.

## Usage

| Command | What it does |
|---|---|
| `allele ./...` | Test every package, one mutant per line |
| `allele -diff main ./...` | Only mutate lines changed since `main` (includes uncommitted edits) |
| `allele -all ./...` | Run every mutant on a line, not just one |
| `allele -arid ./...` | Include logging, metrics, capacity hints, and ignored lines |
| `allele -strict ./...` | Exit 1 if any mutant survives or is not covered (for CI) |
| `allele -json ./...` | Print one JSON object per mutant, all verdicts included |
| `allele -no-cache ./...` | Skip reading and writing the verdict cache |
| `allele -j 4 -v ./...` | Run 4 mutants in parallel, log progress to stderr |

**Verdicts:**
- **killed** — a test failed (good)
- **survived** — all tests passed (test gap)
- **not covered** — no test reached the function
- **timeout** — test ran longer than 3× normal time + 100 ms
- **build error** — mutated code failed to compile

Skip a line with `//allele:ignore` at the end of the line or on the line above.

## Benchmarks

Tested on 61 real bug fixes from Go projects. Each tool ran on the fixed code with the old tests that missed the bug (`-j 8`, same machine). A tool "finds" a bug if a mutant survives on a line the fix touched.

| Tool | Bugs found | Survivors per bug | Time (s) |
|---|---|---|---|
| `allele` | 43 / 61 | 2.2 | 181 |
| `allele -all` | 48 / 61 | 6.2 | 384 |
| Gremlins 0.6.0 | 26 / 61 | 1.2 | 1349 |
| Gremlins (all ops) | 21 / 61 | 1.2 | 1445 |

Across 8 Go libraries, `allele` ran 1.6× to 14× faster than Gremlins. `go-mutesting` builds one mutant at a time (86s vs 1s on `shlex`). Full details are in [ROADMAP.md](ROADMAP.md).

## How it works

Defaults come from measurements on 10 libraries and 3 services (`make measurements` runs [`docs/measurements.py`](docs/measurements.py)).

- **5 operators:** relational (`ROR`), boolean/if (`COR`), math (`AOR`), statement deletion (`SDL`), and error handling (`ERR`).
- **One build per package:** All mutants compile into one test binary, toggled at runtime via the `ALLELE` env var. Build time across the test suite dropped from 47 min to 0.9 min.
- **Targeted tests:** A baseline run tracks which tests reach a function and change a mutant's value. Only those tests run, fastest first, stopping at the first kill.
- **One mutant per line:** Picks the mutant fewest tests affect (or an unreached one). Finds more bugs with less noise than running every mutant.
- **Noise skipped:** Ignores equivalent mutants (like `len(s) == 0` → `<= 0`) and arid code (logging, metrics, tracing, capacity hints, `//allele:ignore`).
- **Flake check:** A kill counts only if the test passes on original code and fails twice on the mutant.
- **Build repair:** Adds `_ = x` when deleting a line leaves an unused variable. Cut build errors from 249 to 39.

## Project layout

`pkg/` holds reusable packages. `internal/` is `allele` itself.

| Package | Role |
|---|---|
| `pkg/gotest` | Builds and runs Go test binaries with timeouts and clean teardown |
| `pkg/adequacy` | Computes dominator mutants, scores, and bug links over kill matrices |
| `pkg/fingerprint` | Hashes a package's files, embeds, `testdata/`, and deps for caching |
| `pkg/xtesting` | Text comparison helpers with readable diffs |
| `internal/mutant` | Finds mutants (`Find`), tags equivalent/arid ones, embeds them (`Embed`) |
| `internal/killmatrix` | Runs tests against mutants and records verdicts or kill matrices |
| `cmd/allele` | CLI |
| `cmd/bench` | Scores `allele` builds (see [ROADMAP.md](ROADMAP.md)) |
| `cmd/killmatrix`, `cmd/simulate`, `cmd/faults` | Measurement tools behind `docs/` |

## Dataset

[`docs/`](docs/) contains the raw benchmark data: full kill matrices for 10 libraries, verdicts for 3 services, 79 real bugs, and 92 hand-labelled survivors. See [`docs/DATA.md`](docs/DATA.md) for the schema.

## Limits

- **Go version:** Requires Go 1.27+. Modules older than Go 1.21 get a `1.21` build tag on mutated files to keep old loop semantics.
- **Single-build fallback:** Packages that `os` or `strconv` depend on, plus `const` and array-size mutants, build one mutant at a time.
- **Cache:** Verdicts are cached in `os.UserCacheDir()/allele`. Keys include Go version/OS/arch, flags, package files, `//go:embed`, `testdata/`, tests, and non-stdlib deps. Env vars, external services (like databases), and files outside `testdata/` are not tracked—pass `-no-cache` when those matter.

## License

Code: [MIT](LICENSE). Data in `docs/`: [CC BY 4.0](docs/LICENSE-DATA) (upstream licenses in [`docs/licenses/`](docs/licenses)).

Open work is tracked in [ROADMAP.md](ROADMAP.md).
