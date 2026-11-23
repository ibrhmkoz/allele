package main

import (
	"fmt"
	"go/token"
	"strings"
	"testing"

	"github.com/ibrhmkoz/go-kit/mutant"
	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
	"github.com/ibrhmkoz/go-kit/xtesting"
)

func at(line int, mutated string, equivalent string) mutant.Mutant {
	return mutant.Mutant{Pos: token.Position{Line: line}, Mutated: mutated, Equivalent: equivalent}
}

func arid(line int, mutated string) mutant.Mutant {
	return mutant.Mutant{Pos: token.Position{Line: line}, Mutated: mutated, Arid: "logging"}
}

func TestChoose(t *testing.T) {
	ms := []mutant.Mutant{
		at(3, "a", ""), at(3, "b", ""), at(3, "c", ""),
		at(4, "d", "smallest value compared"), at(4, "e", ""),
		at(5, "f", "smallest value compared"),
		at(9, "g", ""),
		arid(10, "h"),
	}
	show := func(keep []int) string {
		var s []string
		for _, i := range keep {
			s = append(s, ms[i].Mutated)
		}
		return strings.Join(s, " ")
	}
	// Equivalents never run. -all keeps the rest; by default one per line,
	// picked by a hash of the file and line, so reruns pick the same.
	xtesting.EqualText(t, show(choose(options{all: true}, "/src/p.go", ms)), "a b c e g")
	xtesting.EqualText(t, show(choose(options{all: true, arid: true}, "/src/p.go", ms)), "a b c e g h")
	one := show(choose(options{}, "/src/p.go", ms))
	if len(strings.Fields(one)) != 3 || !strings.HasSuffix(one, "e g") || one != show(choose(options{}, "/src/p.go", ms)) {
		t.Errorf("one per line picked %q", one)
	}
	// A diff limits the run to the lines it touches.
	diff := map[string]map[int]bool{"/src/p.go": {4: true, 9: true}}
	xtesting.EqualText(t, show(choose(options{all: true, diff: diff}, "/src/p.go", ms)), "e g")
	xtesting.EqualText(t, show(choose(options{all: true, diff: diff}, "/src/q.go", ms)), "")
}

func TestReport(t *testing.T) {
	src := map[string]string{
		"/src/p/version.go": "package p\n\nfunc tooLong(v string) bool {\n\tif len(v) > 128 {\n\t\treturn true\n\t}\n\treturn false\n}\n",
	}
	m := &killmatrix.Matrix{
		Mutants: []killmatrix.Mutant{
			{ID: 1, Path: "/src/p/version.go", File: "version.go", Line: 4, Col: 12, Operator: mutant.ROR, Original: "len(v) > 128", Mutated: "len(v) >= 128", Compiled: true},
			{ID: 2, Path: "/src/p/version.go", File: "version.go", Line: 4, Col: 12, Operator: mutant.ROR, Original: "len(v) > 128", Mutated: "len(v) < 128", Compiled: true},
			{ID: 3, Path: "/src/p/version.go", File: "version.go", Line: 5, Col: 3, Operator: mutant.SDL, Original: "return true", Mutated: "", Compiled: true},
			{ID: 4, Path: "/src/p/version.go", File: "version.go", Line: 7, Col: 9, Operator: mutant.COR, Original: "false", Mutated: "true", Compiled: false},
		},
		Results: []killmatrix.Result{
			{MutantID: 1, TestID: 1, Status: killmatrix.Survived},
			{MutantID: 2, TestID: 1, Status: killmatrix.Killed},
			{MutantID: 3, TestID: 1, Status: killmatrix.Survived, Skipped: true},
		},
	}
	read := func(path string) ([]byte, error) { return []byte(src[path]), nil }
	got := report(m, "/src", read)
	xtesting.EqualText(t, got, `
		survived  p/version.go:4  ROR
		  -	if len(v) > 128 {
		  +	if len(v) >= 128 {

		not covered  p/version.go:5  SDL  (no test runs this function)
		  -		return true
		  +		(deleted)

		4 mutants: 1 killed, 1 survived, 1 not covered, 0 timed out, 1 did not build
	`)
}

func TestParseDiff(t *testing.T) {
	out := []byte(`diff --git a/p/version.go b/p/version.go
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
`)
	got := parseDiff(out, "/src")
	xtesting.EqualText(t, fmt.Sprint(got), "map[/src/p/version.go:map[4:true 9:true 10:true]]")
}

func TestReportJSON(t *testing.T) {
	m := &killmatrix.Matrix{
		Mutants: []killmatrix.Mutant{
			{ID: 1, Path: "/src/p/version.go", Line: 4, Col: 12, Operator: mutant.ROR, Original: "len(v) > 128", Mutated: "len(v) >= 128", Compiled: true},
			{ID: 2, Path: "/src/p/version.go", Line: 4, Col: 12, Operator: mutant.ROR, Original: "len(v) > 128", Mutated: "len(v) < 128", Compiled: true},
		},
		Results: []killmatrix.Result{
			{MutantID: 1, TestID: 1, Status: killmatrix.Survived},
			{MutantID: 2, TestID: 1, Status: killmatrix.Killed},
		},
	}
	// One line per mutant, killed ones too, for tools to filter.
	xtesting.EqualText(t, reportJSON(m, "/src"), `
		{"file":"p/version.go","line":4,"col":12,"operator":"ROR","original":"len(v) > 128","mutated":"len(v) >= 128","verdict":"survived"}
		{"file":"p/version.go","line":4,"col":12,"operator":"ROR","original":"len(v) > 128","mutated":"len(v) < 128","verdict":"killed"}
	`)
}
