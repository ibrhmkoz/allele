package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrhmkoz/go-kit/xtesting"
)

// fakeMutant writes a script that prints, as mutant -json would, the file
// of its name in the directory it runs in, or fails with a message.
func fakeMutant(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	script := "#!/bin/sh\ncat " + name + ".json\n"
	if name == "bad" {
		script = "#!/bin/sh\necho oops >&2\nexit 2\n"
	}
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func line(v verdict) string {
	b, _ := json.Marshal(v)
	return string(b) + "\n"
}

// TestCLI runs two builds on a one-repository corpus, in turns, and
// scores each against the reference and its rerun: the one that hides a
// gap scores lower, and a mutant the two runs disagree on counts for
// neither.
func TestCLI(t *testing.T) {
	corpus, ref, rerun := t.TempDir(), t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(corpus, "m", "go.mod"), "module example.com/m\n\ngo 1.21\n")
	if err := refMatrix(false).Write(filepath.Join(ref, "m")); err != nil {
		t.Fatal(err)
	}
	if err := refMatrix(true).Write(filepath.Join(rerun, "m")); err != nil {
		t.Fatal(err)
	}
	// A line longer than a bufio.Scanner takes by default.
	long := line(verdict{File: "p.go", Line: 99, Original: strings.Repeat("x", 100_000), Verdict: "killed"})
	writeFile(t, filepath.Join(corpus, "m", "good.json"), line(reported(1, "p.go", "killed"))+line(reported(3, "sub/s.go", "survived"))+line(reported(4, "sub/s.go", "survived"))+long)
	writeFile(t, filepath.Join(corpus, "m", "hiding.json"), line(reported(1, "p.go", "killed"))+line(reported(4, "sub/s.go", "killed")))
	rows := filepath.Join(t.TempDir(), "rows.ndjson")
	cli := func(args ...string) (int, string) {
		var stdout, stderr strings.Builder
		code := cli(t.Context(), append([]string{"-corpus", corpus, "-ref", ref, "-rerun", rerun, "-repos", "m", "-reps", "2", "-j", "1", "-seeds", "2"}, args...), &stdout, &stderr)
		return code, stdout.String() + stderr.String()
	}
	if code, out := cli("-o", rows, "good="+fakeMutant(t, "good"), "hiding="+fakeMutant(t, "hiding")); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	data, err := os.ReadFile(rows)
	if err != nil {
		t.Fatal(err)
	}
	var summary []string
	ms := map[string]metric{}
	for row := range strings.Lines(string(data)) {
		var r map[string]any
		if err := json.Unmarshal([]byte(row), &r); err != nil {
			t.Fatal(err)
		}
		switch {
		case r["wall_s"] != nil:
			summary = append(summary, fmt.Sprintf("timing %s rep %v", r["bin"], r["rep"]))
		case r["m"] != nil:
			var m metric
			_ = json.Unmarshal([]byte(row), &m)
			ms[m.Bin] = m
			summary = append(summary, "metric "+m.Bin)
		default:
			summary = append(summary, fmt.Sprintf("score %s reported %v unknown %v stable %v hidden %v", r["repo"], r["reported"], r["unknown"], r["stable"], r["hidden"]))
		}
	}
	xtesting.EqualText(t, strings.Join(summary, "\n"), `
		timing good rep 1
		timing hiding rep 1
		timing good rep 2
		timing hiding rep 2
		score m reported 4 unknown 1 stable 2 hidden 0
		metric good
		score m reported 2 unknown 0 stable 2 hidden 1
		metric hiding
	`)
	good, hiding := ms["good"], ms["hiding"]
	if !(hiding.M < good.M && good.M > 0 && good.T > 0 && hiding.T > 0) {
		t.Errorf("good %+v, hiding a gap %+v", good, hiding)
	}

	// Without -o nothing is written, and the table still prints.
	code, out := cli("good=" + fakeMutant(t, "good"))
	for _, says := range []string{"rep 1 m ", "bin    repo", "good   m ", "good   M = "} {
		if code != 0 || !strings.Contains(out, says) {
			t.Errorf("exit %d, no %q: %s", code, says, out)
		}
	}
	for name, c := range map[string]struct {
		args []string
		code int
		says string
	}{
		"no build":             {nil, 2, "no build given"},
		"unknown flag":         {[]string{"-nope"}, 2, "flag provided but not defined"},
		"failing build":        {[]string{"bad=" + fakeMutant(t, "bad")}, 1, "oops"},
		"output not JSON":      {[]string{"notjson=" + fakeMutant(t, "notjson")}, 1, "invalid character"},
		"no reference":         {[]string{"-ref", t.TempDir(), "good=" + fakeMutant(t, "good")}, 1, "no such file"},
		"no go.mod":            {[]string{"-corpus", t.TempDir(), "good=" + fakeMutant(t, "good")}, 1, "go.mod"},
		"line too long":        {[]string{"huge=" + fakeMutant(t, "huge")}, 1, "token too long"},
		"rows cannot be saved": {[]string{"-o", t.TempDir(), "good=" + fakeMutant(t, "good")}, 1, "is a directory"},
	} {
		writeFile(t, filepath.Join(corpus, "m", "notjson.json"), "not json\n")
		writeFile(t, filepath.Join(corpus, "m", "huge.json"), line(verdict{Original: strings.Repeat("x", 2<<20)}))
		code, out := cli(c.args...)
		if code != c.code || !strings.Contains(out, c.says) {
			t.Errorf("%s: exit %d, want %d saying %q: %s", name, code, c.code, c.says, out)
		}
	}
}

func TestParseBins(t *testing.T) {
	bins, err := parseBins([]string{"base=/a/mutant", "/b/new"})
	if err != nil {
		t.Fatal(err)
	}
	xtesting.EqualText(t, bins[0].name+" "+bins[0].path+" "+bins[1].name+" "+bins[1].path, "base /a/mutant new /b/new")
	if _, err := parseBins(nil); err == nil {
		t.Error("no error without a build")
	}
}

func TestModuleDir(t *testing.T) {
	dir := t.TempDir()
	for gomod, want := range map[string]string{
		"module example.com/m\n":             "m",
		"// c\nmodule \"github.com/a/v3\"\n": "v3",
		"module shlex\n":                     "shlex",
	} {
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
			t.Fatal(err)
		}
		if got, err := moduleDir(dir); err != nil || got != want {
			t.Errorf("%q: %q, %v; want %q", gomod, got, err, want)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("go 1.21\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := moduleDir(dir); err == nil {
		t.Error("no error without a module line")
	}
	if _, err := moduleDir(t.TempDir()); err == nil {
		t.Error("no error without go.mod")
	}
}

func TestMedian(t *testing.T) {
	for _, c := range []struct {
		xs   []float64
		want float64
	}{{nil, 0}, {[]float64{3}, 3}, {[]float64{5, 1, 3}, 3}, {[]float64{4, 1, 3, 2}, 2.5}} {
		if got := median(c.xs); got != c.want {
			t.Errorf("median(%v) = %v, want %v", c.xs, got, c.want)
		}
	}
}
