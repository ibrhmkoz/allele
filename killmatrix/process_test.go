//go:build unix

package killmatrix

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ibrhmkoz/allele/pkg/gotest"
)

// TestCancelHelpers builds one tiny test binary and exercises the OS-process
// cancellation and cwd paths in recheck, runTests, runTestsFailFast, runBatch,
// and recordSingle without paying for ten separate go test -c builds.
func TestCancelHelpers(t *testing.T) {
	t.Parallel()
	srcDir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod": "module chelper\n\ngo 1.21\n",
		"p_test.go": `package chelper
import ("os"; "strings"; "testing"; "time")
func mode() string { b, _ := os.ReadFile("mode.txt"); return strings.TrimSpace(string(b)) }
func bump() int {
	b, _ := os.ReadFile("step")
	_ = os.WriteFile("step", append(b, 'x'), 0o600)
	return len(b)
}
func block() { _ = os.WriteFile("hit", nil, 0o600); time.Sleep(5 * time.Second) }

func TestA(t *testing.T) {
	if _, err := os.Stat("mode.txt"); err != nil { t.Fatal(err) }
	switch mode() {
	case "fail-always":
		t.Fatal("always")
	case "fail-then-block":
		if bump() == 0 { t.Fatal("first") }
		block()
	case "timeout-then-block":
		if bump() == 0 { time.Sleep(5 * time.Second) }
		block()
	case "crash-then-block":
		if bump() == 0 { go func() { panic("boom") }(); select {} }
		block()
	case "block-always":
		block()
	case "pass-then-z", "pass-then-z-fail", "require-cwd":
		return
	}
}

func TestZ(t *testing.T) {
	if _, err := os.Stat("mode.txt"); err != nil { t.Fatal(err) }
	switch mode() {
	case "require-cwd":
		return
	case "pass-then-z-fail":
		t.Fatal("z failed")
	case "pass-then-z":
		switch bump() {
		case 0:
			time.Sleep(5 * time.Second)
		case 1:
			t.Fatal("fail after timeout")
		default:
			block()
		}
	case "pass-then-z-to":
		if bump() == 0 { time.Sleep(5 * time.Second) }
		block()
	}
}
`,
	} {
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(srcDir, "chelper.test")
	if out, err := gotest.Build(t.Context(), srcDir, bin, ""); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	// Warm macOS AMFI code-signature check once so tight timeout subtests start in ~3ms.
	if _, err := gotest.List(t.Context(), srcDir, bin); err != nil {
		t.Fatalf("warm list: %v", err)
	}

	makeRunDir := func(t *testing.T, mode string) (string, context.Context, context.CancelFunc) {
		t.Helper()
		d := t.TempDir()
		if err := os.WriteFile(filepath.Join(d, "mode.txt"), []byte(mode+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		marker := filepath.Join(d, "hit")
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
		return d, ctx, cancel
	}

	newRunner := func(ff bool, pad time.Duration) Runner {
		r := Runner{Workers: 1, BaselineRuns: 1, TimeoutFactor: 1, TimeoutPad: pad, FailFast: ff, Confirm: true, Log: io.Discard}
		r.sem = make(chan struct{}, 1)
		return r
	}

	t.Run("recheck", func(t *testing.T) {
		t.Parallel()
		d, ctx, cancel := makeRunDir(t, "block-always")
		defer cancel()
		r := newRunner(false, 500*time.Millisecond)
		tests := []Test{{ID: 1, Name: "TestA", BaselineMS: 5, State: OK}}
		muts := []Mutant{{ID: 1, Compiled: true, Schema: true}}
		res := [][]Result{{{MutantID: 1, TestID: 1, Status: recheck}}}
		if err := r.recheck(ctx, d, bin, tests, res, muts); err == nil {
			t.Fatal("expected cancellation error in recheck")
		}
	})

	t.Run("runBatch uses dir", func(t *testing.T) {
		t.Parallel()
		d, ctx, cancel := makeRunDir(t, "require-cwd")
		defer cancel()
		rep := newRunner(true, 0).runBatch(ctx, d, bin, []string{"TestA", "TestZ"}, 500*time.Millisecond, 0)
		if !rep.OK {
			t.Fatalf("runBatch failed in %s: %+v", d, rep)
		}
	})

	t.Run("runTests cancelled", func(t *testing.T) {
		t.Parallel()
		d, ctx, cancel := makeRunDir(t, "block-always")
		defer cancel()
		r := newRunner(false, 500*time.Millisecond)
		r.Confirm = false
		tests := []Test{{ID: 1, Name: "TestA", BaselineMS: 5, State: OK}}
		mu := Mutant{ID: 1, Compiled: true, Schema: true}
		if _, err := r.runTests(ctx, d, bin, &mu, tests, nil, nil, nil); err == nil {
			t.Fatal("expected cancellation error in runTests cancelled")
		}
	})

	t.Run("runTests confirm non-schema and recheck", func(t *testing.T) {
		t.Parallel()
		d, ctx, cancel := makeRunDir(t, "fail-always")
		defer cancel()
		var logBuf bytes.Buffer
		r := newRunner(false, 500*time.Millisecond)
		r.Log = &logBuf
		tests := []Test{{ID: 1, Name: "TestA", BaselineMS: 5, State: OK}}
		muNoSchema := Mutant{ID: 1, Compiled: true, Schema: false}
		res, err := r.runTests(ctx, d, bin, &muNoSchema, tests, nil, nil, nil)
		if err != nil || len(res) != 1 || res[0].Status != Killed {
			t.Fatalf("non-schema: res=%+v err=%v, want Killed", res, err)
		}
		if !strings.Contains(logBuf.String(), "M1 done") {
			t.Errorf("log = %q, want M1 done", logBuf.String())
		}
		muSchema := Mutant{ID: 2, Compiled: true, Schema: true}
		res2, err := r.runTests(ctx, d, bin, &muSchema, tests, nil, nil, nil)
		if err != nil || len(res2) != 1 || res2[0].Status != recheck {
			t.Fatalf("schema original-fail: res=%+v err=%v, want recheck", res2, err)
		}
	})

	t.Run("runTests confirm", func(t *testing.T) {
		t.Parallel()
		d, ctx, cancel := makeRunDir(t, "fail-then-block")
		defer cancel()
		r := newRunner(false, 500*time.Millisecond)
		tests := []Test{{ID: 1, Name: "TestA", BaselineMS: 5, State: OK}}
		mu := Mutant{ID: 1, Compiled: true, Schema: false}
		if _, err := r.runTests(ctx, d, bin, &mu, tests, nil, nil, nil); err == nil {
			t.Fatal("expected cancellation error in runTests confirm")
		}
	})

	t.Run("failfast batch cancelled", func(t *testing.T) {
		t.Parallel()
		d, ctx, cancel := makeRunDir(t, "block-always")
		defer cancel()
		r := newRunner(true, 500*time.Millisecond)
		r.Confirm = false
		tests := []Test{{ID: 1, Name: "TestA", BaselineMS: 5, State: OK}}
		mu := Mutant{ID: 1, Compiled: true, Schema: true}
		if _, err := r.runTestsFailFast(ctx, d, bin, &mu, tests, nil, nil, nil); err == nil {
			t.Fatal("expected cancellation error in failfast batch cancelled")
		}
	})

	t.Run("failfast single kill confirm", func(t *testing.T) {
		t.Parallel()
		d, ctx, cancel := makeRunDir(t, "fail-then-block")
		defer cancel()
		r := newRunner(true, 500*time.Millisecond)
		tests := []Test{{ID: 1, Name: "TestA", BaselineMS: 5, State: OK}}
		mu := Mutant{ID: 1, Compiled: true, Schema: true}
		if _, err := r.runTestsFailFast(ctx, d, bin, &mu, tests, nil, nil, nil); err == nil {
			t.Fatal("expected cancellation error in single kill confirm")
		}
	})

	t.Run("failfast single timeout recheck", func(t *testing.T) {
		t.Parallel()
		d, ctx, cancel := makeRunDir(t, "timeout-then-block")
		defer cancel()
		r := newRunner(true, 150*time.Millisecond)
		tests := []Test{{ID: 1, Name: "TestA", BaselineMS: 5, State: OK}}
		mu := Mutant{ID: 1, Compiled: true, Schema: true}
		if _, err := r.runTestsFailFast(ctx, d, bin, &mu, tests, nil, nil, nil); err == nil {
			t.Fatal("expected cancellation error in single timeout recheck")
		}
	})

	t.Run("failfast multi records prior pass", func(t *testing.T) {
		t.Parallel()
		d, ctx, cancel := makeRunDir(t, "pass-then-z-fail")
		defer cancel()
		r := newRunner(true, 500*time.Millisecond)
		r.Confirm = false
		tests := []Test{
			{ID: 1, Name: "TestA", BaselineMS: 5, State: OK},
			{ID: 2, Name: "TestZ", BaselineMS: 6, State: OK},
		}
		mu := Mutant{ID: 1, Compiled: true, Schema: true}
		res, err := r.runTestsFailFast(ctx, d, bin, &mu, tests, nil, nil, nil)
		if err != nil || len(res) != 2 || res[0].TestID != 1 || res[0].Status != Survived || res[1].TestID != 2 || res[1].Status != Killed {
			t.Fatalf("res=%+v err=%v, want TestA=Survived then TestZ=Killed", res, err)
		}
	})

	t.Run("failfast multi fail confirm", func(t *testing.T) {
		t.Parallel()
		d, ctx, cancel := makeRunDir(t, "fail-then-block")
		defer cancel()
		r := newRunner(true, 500*time.Millisecond)
		tests := []Test{
			{ID: 1, Name: "TestA", BaselineMS: 5, State: OK},
			{ID: 2, Name: "TestZ", BaselineMS: 6, State: OK},
		}
		mu := Mutant{ID: 1, Compiled: true, Schema: true}
		if _, err := r.runTestsFailFast(ctx, d, bin, &mu, tests, nil, nil, nil); err == nil {
			t.Fatal("expected cancellation error in multi fail confirm")
		}
	})

	t.Run("failfast multi timeout recheck", func(t *testing.T) {
		t.Parallel()
		d, ctx, cancel := makeRunDir(t, "pass-then-z-to")
		defer cancel()
		r := newRunner(true, 150*time.Millisecond)
		tests := []Test{
			{ID: 1, Name: "TestA", BaselineMS: 5, State: OK},
			{ID: 2, Name: "TestZ", BaselineMS: 6, State: OK},
		}
		mu := Mutant{ID: 1, Compiled: true, Schema: true}
		if _, err := r.runTestsFailFast(ctx, d, bin, &mu, tests, nil, nil, nil); err == nil {
			t.Fatal("expected cancellation error in multi timeout recheck")
		}
	})

	t.Run("failfast multi timeout then kill", func(t *testing.T) {
		t.Parallel()
		d, ctx, cancel := makeRunDir(t, "pass-then-z")
		defer cancel()
		r := newRunner(true, 150*time.Millisecond)
		tests := []Test{
			{ID: 1, Name: "TestA", BaselineMS: 5, State: OK},
			{ID: 2, Name: "TestZ", BaselineMS: 6, State: OK},
		}
		mu := Mutant{ID: 1, Compiled: true, Schema: true}
		if _, err := r.runTestsFailFast(ctx, d, bin, &mu, tests, nil, nil, nil); err == nil {
			t.Fatal("expected cancellation error in multi timeout then kill")
		}
	})

	t.Run("failfast multi crash confirm", func(t *testing.T) {
		t.Parallel()
		d, ctx, cancel := makeRunDir(t, "crash-then-block")
		defer cancel()
		r := newRunner(true, 500*time.Millisecond)
		tests := []Test{
			{ID: 1, Name: "TestA", BaselineMS: 5, State: OK},
			{ID: 2, Name: "TestZ", BaselineMS: 6, State: OK},
		}
		mu := Mutant{ID: 1, Compiled: true, Schema: true}
		if _, err := r.runTestsFailFast(ctx, d, bin, &mu, tests, nil, nil, nil); err == nil {
			t.Fatal("expected cancellation error in multi crash confirm")
		}
	})
}
