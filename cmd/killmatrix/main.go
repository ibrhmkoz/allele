// Command killmatrix computes the full kill matrix of Go packages and writes
// it as mutants.ndjson, tests.ndjson and results.ndjson, ready for DuckDB:
//
//	killmatrix -o out ./...
//	duckdb -c "select status, count(*) from 'out/results.ndjson' group by all"
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/ibrhmkoz/allele/killmatrix"
)

func main() {
	out := flag.String("o", "killmatrix", "directory to write the tables to")
	var r killmatrix.Runner
	flag.IntVar(&r.Workers, "j", 1, "mutants to build and run at once; above 1, times in the matrix include contention")
	flag.IntVar(&r.BaselineRuns, "runs", 3, "runs per test on the unmutated package, to find flaky tests")
	flag.Float64Var(&r.TimeoutFactor, "timeout-factor", 3, "a run times out after baseline × factor + pad")
	flag.DurationVar(&r.TimeoutPad, "timeout-pad", 1e9, "a run times out after baseline × factor + pad")
	flag.BoolVar(&r.Schemata, "schemata", false, "build each package once with its mutants embedded, switched on per run")
	flag.BoolVar(&r.Confirm, "confirm", false, "run a failing test again and count a kill only if it fails twice")
	flag.BoolVar(&r.FailFast, "failfast", false, "stop a mutant's tests at its first kill: verdicts, not the full matrix")
	flag.BoolVar(&r.Select, "select", false, "with -schemata, run a test against a mutant only if the test reached it in the baseline")
	flag.Parse()
	r.Log = os.Stderr

	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, r, *out, patterns); err != nil {
		fmt.Fprintln(os.Stderr, "killmatrix:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, r killmatrix.Runner, out string, patterns []string) error {
	m, err := r.Run(ctx, ".", patterns...)
	if err != nil {
		return err
	}
	if err := m.Write(out); err != nil {
		return err
	}
	fmt.Print(m)
	return nil
}
