// Command killmatrix computes the full kill matrix of Go packages and writes
// it as mutants.ndjson, tests.ndjson and results.ndjson, ready for DuckDB:
//
//	killmatrix -o out ./...
//	duckdb -c "select status, count(*) from 'out/results.ndjson' group by all"
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
)

func main() {
	out := flag.String("o", "killmatrix", "directory to write the tables to")
	var r killmatrix.Runner
	flag.IntVar(&r.Workers, "j", 1, "mutants to build and run at once; above 1, times in the matrix include contention")
	flag.IntVar(&r.BaselineRuns, "runs", 3, "runs per test on the unmutated package, to find flaky tests")
	flag.Float64Var(&r.TimeoutFactor, "timeout-factor", 3, "a run times out after baseline × factor + pad")
	flag.DurationVar(&r.TimeoutPad, "timeout-pad", 1e9, "a run times out after baseline × factor + pad")
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
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	if err := write(filepath.Join(out, "mutants.ndjson"), m.Mutants); err != nil {
		return err
	}
	if err := write(filepath.Join(out, "tests.ndjson"), m.Tests); err != nil {
		return err
	}
	if err := write(filepath.Join(out, "results.ndjson"), m.Results); err != nil {
		return err
	}
	fmt.Print(m)
	return nil
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
