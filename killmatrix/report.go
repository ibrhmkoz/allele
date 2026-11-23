package killmatrix

import (
	"bufio"
	"bytes"
	"strings"
	"time"
)

// report is what one run of a top-level test showed.
type report struct {
	ok, timedOut bool
	elapsed      time.Duration
	// passed maps each test the run reported, at any depth, to whether it
	// passed. A skipped test passed: it did not fail.
	passed map[string]bool
	// started records every test name that printed "=== RUN".
	started map[string]bool
	// order lists the names in passed as the run reported them.
	order []string
}

// status is how leaf did in the run. A leaf the run did not report takes
// the outcome of the whole run: a panic in one subtest ends the binary
// before its siblings report, and a mutant that renames subtests makes the
// leaves it hid fail the run.
func (r report) status(leaf string) Status {
	if passed, ok := r.passed[leaf]; ok {
		if passed {
			return Survived
		}
		return Killed
	}
	switch {
	case r.timedOut:
		return Timeout
	case r.ok:
		return Survived
	default:
		return Killed
	}
}

// parse reads the result lines of -test.v=test2json output into r. They
// start with ^V, which test output cannot fake by printing "--- FAIL".
func (r *report) parse(out []byte) report {
	r.passed = map[string]bool{}
	r.started = map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if name, ok := strings.CutPrefix(line, "\x16=== RUN   "); ok {
			if name = strings.TrimSpace(name); name != "" {
				r.started[name] = true
			}
			continue
		}
		line, ok := strings.CutPrefix(line, "\x16--- ")
		if !ok {
			continue
		}
		verdict, rest, ok := strings.Cut(line, ": ")
		name, _, _ := strings.Cut(rest, " (")
		if !ok || name == "" {
			continue
		}
		if verdict != "PASS" && verdict != "SKIP" && verdict != "FAIL" {
			continue
		}
		if _, seen := r.passed[name]; !seen {
			r.order = append(r.order, name)
		}
		r.passed[name] = verdict != "FAIL"
	}
	return *r
}

// leavesOf returns the names no other name extends with a subtest, in the
// order given.
func leavesOf(names []string) []string {
	var leaves []string
	for _, n := range names {
		parent := false
		for _, o := range names {
			if strings.HasPrefix(o, n+"/") {
				parent = true
				break
			}
		}
		if !parent {
			leaves = append(leaves, n)
		}
	}
	return leaves
}

func topOf(name string) string {
	top, _, _ := strings.Cut(name, "/")
	return top
}
