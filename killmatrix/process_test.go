//go:build unix

package killmatrix

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A mutant can make a test hang or leak the processes it starts; killmatrix's
// own tests start fixture binaries, so mutating killmatrix does both. Every
// process a test starts must be gone once its run returns.
func TestOutputLeavesNoProcess(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit time.Duration
		// script prints the pid of a child that would outlive it.
		script string
	}{
		{"timed out", 200 * time.Millisecond, `sleep 300 & echo $!; exec sleep 300`},
		{"exited", 0, `sleep 300 & echo $!`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.limit > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.limit)
				defer cancel()
			}
			start := time.Now()
			out, _ := output(command(ctx, "sh", "-c", tc.script))
			if d := time.Since(start); d > 5*time.Second {
				t.Errorf("output took %v: it waited for the leaked child", d)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
			if err != nil {
				t.Fatalf("output %q: %v", out, err)
			}
			// SIGKILL is delivered asynchronously, and launchd or init reaps the
			// orphan a moment later.
			deadline := time.Now().Add(2 * time.Second)
			for !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
				if time.Now().After(deadline) {
					_ = syscall.Kill(pid, syscall.SIGKILL)
					t.Fatalf("child %d still running after output returned", pid)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

// TestArgsStopAHangingTest: a test binary must stop by itself even if no
// one is left to kill it. Mutating killmatrix runs killmatrix's own tests,
// and a mutant that breaks its timeout handling would otherwise leave a
// looping fixture running for hours.
func TestArgsStopAHangingTest(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, src := range map[string]string{
		"go.mod":       "module hang\n\ngo 1.21\n",
		"hang_test.go": "package hang\n\nimport \"testing\"\n\nfunc TestHang(t *testing.T) {\n\tfor {\n\t}\n}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "hang.test")
	if out, err := build(t.Context(), dir, bin, ""); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// No context: nothing but the binary itself can stop it.
	cmd := exec.Command(bin, testArgs("^TestHang$", 200*time.Millisecond)...)
	cmd.Dir = dir
	start := time.Now()
	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("the looping test passed; want it to fail on its deadline")
		}
		if d := time.Since(start); d < 200*time.Millisecond {
			t.Errorf("stopped after %v, before killmatrix's own limit", d)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("still running after 10s")
	}
}
