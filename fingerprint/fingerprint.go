// Package fingerprint hashes what a Go package's test results depend on,
// so that a tool can reuse results whose inputs have not changed.
//
// A Print covers the go command's version and target, the package's files,
// the files of every package it or its tests import outside the standard
// library, the files it embeds and its testdata directory. Each Test,
// Example and Fuzz function is hashed on its own, so a tool can tell that
// only some tests changed. Not covered: environment variables, external
// services, and files tests read outside the package's testdata.
package fingerprint

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"hash"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/packages"
)

// Print is a package's fingerprint. Two loads of unchanged inputs give
// equal prints, wherever the module sits on disk.
type Print struct {
	// Shared hashes every input but the tests' own functions.
	Shared string
	// Tests maps each Test, Example and Fuzz function to a hash of its
	// source. TestMain is shared: it wraps every test.
	Tests map[string]string
}

// Load returns the prints of the packages that patterns match in dir,
// keyed by import path.
func Load(ctx context.Context, dir string, patterns ...string) (map[string]Print, error) {
	cfg := &packages.Config{
		Context: ctx,
		Dir:     dir,
		Mode:    packages.NeedName | packages.NeedFiles | packages.NeedImports | packages.NeedDeps | packages.NeedEmbedFiles | packages.NeedForTest | packages.NeedModule,
		Tests:   true,
	}
	loaded, err := packages.Load(cfg, patterns...)
	cmd := exec.CommandContext(ctx, "go", "env", "GOVERSION", "GOOS", "GOARCH")
	cmd.Dir = dir //mut:ignore the tests run in a module of their own toolchain
	toolchain, envErr := cmd.Output()
	if err := errors.Join(err, envErr); err != nil {
		return nil, err
	}
	packages.Visit(loaded, nil, func(p *packages.Package) {
		if len(p.Errors) > 0 && err == nil {
			err = fmt.Errorf("%s: %w", p.PkgPath, p.Errors[0])
		}
	})
	if err != nil {
		return nil, err
	}
	// With Tests, a package comes back with its test variants, which
	// ForTest names it in, and the test binary's generated main package,
	// whose list of tests is in Tests.
	variants := map[string][]*packages.Package{}
	for _, p := range loaded {
		if p.Name == "main" && strings.HasSuffix(p.PkgPath, ".test") {
			continue
		}
		path := cmp.Or(p.ForTest, p.PkgPath)
		variants[path] = append(variants[path], p)
	}
	prints := map[string]Print{}
	for path, ps := range variants {
		prints[path] = of(ps, toolchain)
	}
	return prints, nil
}

// of hashes the inputs of a package, reached through its variants: the
// package itself and, if it has tests, the packages its test binary is
// built from. All of them share the package's directory and module.
func of(variants []*packages.Package, toolchain []byte) Print {
	pkg := variants[0]
	label := map[string]string{} // file → its name in the hash
	packages.Visit(variants, nil, func(p *packages.Package) {
		// The standard library is pinned by the toolchain.
		if p.Module == nil {
			return
		}
		for _, f := range slices.Concat(p.GoFiles, p.EmbedFiles) {
			label[f] = name(p.Module, f)
		}
	})
	_ = filepath.WalkDir(filepath.Join(pkg.Dir, "testdata"), func(f string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			label[f] = name(pkg.Module, f)
		}
		return nil
	})

	shared := sha256.New()
	_, _ = shared.Write(toolchain)
	tests := map[string]string{}
	for _, f := range slices.Sorted(maps.Keys(label)) {
		// A file that cannot be read still counts, by name.
		src, _ := os.ReadFile(f)
		if strings.HasSuffix(f, "_test.go") {
			splitTests(shared, tests, label[f], src)
		} else {
			frame(shared, label[f], src)
		}
	}
	return Print{Shared: hex.EncodeToString(shared.Sum(nil)), Tests: tests}
}

// splitTests hashes each test function of a test file into tests and the
// rest of the file into shared, so that an edit to one test leaves the
// others' hashes alone.
func splitTests(shared hash.Hash, tests map[string]string, label string, src []byte) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, 0)
	if err != nil { //mut:ignore Load fails on a file that does not parse before it gets here
		frame(shared, label, src) //mut:ignore
		return
	}
	frame(shared, label, []byte(f.Name.Name))
	for _, d := range f.Decls {
		text := src[fset.Position(d.Pos()).Offset:fset.Position(d.End()).Offset]
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && isTest(fn.Name.Name) {
			sum := sha256.Sum256(text)
			tests[fn.Name.Name] = hex.EncodeToString(sum[:])
			continue
		}
		frame(shared, label, text)
	}
}

// isTest reports whether go test runs a function of this name, by its
// rule: a prefix not followed by a lowercase letter. TestMain is no test.
func isTest(name string) bool {
	for _, prefix := range []string{"Test", "Example", "Fuzz"} {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok || name == "TestMain" {
			continue
		}
		r, _ := utf8.DecodeRuneInString(rest)
		return !unicode.IsLower(r)
	}
	return false
}

// name is a file's name relative to its module, so that a print does not
// depend on where the module sits on disk.
func name(m *packages.Module, f string) string {
	rel, _ := filepath.Rel(m.Dir, f)
	return m.Path + "@" + m.Version + "/" + filepath.ToSlash(rel)
}

// frame writes data with its label and length, so that no two different
// sequences of inputs hash the same bytes.
func frame(h hash.Hash, label string, data []byte) {
	_, _ = fmt.Fprintf(h, "%s:%d\x00", label, len(data))
	_, _ = h.Write(data)
}
