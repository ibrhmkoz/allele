package killmatrix_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ibrhmkoz/go-kit/mutant"
	"github.com/ibrhmkoz/go-kit/mutant/killmatrix"
	"github.com/ibrhmkoz/go-kit/xtesting"
)

func writeModule(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// cancelOnMatch wraps a writer and cancels ctx the first time written output
// contains substr, letting tests cancel deterministically at runner stages.
type cancelOnMatch struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	substr string
	cancel context.CancelFunc
	done   bool
}

func (w *cancelOnMatch) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	if !w.done && strings.Contains(w.buf.String(), w.substr) {
		w.done = true
		w.cancel()
	}
	return len(p), nil
}

// cancelOnFile polls for marker and cancels ctx as soon as the fixture creates it.
func cancelOnFile(t *testing.T, marker string) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		for {
			if _, err := os.Stat(marker); err == nil {
				cancel()
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}()
	return ctx, cancel
}

func TestRunErrors(t *testing.T) {
	t.Parallel()

	t.Run("cancelled before load", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		r := killmatrix.Runner{Workers: 1, BaselineRuns: 1}
		if _, err := r.Run(ctx, "testdata", "./discount"); err == nil {
			t.Fatal("expected error for cancelled context, got nil")
		}
	})

	t.Run("package parse error", func(t *testing.T) {
		t.Parallel()
		dir := writeModule(t, map[string]string{
			"go.mod": "module parseerr\n\ngo 1.21\n",
			"bad.go": "package parseerr\nfunc f( {\n",
		})
		r := killmatrix.Runner{Workers: 1, BaselineRuns: 1}
		if _, err := r.Run(t.Context(), dir, "."); err == nil {
			t.Fatal("expected package error, got nil")
		}
	})

	t.Run("list tests fails", func(t *testing.T) {
		t.Parallel()
		dir := writeModule(t, map[string]string{
			"go.mod": "module listfail\n\ngo 1.21\n",
			"p.go":   "package listfail\nfunc F(x int) bool { return x > 0 }\n",
			"p_test.go": `package listfail
import ("os"; "testing")
func TestMain(m *testing.M) { os.Exit(1) }
func TestF(t *testing.T) { if !F(1) { t.Fatal() } }
`,
		})
		r := killmatrix.Runner{Workers: 1, BaselineRuns: 1, Schemata: true}
		if _, err := r.Run(t.Context(), dir, "."); err == nil {
			t.Fatal("expected list tests error, got nil")
		}
	})

	t.Run("cancel before buildSchema", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		r := killmatrix.Runner{
			Workers:      1,
			BaselineRuns: 1,
			Schemata:     true,
			Choose: func(_ string, _ []mutant.Mutant) []int {
				cancel()
				return []int{0}
			},
		}
		_, err := r.Run(ctx, "testdata", "./discount")
		if err == nil || strings.Contains(err.Error(), "baseline build") {
			t.Fatalf("err = %v, want buildSchema cancellation (not baseline build fallback)", err)
		}
	})

	// Cancel right after baseline finishes with Schemata=false: kills runMutant (L1201)
	// and runPackage worker error join (L569, L570).
	t.Run("cancel runMutant", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		r := killmatrix.Runner{
			Workers:      1,
			BaselineRuns: 1,
			Schemata:     false,
			Choose:       func(_ string, _ []mutant.Mutant) []int { return []int{0} },
			Log:          &cancelOnMatch{substr: "top-level tests", cancel: cancel},
		}
		if _, err := r.Run(ctx, "testdata", "./discount"); err == nil {
			t.Fatal("expected cancellation error in cancel runMutant, got nil")
		}
	})

	// Cancel during initial baseline (L500) and during parallel baseline recheck (L513).
	for _, tc := range []struct {
		name    string
		workers int
		body    string
	}{
		{"cancel baseline", 1, `_ = os.WriteFile("hit", nil, 0o600); time.Sleep(5 * time.Second)`},
		{"cancel baseline recheck", 2, `
	if _, err := os.Stat("once"); err != nil {
		_ = os.WriteFile("once", nil, 0o600)
		t.Fatal("fail run 1")
	}
	_ = os.WriteFile("hit", nil, 0o600)
	time.Sleep(5 * time.Second)
`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := writeModule(t, map[string]string{
				"go.mod": "module cbase\n\ngo 1.21\n",
				"p.go":   "package cbase\nfunc F(x int) bool { return x > 0 }\n",
				"p_test.go": fmt.Sprintf(`package cbase
import ("os"; "testing"; "time")
func TestA(t *testing.T) { %s; _ = F(1) }
`, tc.body),
			})
			ctx, cancel := cancelOnFile(t, filepath.Join(dir, "hit"))
			defer cancel()
			r := killmatrix.Runner{Workers: tc.workers, BaselineRuns: 1, Schemata: false, Choose: func(_ string, _ []mutant.Mutant) []int { return []int{0} }}
			if _, err := r.Run(ctx, dir, "."); err == nil {
				t.Fatalf("expected cancellation error in %s", tc.name)
			}
		})
	}
}

func TestPackageAndSchemaEdgeCases(t *testing.T) {
	t.Parallel()

	// Single multi-package module exercising package sorting, generated-file skip,
	// no-test package drop, cwd in list/run/runBatch, long & spoofed output,
	// package-var selection, schema retry & fallback, BuildMS sharing, and Log.
	t.Run("failfast multi-package paths", func(t *testing.T) {
		t.Parallel()
		dir := writeModule(t, map[string]string{
			"go.mod":        "module edges\n\ngo 1.21\n",
			"apkg/here.txt": "ok\n",
			"apkg/a.go": `package apkg
func Unused(x int) bool { return x > 0 }
var Closure = func(x int) bool { return x > 0 }
func Live(x int) bool { return x > 0 }
`,
			"apkg/a_test.go": `package apkg
import ("fmt"; "os"; "strings"; "testing")
func TestMain(m *testing.M) {
	if _, err := os.Stat("here.txt"); err != nil { os.Exit(1) }
	os.Exit(m.Run())
}
func TestA(t *testing.T) { if _, err := os.Stat("here.txt"); err != nil { t.Fatal(err) }; if !Live(5) { t.Fatal() } }
func TestB(t *testing.T) { if _, err := os.Stat("here.txt"); err != nil { t.Fatal(err) }; if !Closure(2) { t.Fatal() } }
func TestC(t *testing.T) { if _, err := os.Stat("here.txt"); err != nil { t.Fatal(err) }; if !Live(5) { t.Fatal() } }
func TestLong(t *testing.T) {
	t.Run("sub", func(t *testing.T) {
		fmt.Println(strings.Repeat("x", 70000))
		fmt.Println("FAIL: TestFake (0.00s)")
		if !Live(5) { t.Fatal() }
	})
}
`,
			"notest/n.go":   "package notest\nfunc N(x int) bool { return x > 0 }\n",
			"gen/g.go":      "// Code generated by test; DO NOT EDIT.\npackage gen\nfunc G(x int) bool { return x > 0 }\n",
			"gen/g_test.go": "package gen\nimport \"testing\"\nfunc TestG(t *testing.T) { if !G(1) { t.Fatal() } }\n",
			// Shadowing _mutOrd makes only a.go's ROR line fail schema compilation;
			// a.go builds individually (Schema=false, Compiled=true, BuildMS>0 -> L1081, L1199).
			"retry/a.go":      "package retry\nfunc Bad(x int) bool { _mutOrd := 0; _ = _mutOrd; return x > 0 }\n",
			"retry/b.go":      "package retry\nfunc Good(x int) bool { return x > 0 }\n",
			"retry/r_test.go": "package retry\nimport \"testing\"\nfunc TestBoth(t *testing.T) { if !Bad(1) || Bad(0) || !Good(1) || Good(0) { t.Fatal() } }\n",
			// Declaring _mutActive collides with zz_mutant_runtime.go -> full fallback (L683, L708, L710);
			// F embeds in schema attempt 1 (so L710 must clear Schema=false), while BadErr fails own build (L1205).
			"fallback/f.go": `package fallback
import "strconv"
type code string
func (c code) Error() string { return string(c) }
var _mutActive = 0
func F(x int) bool { return x > _mutActive }
func BadErr(n int) code { return code(strconv.Itoa(n)) }
`,
			"fallback/f_test.go": "package fallback\nimport \"testing\"\nfunc TestF(t *testing.T) { if !F(1) || F(0) { t.Fatal() }; _ = BadErr(1) }\n",
		})
		choose := func(file string, ms []mutant.Mutant) []int {
			var keep []int
			switch filepath.Base(filepath.Dir(file)) {
			case "apkg":
				for i, m := range ms {
					// Line 3 (Closure, outside any func): x >= 0 survives TestB so TestA must also run (kills L848).
					// Line 4 (Live, inside func): all 5 ROR mutants so apkg has 6 schema mutants (kills L687, L690).
					if (m.Pos.Line == 3 && m.Mutated == "x >= 0") ||
						(m.Pos.Line == 4 && m.Operator == mutant.ROR) {
						keep = append(keep, i)
					}
				}
				return keep
			case "retry":
				for i, m := range ms {
					if m.Operator == mutant.ROR && m.Mutated == "x < 0" {
						return []int{i}
					}
				}
				return nil
			case "fallback":
				for i, m := range ms {
					if (m.Pos.Line == 6 && m.Mutated == "x < _mutActive") || m.Operator == mutant.ERR {
						keep = append(keep, i)
					}
				}
				return keep
			default:
				return []int{0}
			}
		}
		var logBuf bytes.Buffer
		start := time.Now()
		r := killmatrix.Runner{
			Workers:      6,
			BaselineRuns: 3, // median keeps fast tests <50ms so TestA/TestC always batch into runBatch
			TimeoutPad:   500 * time.Millisecond,
			Schemata:     true,
			Select:       true,
			FailFast:     true,
			Confirm:      true,
			Log:          &logBuf,
			Choose:       choose,
		}
		// Pass retry before apkg to verify PkgPath sorting (L238).
		m := run(t, r, dir, "./retry", "./apkg", "./notest", "./gen", "./fallback")
		wallMS := float64(time.Since(start)) / float64(time.Millisecond)

		var pkgs, retrySchema, retryOwn []string
		var apkgSchemaBuild float64
		var liveSurvID int
		for _, mu := range m.Mutants {
			if !slices.Contains(pkgs, mu.Pkg) {
				pkgs = append(pkgs, mu.Pkg)
			}
			if mu.Pkg == "edges/apkg" {
				if mu.Schema {
					apkgSchemaBuild += mu.BuildMS
				}
				if mu.Line == 4 && mu.Mutated == "x >= 0" {
					liveSurvID = mu.ID
				}
			}
			if mu.Pkg == "edges/retry" {
				if mu.Schema {
					retrySchema = append(retrySchema, mu.File)
				} else {
					retryOwn = append(retryOwn, mu.File)
					if !mu.Compiled || mu.BuildMS <= 0 {
						t.Errorf("retry own mutant = %+v, want Compiled=true BuildMS>0", mu)
					}
				}
			}
			if mu.Pkg == "edges/fallback" && mu.Schema {
				t.Errorf("fallback mutant = %+v, want Schema=false", mu)
			}
		}
		xtesting.EqualText(t, strings.Join(pkgs, "\n"), `
			edges/apkg
			edges/fallback
			edges/retry
		`)
		xtesting.EqualText(t, fmt.Sprintf("schema=%v own=%v", retrySchema, retryOwn), "schema=[b.go] own=[a.go]")
		// apkg has 6 schema mutants; skipping n++ (L687) or multiplying (L690) blows up BuildMS.
		if apkgSchemaBuild > wallMS {
			t.Errorf("apkg sum(BuildMS)=%.1fms > wall(%.1fms)", apkgSchemaBuild, wallMS)
		}

		var apkgTests []string
		for _, tt := range m.Tests {
			if tt.Pkg == "edges/apkg" {
				apkgTests = append(apkgTests, tt.Name)
			}
		}
		xtesting.EqualText(t, strings.Join(apkgTests, "\n"), `
			TestA
			TestB
			TestC
			TestLong/sub
		`)

		v := m.Verdicts()
		if v[1] != killmatrix.VerdictSurvived || v[liveSurvID] != killmatrix.VerdictSurvived {
			t.Fatalf("apkg verdicts: M1=%s M%d=%s, want survived/survived", v[1], liveSurvID, v[liveSurvID])
		}
		sawClosureTestA := false
		for _, res := range m.Results {
			if res.MutantID == 1 && m.Tests[res.TestID-1].Name == "TestA" {
				sawClosureTestA = true
			}
			if res.MutantID == liveSurvID {
				if m.Tests[res.TestID-1].Name == "TestB" {
					t.Errorf("Select=true FailFast=true ran unreached TestB on Live's survivor")
				}
				// Wrong cmd.Dir in runBatch (L1277) would fail TestA/TestC in batch and turn them into FlakyKill.
				if res.Status != killmatrix.Survived {
					t.Errorf("apkg survivor M%d %s = %s, want survived", liveSurvID, m.Tests[res.TestID-1].Name, res.Status)
				}
			}
		}
		if !sawClosureTestA {
			t.Errorf("package-var Closure mutant M1 did not run TestA under Select=true")
		}
		for _, mu := range m.Mutants {
			if mu.Pkg == "edges/retry" && v[mu.ID] != killmatrix.VerdictKilled {
				t.Errorf("retry M%d = %s, want killed", mu.ID, v[mu.ID])
			}
		}
		logOut := logBuf.String()
		for _, want := range []string{"schema of", "in 2 attempt(s)", "schema build failed", "top-level tests", "M1 done", "build error"} {
			if !strings.Contains(logOut, want) {
				t.Errorf("log missing %q:\n%s", want, logOut)
			}
		}
	})

	// Package-level var mutant under FailFast=false, Select=true: fn[id] must be -1
	// (not missing -> 0, colliding with Unused) so TestA runs without Skipped=true (L734).
	t.Run("package var not skipped with select and failfast=false", func(t *testing.T) {
		t.Parallel()
		dir := writeModule(t, map[string]string{
			"go.mod": "module pkgvarnf\n\ngo 1.21\n",
			"p.go": `package pkgvarnf
func Unused(x int) bool { return x > 0 }
var Closure = func(x int) bool { return x > 0 }
`,
			"p_test.go": `package pkgvarnf
import "testing"
func TestA(t *testing.T) {}
`,
		})
		r := killmatrix.Runner{
			Workers:      1,
			BaselineRuns: 1,
			TimeoutPad:   500 * time.Millisecond,
			Schemata:     true,
			Select:       true,
			FailFast:     false,
			Choose: func(_ string, ms []mutant.Mutant) []int {
				for i, m := range ms {
					if m.Pos.Line == 3 && m.Mutated == "x < 0" {
						return []int{i}
					}
				}
				return nil
			},
		}
		m := run(t, r, dir, ".")
		if len(m.Results) != 1 || m.Results[0].Skipped {
			t.Fatalf("results = %+v, want 1 non-skipped result for package-var mutant", m.Results)
		}
	})

	t.Run("Matrix.String with tests and zero mutants", func(t *testing.T) {
		t.Parallel()
		m := &killmatrix.Matrix{
			Tests: []killmatrix.Test{{ID: 1, Pkg: "p", Name: "TestA", State: killmatrix.OK}},
		}
		xtesting.Prints(t, m, `
			p
			  T1 TestA
		`)
	})
}
