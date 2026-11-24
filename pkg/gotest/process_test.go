//go:build unix

package gotest

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A test can hang or leak the processes it starts, and a mutation tool's
// changes make both likely. Every process a test starts must be gone once
// its run returns.
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
// one is left to kill it: a mutation tool run on its own tests can break
// the code that would, and leave a looping fixture running for hours.
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
	if out, err := Build(t.Context(), dir, bin, ""); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// The binary's own deadline is twice the limit and a second, past Run's.
	if args := runArgs(Options{Tests: []string{"TestHang"}, Limit: time.Second}); !slices.Contains(args, "-test.timeout=3s") {
		t.Errorf("runArgs with a 1s limit = %v, want -test.timeout=3s", args)
	}
	// No limit uses go test's 10-minute default deadline.
	if args := runArgs(Options{Tests: []string{"TestHang"}}); !slices.Contains(args, "-test.timeout=10m0s") {
		t.Errorf("runArgs with no limit = %v, want -test.timeout=10m0s", args)
	}
	// No context: nothing but the binary itself can stop it.
	cmd := exec.Command(bin, runArgs(Options{Tests: []string{"TestHang"}, Limit: 200 * time.Millisecond})...)
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
			t.Errorf("stopped after %v, before Run's own limit", d)
		}
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("still running after 3s")
	}
}

// TestCommandCancelKillsGroup verifies command's Cancel hook kills the whole
// process group on timeout without relying on output()'s post-exit cleanup.
func TestCommandCancelKillsGroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	out, _ := command(ctx, "sh", "-c", `sleep 300 & echo $!; exec sleep 300`).Output()
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("output %q: %v", out, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild %d still running after command cancelled", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestBuildArgs(t *testing.T) {
	if got, want := strings.Join(buildArgs("p.test", ""), " "), "test -c -vet=off -ldflags=-s -w -o p.test"; got != want {
		t.Errorf("without an overlay: %s, want %s", got, want)
	}
	if got, want := strings.Join(buildArgs("p.test", "o.json"), " "), "test -c -vet=off -ldflags=-s -w -o p.test -overlay o.json"; got != want {
		t.Errorf("with an overlay: %s, want %s", got, want)
	}
}
