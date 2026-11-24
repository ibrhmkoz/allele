package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

var hunk = regexp.MustCompile(`(?m)^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// changedLines returns the lines of the new file that a unified diff with
// no context (git diff -U0) adds or changes. A hunk that only deletes marks
// the lines on both sides of the gap it leaves.
func changedLines(diff []byte) map[int]bool {
	lines := map[int]bool{}
	for _, m := range hunk.FindAllSubmatch(diff, -1) {
		start, _ := strconv.Atoi(string(m[1]))
		n := 1
		if len(m[2]) > 0 {
			n, _ = strconv.Atoi(string(m[2]))
		}
		if n == 0 {
			lines[start], lines[start+1] = true, true
			continue
		}
		for l := start; l < start+n; l++ {
			lines[l] = true
		}
	}
	return lines
}

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
