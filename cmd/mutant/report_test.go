package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/token"
	"slices"
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
	// The picks come back in source order. byLine is a map, so with only a
	// few lines an unsorted result can come out sorted by chance.
	var many []mutant.Mutant
	for line := range 20 {
		many = append(many, at(line+1, "x", ""), at(line+1, "y", ""))
	}
	if keep := choose(options{}, "/src/p.go", many); len(keep) != 20 || !slices.IsSorted(keep) {
		t.Errorf("one per line over 20 lines picked %v, want 20 in source order", keep)
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

// TestChooseProperties checks deterministic one-per-line hashing varies across
// lines and filenames, preserves ordering, and respects equivalent/arid/diff filters.
func TestChooseProperties(t *testing.T) {
	var ms []mutant.Mutant
	for line := 1; line <= 40; line++ {
		ms = append(ms,
			at(line, "a", ""),
			at(line, "b", ""),
			at(line, "c", ""),
			at(line, "d", ""),
			at(line, "eq", "smallest value compared"),
			arid(line, "log"),
		)
	}
	pickP := choose(options{}, "/src/p.go", ms)
	pickQ := choose(options{}, "/src/q.go", ms)
	all := choose(options{all: true}, "/src/p.go", ms)
	if len(pickP) != 40 || !slices.IsSorted(pickP) {
		t.Fatalf("picked %d mutants, sorted=%v; want 40 sorted", len(pickP), slices.IsSorted(pickP))
	}
	slots := map[string]bool{}
	allSet := map[int]bool{}
	for _, i := range all {
		allSet[i] = true
		if ms[i].Equivalent != "" || ms[i].Arid != "" {
			t.Fatalf("choose(all=true) kept filtered mutant %+v", ms[i])
		}
	}
	seenLine := map[int]bool{}
	for _, i := range pickP {
		if !allSet[i] {
			t.Fatalf("one-per-line pick %d not in all", i)
		}
		if seenLine[ms[i].Pos.Line] {
			t.Fatalf("duplicate line %d in one-per-line picks", ms[i].Pos.Line)
		}
		seenLine[ms[i].Pos.Line] = true
		slots[ms[i].Mutated] = true
	}
	if len(slots) < 2 {
		t.Fatalf("one-per-line hash picked only %v across 40 lines; hash is not varying", slots)
	}
	if slices.Equal(pickP, pickQ) {
		t.Fatal("different file names picked identical indices across 40 lines")
	}
}

// TestReportOrderingAndFallbacks checks verdict/path/line sorting, per-file
// source caching, read-error fallback, relative-path fallback, and JSON output.
func TestReportOrderingAndFallbacks(t *testing.T) {
	src := map[string]string{
		"/src/a.go": "if a > 0 {\n\treturn 1\n}\nif b > 0 {\n\treturn 2\n}",
		"/src/b.go": "if c > 0 {\n\treturn 3\n}",
	}
	// Deliberately unsorted by verdict rank, path, and line.
	m := &killmatrix.Matrix{
		Mutants: []killmatrix.Mutant{
			{ID: 1, Path: "/src/b.go", Line: 1, Col: 6, Operator: mutant.ROR, Original: "c > 0", Mutated: "c >= 0", Compiled: true},
			{ID: 2, Path: "/src/a.go", Line: 4, Col: 6, Operator: mutant.ROR, Original: "b > 0", Mutated: "b >= 0", Compiled: true},
			{ID: 3, Path: "/src/a.go", Line: 1, Col: 6, Operator: mutant.ROR, Original: "a > 0", Mutated: "a >= 0", Compiled: true},
			{ID: 4, Path: "/src/missing.go", Line: 1, Col: 1, Operator: mutant.SDL, Original: "x++", Mutated: "", Compiled: true},
			{ID: 5, Path: "/src/b.go", Line: 2, Col: 2, Operator: mutant.SDL, Original: "return 3", Mutated: "", Compiled: true},
			{ID: 6, Path: "/src/a.go", Line: 2, Col: 2, Operator: mutant.SDL, Original: "return 1", Mutated: "", Compiled: false},
		},
		Results: []killmatrix.Result{
			{MutantID: 1, TestID: 1, Status: killmatrix.Timeout},
			{MutantID: 2, TestID: 1, Status: killmatrix.Survived},
			{MutantID: 3, TestID: 1, Status: killmatrix.Survived},
			{MutantID: 4, TestID: 1, Status: killmatrix.Survived, Skipped: true},
			{MutantID: 5, TestID: 1, Status: killmatrix.Killed},
		},
	}
	reads := map[string]int{}
	read := func(path string) ([]byte, error) {
		reads[path]++
		if s, ok := src[path]; ok {
			return []byte(s), nil
		}
		return []byte("  x++ // stale"), errors.New("not found")
	}
	got := report(m, "/src", read)
	xtesting.EqualText(t, got, `
		survived  a.go:1  ROR
		  -if a > 0 {
		  +if a >= 0 {

		survived  a.go:4  ROR
		  -if b > 0 {
		  +if b >= 0 {

		not covered  missing.go:1  SDL  (no test runs this function)
		  -x++
		  +(deleted)

		timeout  b.go:1  ROR
		  -if c > 0 {
		  +if c >= 0 {

		6 mutants: 1 killed, 2 survived, 1 not covered, 1 timed out, 1 did not build
	`)
	if reads["/src/a.go"] != 1 {
		t.Errorf("a.go read %d times, want 1", reads["/src/a.go"])
	}
	// Relative root with absolute path makes filepath.Rel fail and fall back to mu.Path.
	if relFallback := report(m, "relative", read); !strings.Contains(relFallback, "survived  /src/a.go:1") {
		t.Errorf("report did not fall back to full path on Rel error:\n%s", relFallback)
	}
	rawJSON := reportJSON(m, "relative")
	lines := strings.Split(strings.TrimSpace(rawJSON), "\n")
	if len(lines) != len(m.Mutants) {
		t.Fatalf("reportJSON emitted %d lines, want %d", len(lines), len(m.Mutants))
	}
	verdicts := m.Verdicts()
	for i, line := range lines {
		var jm jsonMutant
		if err := json.Unmarshal([]byte(line), &jm); err != nil {
			t.Fatalf("line %d invalid JSON: %v", i, err)
		}
		if jm.File != m.Mutants[i].Path || jm.Verdict != verdicts[m.Mutants[i].ID] {
			t.Errorf("line %d = %+v, want path=%q verdict=%q", i, jm, m.Mutants[i].Path, verdicts[m.Mutants[i].ID])
		}
	}
	if strings.Contains(rawJSON, `\u003e`) {
		t.Error("reportJSON escaped '>'")
	}
}

func TestLineChange(t *testing.T) {
	lines := []string{
		"a > 0; a > 0 // first",
		"if a > 0 && a > 0 {",
		"\treturn x",
	}
	tests := []struct {
		name      string
		mu        killmatrix.Mutant
		wantBef   string
		wantAfter string
	}{
		{
			name:      "first line at col 1",
			mu:        killmatrix.Mutant{Line: 1, Col: 5, Original: "a > 0", Mutated: "a >= 0"},
			wantBef:   "a > 0; a > 0 // first",
			wantAfter: "a >= 0; a > 0 // first",
		},
		{
			name:      "second occurrence on same line",
			mu:        killmatrix.Mutant{Line: 2, Col: 15, Original: "a > 0", Mutated: "a >= 0"},
			wantBef:   "if a > 0 && a > 0 {",
			wantAfter: "if a > 0 && a >= 0 {",
		},
		{
			name:      "last line",
			mu:        killmatrix.Mutant{Line: 3, Col: 2, Original: "return x", Mutated: ""},
			wantBef:   "\treturn x",
			wantAfter: "\t(deleted)",
		},
		{
			name:      "line 0 falls back",
			mu:        killmatrix.Mutant{Line: 0, Col: 1, Original: "a > 0", Mutated: "a >= 0"},
			wantBef:   "a > 0",
			wantAfter: "a >= 0",
		},
		{
			name:      "line past EOF falls back",
			mu:        killmatrix.Mutant{Line: 4, Col: 1, Original: "a > 0", Mutated: "a >= 0"},
			wantBef:   "a > 0",
			wantAfter: "a >= 0",
		},
		{
			name:      "multiline original falls back",
			mu:        killmatrix.Mutant{Line: 2, Col: 1, Original: "a\nb", Mutated: "c"},
			wantBef:   "a\nb",
			wantAfter: "c",
		},
		{
			name:      "original missing from line falls back",
			mu:        killmatrix.Mutant{Line: 3, Col: 1, Original: "absent", Mutated: "other"},
			wantBef:   "absent",
			wantAfter: "other",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bef, aft := lineChange(lines, tc.mu)
			if bef != tc.wantBef || aft != tc.wantAfter {
				t.Errorf("got (%q, %q), want (%q, %q)", bef, aft, tc.wantBef, tc.wantAfter)
			}
		})
	}
}

func TestParseDiffLongLine(t *testing.T) {
	diff := fmt.Sprintf("diff --git a/p.go b/p.go\n--- a/p.go\n+++ b/p.go\n@@ -1 +1 @@\n+%s\n@@ -10 +10 @@\n+x\n", strings.Repeat("x", 70*1024))
	got := parseDiff([]byte(diff), "/src")
	if !got["/src/p.go"][1] || !got["/src/p.go"][10] {
		t.Fatalf("long diff line truncated parseDiff: %v", got)
	}
}
