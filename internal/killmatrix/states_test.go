package killmatrix

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ibrhmkoz/allele/pkg/gotest"
	"github.com/ibrhmkoz/allele/pkg/xtesting"
)

func out(names ...string) []byte {
	var b strings.Builder
	for _, n := range names {
		verdict := "PASS"
		if after, ok := strings.CutPrefix(n, "!"); ok {
			n, verdict = after, "FAIL"
		}
		fmt.Fprintf(&b, "\x16--- %s: %s (0.00s)\n", verdict, n)
	}
	return []byte(b.String())
}

func showStates(tests []Test) string {
	var b strings.Builder
	for _, t := range tests {
		fmt.Fprintf(&b, "%s %s\n", t.Name, t.State)
	}
	return b.String()
}

func TestStates(t *testing.T) {
	steady := gotest.Parse(out("TestA/x", "TestA/y", "TestA"), true, false)
	shaky := gotest.Parse(out("TestA/x", "!TestA/y", "!TestA"), false, false)
	xtesting.EqualText(t, showStates(states("TestA", []gotest.Report{steady, steady})), "TestA/x ok\nTestA/y ok\n")
	xtesting.EqualText(t, showStates(states("TestA", []gotest.Report{steady, shaky})), "TestA/x ok\nTestA/y flaky\n")
	xtesting.EqualText(t, showStates(states("TestA", []gotest.Report{shaky, shaky})), "TestA/x ok\nTestA/y failing\n")
	// A run that reported nothing stands for the top-level test: passed if
	// it exited 0, failed if not.
	silent, crashed := gotest.Parse(nil, true, false), gotest.Parse(nil, false, false)
	xtesting.EqualText(t, showStates(states("TestA", []gotest.Report{silent, silent})), "TestA ok\n")
	xtesting.EqualText(t, showStates(states("TestA", []gotest.Report{silent, crashed})), "TestA flaky\n")
	// A leaf one run missed did not pass in it.
	xtesting.EqualText(t, showStates(states("TestA", []gotest.Report{steady, silent})), "TestA/x flaky\nTestA/y flaky\n")
}

func TestTopFailed(t *testing.T) {
	g := topGroup{name: "TestA", leaves: []Test{{Name: "TestA/x"}, {Name: "TestA/y"}}}
	for _, c := range []struct {
		out  []byte
		want bool
	}{
		{out("TestA/x", "TestA/y", "TestA"), false},
		{out("TestA/x", "!TestA/y"), true},
		{out("!TestA"), true},
		{out("TestB", "!TestC"), false},
		{nil, false},
	} {
		if got := topFailed(g, gotest.Parse(c.out, false, false)); got != c.want {
			t.Errorf("%q: topFailed %v, want %v", c.out, got, c.want)
		}
	}
}
