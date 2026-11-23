package main

import (
	"fmt"
	"slices"
	"testing"

	"github.com/ibrhmkoz/go-kit/xtesting"
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
	if parseTestJSON([]byte(`{"Action":"output","Output":"# x [build failed]"}`)).built {
		t.Error("a run that reported no test counts as built")
	}
}
