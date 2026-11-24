package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibrhmkoz/allele/internal/xtesting"
)

func TestNext(t *testing.T) {
	code := commit{files: []string{"killmatrix/killmatrix.go"}}
	docs := commit{files: []string{"docs/DATA.md", "README.md", "docs/data/glob/mutants.ndjson.gz"}}
	with := func(c commit, release string) commit { c.release = release; return c }
	var b strings.Builder
	for _, c := range []struct {
		name    string
		last    string
		commits []commit
	}{
		{"nothing since the tag", "v0.1.1", nil},
		{"code: patch", "v0.1.1", []commit{code}},
		{"docs only: none", "v0.1.1", []commit{docs, docs}},
		{"docs and code: patch", "v0.1.1", []commit{docs, code}},
		{"a minor trailer wins", "v0.1.1", []commit{code, with(docs, "minor")}},
		{"major beats minor", "v0.4.2", []commit{with(code, "minor"), with(code, "Major")}},
		{"none skips code", "v0.1.1", []commit{with(code, "none")}},
		{"none on one, patch on another", "v0.1.1", []commit{with(code, "none"), code}},
		{"an unknown trailer counts as patch", "v0.1.1", []commit{with(docs, "soon")}},
		{"no tag yet", "", []commit{code}},
	} {
		next, ok := next(c.last, c.commits)
		fmt.Fprintf(&b, "%-36s %q %v\n", c.name, next, ok)
	}
	xtesting.EqualText(t, b.String(), `
		nothing since the tag                "" false
		code: patch                          "v0.1.2" true
		docs only: none                      "" false
		docs and code: patch                 "v0.1.2" true
		a minor trailer wins                 "v0.2.0" true
		major beats minor                    "v1.0.0" true
		none skips code                      "" false
		none on one, patch on another        "v0.1.2" true
		an unknown trailer counts as patch   "v0.1.2" true
		no tag yet                           "v0.0.1" true
	`)
}

func TestNextBadTag(t *testing.T) {
	for _, tag := range []string{"release-1", "v1.2", "v1.2.3.4", "1.2.3", "v1.x.3"} {
		if got, ok := next(tag, []commit{{files: []string{"a.go"}}}); ok {
			t.Errorf("%s, no version, gave %s", tag, got)
		}
	}
}

// TestPlan tells a repository's story: each step commits, then asks for
// the next tag.
func TestPlan(t *testing.T) {
	dir := t.TempDir()
	git := gitIn(dir)
	must := func(args ...string) {
		t.Helper()
		if out, err := git(args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	must("init", "-q", "-b", "main")
	must("config", "user.email", "t@example.com")
	must("config", "user.name", "t")
	commitFile := func(name, msg string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(msg), 0o600); err != nil {
			t.Fatal(err)
		}
		must("add", "-A")
		must("commit", "-q", "-m", msg)
	}
	var b strings.Builder
	step := func(what string) {
		tag, ok, err := plan(git)
		fmt.Fprintf(&b, "%-28s %q %v %v\n", what, tag, ok, err)
	}
	commitFile("a.go", "first code")
	step("code, no tag yet")
	must("tag", "-a", "v0.1.0", "-m", "v0.1.0")
	step("tagged")
	commitFile("docs/notes.md", "notes")
	step("docs since the tag")
	commitFile("b.go", "feature\n\nRelease: minor")
	step("a minor trailer")
	commitFile("c.go", "quiet\n\nRelease: none")
	must("tag", "-a", "v0.2.0", "-m", "v0.2.0")
	commitFile("c.go", "quiet again\n\nRelease: none")
	step("none since v0.2.0")
	xtesting.EqualText(t, b.String(), `
		code, no tag yet             "v0.0.1" true <nil>
		tagged                       "" false <nil>
		docs since the tag           "" false <nil>
		a minor trailer              "v0.2.0" true <nil>
		none since v0.2.0            "" false <nil>
	`)

	if _, _, err := plan(func(args ...string) (string, error) {
		if args[0] == "rev-list" {
			return "", errors.New("no repository")
		}
		return "v0.1.0", nil
	}); err == nil {
		t.Error("a failing git gave no error")
	}
	if _, _, err := plan(gitIn(t.TempDir())); err == nil {
		t.Error("a directory with no repository gave no error")
	}
}
