//go:build unix

package gotest

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

// command starts name in a process group of its own, so that cancelling ctx
// kills whatever the process started too: a mutant can make a test leak a
// child that loops forever, and killing the test alone would orphan it.
func command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	// A leaked child holding stdout would keep Output waiting for it.
	cmd.WaitDelay = 100 * time.Millisecond
	return cmd
}

// output runs cmd from command and kills its process group once it exits,
// so that children it left behind do not outlive it.
func output(cmd *exec.Cmd) ([]byte, error) {
	out, err := cmd.Output()
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	return out, err
}
