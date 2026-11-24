// Package ndjson reads and writes newline-delimited JSON (NDJSON) streams and
// files, transparently compressing and decompressing .gz files.
package ndjson

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
)

// Encode writes each element of rows to w as one JSON line, without HTML
// escaping.
func Encode[T any](w io.Writer, rows []T) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	return nil
}

func Decode[T any](r io.Reader) ([]T, error) {
	dec := json.NewDecoder(bufio.NewReader(r))
	var rows []T
	for {
		var row T
		err := dec.Decode(&row)
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
}

// WriteFile writes rows to path as NDJSON, gzipping the output when path ends
// in ".gz". Encoding happens in memory first so a marshal error leaves path
// untouched.
func WriteFile[T any](path string, rows []T) error {
	var b bytes.Buffer
	var w io.Writer = &b
	var z *gzip.Writer
	if strings.HasSuffix(path, ".gz") {
		z = gzip.NewWriter(&b)
		w = z
	}
	if err := Encode(w, rows); err != nil {
		return err
	}
	if z != nil {
		_ = z.Close() // bytes.Buffer.Write never fails
	}
	return os.WriteFile(path, b.Bytes(), 0o644)
}

// ReadFile reads NDJSON rows from path. When path does not exist and does not
// end in ".gz", ReadFile falls back to path+".gz". Files ending in ".gz" are
// gzip-decompressed.
func ReadFile[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) && !strings.HasSuffix(path, ".gz") {
		path += ".gz"
		f, err = os.Open(path)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() //allele:ignore a leaked descriptor changes no result
	var src io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		z, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer func() { _ = z.Close() }() //allele:ignore a leaked gzip reader changes no result
		src = z
	}
	return Decode[T](src)
}
