//go:build unix

package killmatrix

import (
	"context"
	"errors"
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
