package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrhmkoz/allele/internal/xtesting"
)

func TestRevealed(t *testing.T) {
	var b strings.Builder
	for _, c := range [][2]int{{0, 0}, {3, 0}, {3, 1}, {5, 1}, {6, 1}, {10, 1}, {10, 2}, {100, 1}} {
		fmt.Fprintf(&b, "%d survivors, %d coupled: %.4f\n", c[0], c[1], revealed(c[0], c[1], 5))
	}
	// Reading 5 of 10 with one coupled finds it half the time; of 6, 5/6.
	xtesting.EqualText(t, b.String(), `
		0 survivors, 0 coupled: 0.0000
		3 survivors, 0 coupled: 0.0000
		3 survivors, 1 coupled: 1.0000
		5 survivors, 1 coupled: 1.0000
		6 survivors, 1 coupled: 0.8333
		10 survivors, 1 coupled: 0.5000
		10 survivors, 2 coupled: 0.7778
		100 survivors, 1 coupled: 0.0500
	`)
}

// writeDetail writes cmd/faults -detail rows, gzipped when name says so.
func writeDetail(t *testing.T, name string, rows []coupledRow) {
	t.Helper()
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	var w interface {
		Write([]byte) (int, error)
	} = f
	var z *gzip.Writer
	if strings.HasSuffix(name, ".gz") {
		z = gzip.NewWriter(f)
		w = z
	}
	enc := json.NewEncoder(w)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			t.Fatal(err)
		}
	}
	if z != nil {
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestReadFaults: a tree becomes a fault when detail names coupled
// mutants for its commit and package; plain or gzipped detail alike.
func TestReadFaults(t *testing.T) {
	dir := t.TempDir()
	rows := []coupledRow{
		{Repo: "r", Commit: "aaaaaaaaaaaaaaaa", Pkg: ".", File: "p.go", Line: 3, Col: 5, Operator: "ROR", Original: "a > b", Mutated: "a >= b", Coupled: true},
		{Repo: "r", Commit: "aaaaaaaaaaaaaaaa", Pkg: ".", File: "p.go", Line: 4, Col: 5, Operator: "ROR", Original: "a > b", Mutated: "a < b"},
		{Repo: "r", Commit: "bbbbbbbbbbbbbbbb", Pkg: "sub", File: "s.go", Line: 7, Col: 2, Operator: "SDL", Original: "x++", Coupled: true},
		{Repo: "r", Commit: "cccccccccccccccc", Pkg: ".", File: "p.go", Line: 1, Col: 1, Operator: "ROR", Original: strings.Repeat("a", 100_000), Mutated: "b"},
	}
	trees := filepath.Join(dir, "trees.txt")
	writeFile(t, trees, "r . /x/r-aaaaaaaaaaaa\nr sub /x/r-bbbbbbbbbbbb\nr . /x/r-cccccccccccc\nnot a tree line\ntoo short\n")
	for _, name := range []string{"detail.ndjson", "detail.ndjson.gz"} {
		detail := filepath.Join(dir, name)
		writeDetail(t, detail, rows)
		faults, err := readFaults(trees, detail)
		if err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		for _, f := range faults {
			fmt.Fprintf(&b, "%s %s %d\n", f, f.tree, len(f.coupled))
		}
		xtesting.EqualText(t, b.String(), `
			r-aaaaaaaaaaaa/. /x/r-aaaaaaaaaaaa 1
			r-bbbbbbbbbbbb/sub /x/r-bbbbbbbbbbbb 1
		`)
		if !faults[0].coupled[toolKey(verdict{File: "p.go", Line: 3, Col: 5, Operator: "ROR", Original: "a > b", Mutated: "a >= b"})] {
			t.Error("p.go:3 not coupled by the key a build reports it under")
		}
	}
	writeFile(t, filepath.Join(dir, "bad.ndjson"), "{")
	writeFile(t, filepath.Join(dir, "huge.ndjson"), strings.Repeat(" ", 2<<20))
	writeFile(t, filepath.Join(dir, "bad.ndjson.gz"), "not gzip")
	for _, c := range [][2]string{{trees, filepath.Join(dir, "none")}, {filepath.Join(dir, "none"), filepath.Join(dir, "detail.ndjson")}, {trees, filepath.Join(dir, "bad.ndjson")}, {trees, filepath.Join(dir, "bad.ndjson.gz")}, {trees, filepath.Join(dir, "huge.ndjson")}} {
		if _, err := readFaults(c[0], c[1]); err == nil {
			t.Errorf("readFaults(%s, %s): no error", filepath.Base(c[0]), filepath.Base(c[1]))
		}
	}
}

func TestNoFaults(t *testing.T) {
	if r := combine("x", []score{{D: 1, Stable: 1}}, 1).R; r != -1 {
		t.Errorf("R without faults = %v, want -1", r)
	}
}
