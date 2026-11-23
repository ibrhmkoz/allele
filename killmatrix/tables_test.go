package killmatrix_test

import (
	"compress/gzip"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
)

// TestReadWrite: Read gives back what Write wrote, from plain or gzipped
// tables, so stored matrices of real modules can be read back as matrices.
func TestReadWrite(t *testing.T) {
	t.Parallel()
	m := &killmatrix.Matrix{
		Mutants: []killmatrix.Mutant{
			{ID: 1, Pkg: "p", File: "p.go", Line: 3, Col: 5, Operator: "ROR", Original: "a > b", Mutated: "a >= b", Compiled: true, Schema: true, BuildMS: 1.5},
			{ID: 2, Pkg: "p", File: "p.go", Line: 4, Col: 2, Operator: "SDL", Original: "x++", Equivalent: "rule", Arid: "logging"},
		},
		Tests:   []killmatrix.Test{{ID: 1, Pkg: "p", Name: "TestP/a", BaselineMS: 2, State: killmatrix.OK}, {ID: 2, Pkg: "p", Name: "TestQ", State: killmatrix.Flaky}},
		Results: []killmatrix.Result{{MutantID: 1, TestID: 1, Status: killmatrix.Killed, MS: 3}},
	}
	dir := t.TempDir()
	if err := m.Write(dir); err != nil {
		t.Fatal(err)
	}
	got, err := killmatrix.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, m) {
		t.Fatalf("read back %+v, wrote %+v", got, m)
	}
	gz := t.TempDir()
	for _, name := range []string{"mutants.ndjson", "tests.ndjson", "results.ndjson"} {
		gzipFile(t, filepath.Join(dir, name), filepath.Join(gz, name+".gz"))
	}
	if got, err = killmatrix.Read(gz); err != nil || !reflect.DeepEqual(got, m) {
		t.Fatalf("read gzipped back %+v, %v", got, err)
	}
}

// TestReadWriteErrors: a table that cannot be written or read is an error,
// never a matrix with rows missing.
func TestReadWriteErrors(t *testing.T) {
	t.Parallel()
	m := &killmatrix.Matrix{Tests: []killmatrix.Test{{ID: 1, Name: "T"}}}
	file := filepath.Join(t.TempDir(), "file")
	write(t, file, "")
	if err := m.Write(file); err == nil {
		t.Error("no error writing into a file")
	}
	taken := t.TempDir()
	if err := os.Mkdir(filepath.Join(taken, "tests.ndjson"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := m.Write(taken); err == nil {
		t.Error("no error writing over a directory")
	}
	nan := &killmatrix.Matrix{Tests: []killmatrix.Test{{ID: 1, BaselineMS: math.NaN()}}}
	if err := nan.Write(t.TempDir()); err == nil {
		t.Error("no error writing a time JSON cannot hold")
	}
	for name, tables := range map[string]map[string]string{
		"no tables":     {},
		"broken JSON":   {"mutants.ndjson": "{", "tests.ndjson": "", "results.ndjson": ""},
		"broken gzip":   {"mutants.ndjson.gz": "not gzip", "tests.ndjson": "", "results.ndjson": ""},
		"missing table": {"mutants.ndjson": "", "tests.ndjson": ""},
	} {
		dir := t.TempDir()
		for file, content := range tables {
			write(t, filepath.Join(dir, file), content)
		}
		if _, err := killmatrix.Read(dir); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	locked := t.TempDir()
	for _, name := range []string{"mutants.ndjson", "tests.ndjson", "results.ndjson"} {
		write(t, filepath.Join(locked, name), "")
	}
	if err := os.Chmod(filepath.Join(locked, "tests.ndjson"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := killmatrix.Read(locked); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("a table that cannot be opened: %v", err)
	}
}

func write(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func gzipFile(t *testing.T, from, to string) {
	t.Helper()
	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(to)
	if err != nil {
		t.Fatal(err)
	}
	z := gzip.NewWriter(f)
	if _, err := z.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
