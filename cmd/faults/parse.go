package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

type testRun struct {
	built  bool
	passed map[string]bool // every test reported, at any depth; skipped counts as passed
}

func parseTestJSON(out []byte) testRun {
	r := testRun{passed: map[string]bool{}}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var e struct{ Action, Test string }
		if json.Unmarshal(sc.Bytes(), &e) != nil || e.Test == "" {
			continue
		}
		r.built = true
		switch e.Action {
		case "pass", "skip":
			r.passed[e.Test] = true
		case "fail":
			r.passed[e.Test] = false
		}
	}
	return r
}

// revealing returns the leaf tests that pass on the fixed code and fail on
// the buggy one. A leaf missing from the buggy run counts as failing when
// its top-level test failed: a panic ends the binary before siblings report.
func revealing(fixed, buggy testRun) []string {
	var names []string
	for name, ok := range fixed.passed {
		if !ok || isParent(name, fixed.passed) {
			continue
		}
		top, _, _ := strings.Cut(name, "/")
		p, seen := buggy.passed[name]
		topPassed, topSeen := buggy.passed[top]
		if seen && !p || !seen && topSeen && !topPassed {
			names = append(names, name)
		}
	}
	return names
}

func isParent(name string, all map[string]bool) bool {
	for o := range all {
		if strings.HasPrefix(o, name+"/") {
			return true
		}
	}
	return false
}
