package killmatrix

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var tables = [3]string{"mutants.ndjson", "tests.ndjson", "results.ndjson"}

// Write writes m to dir as mutants.ndjson, tests.ndjson and
// results.ndjson, ready for DuckDB.
func (m *Matrix) Write(dir string) error {
	return errors.Join(
		os.MkdirAll(dir, 0o755),
		writeRows(filepath.Join(dir, tables[0]), m.Mutants),
		writeRows(filepath.Join(dir, tables[1]), m.Tests),
		writeRows(filepath.Join(dir, tables[2]), m.Results),
	)
}

// Read reads a matrix Write wrote to dir, each table gzipped or not.
func Read(dir string) (*Matrix, error) {
	var m Matrix
	if err := errors.Join(
		readRows(dir, tables[0], &m.Mutants),
		readRows(dir, tables[1], &m.Tests),
		readRows(dir, tables[2], &m.Results),
	); err != nil {
		return nil, err
	}
	return &m, nil
}

func writeRows[T any](name string, rows []T) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	return os.WriteFile(name, b.Bytes(), 0o644)
}

func readRows[T any](dir, table string, rows *[]T) error {
	name := filepath.Join(dir, table)
	f, err := os.Open(name)
	if errors.Is(err, os.ErrNotExist) {
		name += ".gz"
		f, err = os.Open(name)
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() //allele:ignore a leaked descriptor changes no result
	var src io.Reader = f
	if strings.HasSuffix(name, ".gz") {
		z, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		src = z
	}
	dec := json.NewDecoder(bufio.NewReader(src))
	for dec.More() {
		var row T
		if err := dec.Decode(&row); err != nil {
			return err
		}
		*rows = append(*rows, row)
	}
	return nil
}
