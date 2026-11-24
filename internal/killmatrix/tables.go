package killmatrix

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/ibrhmkoz/allele/pkg/ndjson"
)

var tables = [3]string{"mutants.ndjson", "tests.ndjson", "results.ndjson"}

// Write writes m to dir as mutants.ndjson, tests.ndjson and
// results.ndjson, ready for DuckDB.
func (m *Matrix) Write(dir string) error {
	return errors.Join(
		os.MkdirAll(dir, 0o755),
		ndjson.WriteFile(filepath.Join(dir, tables[0]), m.Mutants),
		ndjson.WriteFile(filepath.Join(dir, tables[1]), m.Tests),
		ndjson.WriteFile(filepath.Join(dir, tables[2]), m.Results),
	)
}

// Read reads a matrix Write wrote to dir, each table gzipped or not.
func Read(dir string) (*Matrix, error) {
	mutants, errM := ndjson.ReadFile[Mutant](filepath.Join(dir, tables[0]))
	tests, errT := ndjson.ReadFile[Test](filepath.Join(dir, tables[1]))
	results, errR := ndjson.ReadFile[Result](filepath.Join(dir, tables[2]))
	if err := errors.Join(errM, errT, errR); err != nil {
		return nil, err
	}
	return &Matrix{Mutants: mutants, Tests: tests, Results: results}, nil
}
