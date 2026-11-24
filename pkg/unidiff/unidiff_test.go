package unidiff_test

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/ibrhmkoz/allele/pkg/unidiff"
	"github.com/ibrhmkoz/allele/pkg/xtesting"
)

func TestParse(t *testing.T) {
	raw := []byte(`diff --git a/p/version.go b/p/version.go
--- a/p/version.go
+++ b/p/version.go
@@ -4 +4 @@ func tooLong(v string) bool {
-	if len(v) > 128 {
+	if len(v) > 256 {
@@ -9,2 +9,0 @@ func short() {
diff --git a/gone.go b/gone.go
--- a/gone.go
+++ /dev/null
@@ -1,3 +0,0 @@
diff --git a/a/init.go b/a/init.go
--- /dev/null
+++ b/a/init.go
@@ -0,0 +1,3 @@
+package a
+
+var x = 1
`)
	d := unidiff.Parse(raw)
	xtesting.Prints(t, d, `
		a/init.go: 1, 2, 3
		p/version.go: 4, 9, 10
	`)

	if !d.Has("p/version.go", 4) || !d.Has("p/version.go", 9) || !d.Has("p/version.go", 10) {
		t.Errorf("Has missed touched lines in p/version.go: %s", d)
	}
	if d.Has("p/version.go", 5) || d.Has("gone.go", 1) || d.Has("missing.go", 1) {
		t.Errorf("Has matched untouched or deleted line: %s", d)
	}
	xtesting.EqualText(t, fmt.Sprint(d.Files()), "[a/init.go p/version.go]")
	xtesting.EqualText(t, fmt.Sprint(d.Lines("p/version.go")), "[4 9 10]")
	xtesting.EqualText(t, fmt.Sprint(d.Lines("missing.go")), "[]")
}

func TestParseEdgeCases(t *testing.T) {
	// Empty input and hunk before any +++ header produce an empty diff.
	xtesting.Prints(t, unidiff.Parse(nil), "")
	xtesting.Prints(t, unidiff.Parse([]byte("@@ -1 +1 @@\n+x\n")), "")

	// A line longer than bufio.Scanner's 64 KiB default token limit must not
	// stop later hunks from being parsed.
	long := fmt.Sprintf("diff --git a/p.go b/p.go\n--- a/p.go\n+++ b/p.go\n@@ -1 +1 @@\n+%s\n@@ -10 +10 @@\n+x\n", strings.Repeat("x", 70*1024))
	xtesting.Prints(t, unidiff.Parse([]byte(long)), `
		p.go: 1, 10
	`)
}

func TestLines(t *testing.T) {
	raw := []byte(`diff --git a/version.go b/version.go
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
	got := slices.Sorted(maps.Keys(unidiff.Lines(raw)))
	xtesting.EqualText(t, fmt.Sprint(got), "[10 21 22 42 43]")

	long := fmt.Sprintf("@@ -1 +1 @@\n+%s\n@@ -10 +10 @@\n+x\n", strings.Repeat("x", 70*1024))
	gotLong := slices.Sorted(maps.Keys(unidiff.Lines([]byte(long))))
	xtesting.EqualText(t, fmt.Sprint(gotLong), "[1 10]")
}

func ExampleParse() {
	d := unidiff.Parse([]byte(`+++ b/main.go
@@ -10 +10,2 @@
+a
+b
@@ -20,2 +21,0 @@
`))
	fmt.Print(d)
	// Output:
	// main.go: 10, 11, 21, 22
}
