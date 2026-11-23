package fingerprint_test

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/ibrhmkoz/go-kit/mutant/fingerprint"
	"github.com/ibrhmkoz/go-kit/xtesting"
)

// module is a small module: package p uses dep in its code and helper
// only in its tests, embeds msg.txt and reads testdata/want.txt.
var module = map[string]string{
	"go.mod":     "module example.com/m\n\ngo 1.21\n",
	"dep/dep.go": "package dep\n\nfunc Two() int { return 2 }\n\n// TestValue is not a test: it is not in a _test.go file.\nfunc TestValue() int { return 0 }\n",
	"other/o.go": "package other\n\nfunc Three() int { return 3 }\n",
	// A real main package, and packages whose paths end in .test as the
	// test binaries' generated main packages do.
	"cmd/tool/main.go":  "package main\n\nfunc main() {}\n",
	"data.test/data.go": "package data\n",
	"zeta.test/zeta.go": "package zeta\n",
	"helper/h.go":       "package helper\n\nfunc Same(a, b string) bool { return a == b }\n",
	"p/p.go": `package p

import (
	_ "embed"

	"example.com/m/dep"
)

//go:embed msg.txt
var msg string

func Msg(n int) string {
	if n > dep.Two() {
		return msg
	}
	return ""
}
`,
	"p/msg.txt":           "ok",
	"p/README.md":         "Package p.",
	"p/testdata/want.txt": "ok",
	"p/p_test.go": `package p

import (
	"os"
	"testing"

	"example.com/m/helper"
)

func want(t *testing.T) string {
	b, err := os.ReadFile("testdata/want.txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestA(t *testing.T) {
	if !helper.Same(Msg(3), want(t)) {
		t.Fatal(Msg(3))
	}
}

func TestB(t *testing.T) {
	if Msg(1) != "" {
		t.Fatal(Msg(1))
	}
}

// Testify is not a test: a lowercase letter follows Test.
func Testify(t *testing.T) {}
`,
}

func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if content == "/" {
			if err := os.MkdirAll(p, 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if content == "" {
			if err := os.Remove(p); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func load(t *testing.T, edit func(dir string)) fingerprint.Print {
	t.Helper()
	dir := t.TempDir()
	write(t, dir, module)
	if edit != nil {
		edit(dir)
	}
	prints, err := fingerprint.Load(t.Context(), dir, "./...")
	if err != nil {
		t.Fatal(err)
	}
	// One print per package, none for the test binaries' main packages.
	xtesting.EqualText(t, strings.Join(slices.Sorted(maps.Keys(prints)), " "), "example.com/m/cmd/tool example.com/m/data.test example.com/m/dep example.com/m/helper example.com/m/other example.com/m/p example.com/m/zeta.test")
	return prints["example.com/m/p"]
}

// changed names what differs between two prints: "shared" and the tests
// whose own source changed, appeared or went away.
func changed(a, b fingerprint.Print) string {
	var out []string
	if a.Shared != b.Shared {
		out = append(out, "shared")
	}
	names := slices.Sorted(maps.Keys(a.Tests))
	for name := range b.Tests {
		if _, ok := a.Tests[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		if a.Tests[name] != b.Tests[name] {
			out = append(out, name)
		}
	}
	return strings.Join(out, " ")
}

// TestLoad edits one input of package p at a time and checks which part of
// its print changes: a test's own source changes only that test, anything
// else its tests could depend on changes the shared part, and the rest
// changes nothing.
func TestLoad(t *testing.T) {
	t.Parallel()
	ref := load(t, nil)
	xtesting.EqualText(t, strings.Join(slices.Sorted(maps.Keys(ref.Tests)), " "), "TestA TestB")
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"nothing", nil, ""},
		{"package code", map[string]string{"p/p.go": strings.Replace(module["p/p.go"], "> dep.Two()", ">= dep.Two()", 1)}, "shared"},
		{"dependency", map[string]string{"dep/dep.go": "package dep\n\nfunc Two() int { return 3 }\n"}, "shared"},
		{"test-only dependency", map[string]string{"helper/h.go": "package helper\n\nfunc Same(a, b string) bool { return true }\n"}, "shared"},
		{"embedded file", map[string]string{"p/msg.txt": "ok!"}, "shared"},
		{"testdata file", map[string]string{"p/testdata/want.txt": "ok!"}, "shared"},
		{"same-length edit", map[string]string{"p/testdata/want.txt": "no"}, "shared"},
		{"new testdata file", map[string]string{"p/testdata/more.txt": "x"}, "shared"},
		{"shared test helper", map[string]string{"p/p_test.go": strings.Replace(module["p/p_test.go"], "return string(b)", "return string(b) + \"\"", 1)}, "shared"},
		{"TestMain, which wraps every test", map[string]string{"p/p_test.go": module["p/p_test.go"] + "\nfunc TestMain(m *testing.M) { os.Exit(m.Run()) }\n"}, "shared"},
		{"helper named like a test", map[string]string{"p/p_test.go": strings.Replace(module["p/p_test.go"], "func Testify(t *testing.T) {}", "func Testify(t *testing.T) { t.Helper() }", 1)}, "shared"},
		{"test package clause", map[string]string{"p/p_test.go": strings.Replace(module["p/p_test.go"], "package p\n", "package p_test\n", 1)}, "shared"},
		{"renamed testdata file", map[string]string{"p/testdata/want.txt": "", "p/testdata/w.txt": "ok"}, "shared"},
		{"one test", map[string]string{"p/p_test.go": strings.Replace(module["p/p_test.go"], "Msg(1)", "Msg(2)", 2)}, "TestB"},
		{"new test", map[string]string{"p/p_test.go": module["p/p_test.go"] + "\nfunc TestC(t *testing.T) {}\n"}, "TestC"},
		{"removed test", map[string]string{"p/p_test.go": strings.Replace(module["p/p_test.go"], "func TestB(t *testing.T) {\n\tif Msg(1) != \"\" {\n\t\tt.Fatal(Msg(1))\n\t}\n}\n", "", 1)}, "TestB"},
		{"unrelated package", map[string]string{"other/o.go": "package other\n\nfunc Three() int { return 4 }\n"}, ""},
		{"file no build reads", map[string]string{"p/README.md": "Package p does things."}, ""},
		{"empty testdata directory", map[string]string{"p/testdata/empty/": "/"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := load(t, func(dir string) { write(t, dir, tc.files) })
			xtesting.EqualText(t, changed(ref, got), tc.want)
		})
	}
}

// TestLoadTarget: the same sources built for another target are another
// build.
func TestLoadTarget(t *testing.T) {
	ref := load(t, nil)
	t.Setenv("GOARCH", map[string]string{"arm64": "amd64"}[runtime.GOARCH]+map[string]string{"amd64": "arm64"}[runtime.GOARCH])
	xtesting.EqualText(t, changed(ref, load(t, nil)), "shared")
}

func TestLoadErrors(t *testing.T) {
	t.Parallel()
	if _, err := fingerprint.Load(t.Context(), filepath.Join(t.TempDir(), "missing"), "./..."); err == nil {
		t.Error("no error for a directory that does not exist")
	}
	dir := t.TempDir()
	write(t, dir, map[string]string{"go.mod": module["go.mod"], "p/p.go": "package p\n\nimport \"example.com/m/missing\"\n\nvar _ = missing.X\n"})
	if _, err := fingerprint.Load(t.Context(), dir, "./..."); err == nil {
		t.Error("no error for a package that imports a missing one")
	}
	dir = t.TempDir()
	write(t, dir, map[string]string{"go.mod": module["go.mod"], "p/p.go": "package p\n", "p/p_test.go": "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { a + }\n"})
	if _, err := fingerprint.Load(t.Context(), dir, "./..."); err == nil {
		t.Error("no error for a test file that does not parse")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := fingerprint.Load(ctx, dir, "./..."); err == nil {
		t.Error("no error for a cancelled context")
	}
}
