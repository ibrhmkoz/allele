package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ibrhmkoz/allele/pkg/xtesting"
)

func TestChangedLines(t *testing.T) {
	diff := []byte(`diff --git a/version.go b/version.go
@@ -10 +10 @@ func Parse
-	if n > 0 {
+	if n >= 0 {
@@ -20,0 +21,2 @@ func Parse
+	a := 1
+	b := 2
@@ -40,3 +42,0 @@ func Compare
-	x
-	y
-	z
`)
	lines := changedLines(diff)
	var got []int
	for l := range lines {
		got = append(got, l)
	}
	slices.Sort(got)
	xtesting.EqualText(t, fmt.Sprint(got), "[10 21 22 42 43]")
}

func TestRevealing(t *testing.T) {
	fixed := parseTestJSON([]byte(`{"Action":"pass","Test":"TestParse/empty"}
{"Action":"pass","Test":"TestParse/zero"}
{"Action":"pass","Test":"TestParse"}
{"Action":"pass","Test":"TestCompare"}
{"Action":"pass","Test":"TestPanics/first"}
{"Action":"pass","Test":"TestPanics/second"}
{"Action":"pass","Test":"TestPanics"}
`))
	// The buggy code fails TestParse/zero, and panics in TestPanics/first,
	// so TestPanics/second never reports.
	buggy := parseTestJSON([]byte(`{"Action":"output","Output":"ok"}
{"Action":"pass","Test":"TestParse/empty"}
{"Action":"fail","Test":"TestParse/zero"}
{"Action":"fail","Test":"TestParse"}
{"Action":"pass","Test":"TestCompare"}
{"Action":"fail","Test":"TestPanics/first"}
{"Action":"fail","Test":"TestPanics"}
`))
	got := revealing(fixed, buggy)
	slices.Sort(got)
	xtesting.EqualText(t, fmt.Sprint(got), "[TestPanics/first TestPanics/second TestParse/zero]")
	if !fixed.built || !buggy.built {
		t.Error("runs that reported tests must count as built")
	}
	if same := revealing(fixed, fixed); len(same) != 0 {
		t.Errorf("revealing(fixed, fixed) = %v, want empty", same)
	}
	if parseTestJSON([]byte(`{"Action":"output","Output":"# x [build failed]"}`)).built {
		t.Error("a run that reported no test counts as built")
	}
	// Output lines over bufio.Scanner's 64 KiB default token limit must not
	// truncate later test results.
	long := fmt.Sprintf("{\"Action\":\"output\",\"Output\":%q}\n{\"Action\":\"pass\",\"Test\":\"TestAfterLong\"}\n", strings.Repeat("x", 70*1024))
	if r := parseTestJSON([]byte(long)); !r.built || !r.passed["TestAfterLong"] {
		t.Errorf("long line truncated parseTestJSON: built=%v passed=%v", r.built, r.passed)
	}
}

func TestWanted(t *testing.T) {
	var b strings.Builder
	for _, c := range []struct {
		commit   string
		prefixes []string
	}{
		{"198ca931c72a4aae", nil},
		{"198ca931c72a4aae", []string{"198ca931c72a"}},
		{"198ca931c72a4aae", []string{"fa4ea64dd518", "198ca9"}},
		{"198ca931c72a4aae", []string{"fa4ea64dd518"}},
	} {
		fmt.Fprintf(&b, "%s %v: %v\n", c.commit, c.prefixes, wanted(c.commit, c.prefixes))
	}
	xtesting.EqualText(t, b.String(), `
		198ca931c72a4aae []: true
		198ca931c72a4aae [198ca931c72a]: true
		198ca931c72a4aae [fa4ea64dd518 198ca9]: true
		198ca931c72a4aae [fa4ea64dd518]: false
	`)
}
