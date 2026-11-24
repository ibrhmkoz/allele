package main

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"path"
	"strings"
)

// A fault is a real bug fix's package, prepared by docs/faulttrees.sh as
// the fixed code with the tests from before the fix: the suite that
// missed the bug. Its coupled mutants are those only the fix's new tests
// kill, in the exhaustive matrix cmd/faults -detail computes.
type fault struct {
	name, pkg, tree string
	coupled         map[string]bool // mutant keys
}

// coupledRow is the part of a cmd/faults -detail row R needs.
type coupledRow struct {
	Repo     string `json:"repo"`
	Commit   string `json:"commit"`
	Pkg      string `json:"pkg"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Operator string `json:"operator"`
	Original string `json:"original"`
	Mutated  string `json:"mutated"`
	Coupled  bool   `json:"coupled"`
}

// readFaults pairs the trees listed in trees ("name pkg tree" lines, the
// tree named name-commit) with their coupled mutants from detail.
func readFaults(trees, detail string) ([]fault, error) {
	coupled := map[string]map[string]bool{} // name-commit12/pkg → keys
	f, err := os.Open(detail)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() //allele:ignore a leaked descriptor changes no result
	var src io.Reader = f
	if strings.HasSuffix(detail, ".gz") {
		if src, err = gzip.NewReader(f); err != nil {
			return nil, err
		}
	}
	s := bufio.NewScanner(src)
	s.Buffer(nil, 1<<20)
	for s.Scan() {
		var r coupledRow
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			return nil, err
		}
		if !r.Coupled {
			continue
		}
		id := faultID(r.Repo+"-"+r.Commit[:min(12, len(r.Commit))], r.Pkg)
		if coupled[id] == nil {
			coupled[id] = map[string]bool{}
		}
		coupled[id][key(r.Pkg, r.File, r.Line, r.Col, r.Operator, r.Original, r.Mutated)] = true
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(trees)
	if err != nil {
		return nil, err
	}
	var faults []fault
	for line := range strings.Lines(string(data)) {
		fs := strings.Fields(line)
		if len(fs) != 3 {
			continue
		}
		id := faultID(path.Base(fs[2]), fs[1])
		if c := coupled[id]; len(c) > 0 {
			faults = append(faults, fault{name: path.Base(fs[2]), pkg: fs[1], tree: fs[2], coupled: c})
		}
	}
	return faults, nil
}

func faultID(tree, pkg string) string { return tree + "/" + pkg }

// revealed is R_k for one fault: the chance that a developer reading k of
// the survivors a build reports, at random, reads one coupled to the
// fault (Chekam et al. 2017 compare at a fixed inspection effort). With
// none coupled it is 0; with no more than k reported, all are read.
func revealed(survivors, coupled, k int) float64 {
	switch {
	case coupled == 0:
		return 0
	case survivors <= k:
		return 1
	}
	miss := new(big.Rat).SetFrac(new(big.Int).Binomial(int64(survivors-coupled), int64(k)), new(big.Int).Binomial(int64(survivors), int64(k)))
	m, _ := miss.Float64()
	return 1 - m
}

// faultScore is how one build did on one fault.
type faultScore struct {
	Bin       string  `json:"bin"`
	Fault     string  `json:"fault"`
	Survivors int     `json:"survivors"`
	Coupled   int     `json:"coupled"`
	R         float64 `json:"r"`
}

func (f fault) String() string { return fmt.Sprintf("%s/%s", f.name, f.pkg) }
