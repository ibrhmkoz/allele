//go:build !unix

package gotest

import (
	"context"
	"os/exec"
)

func command(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

func output(cmd *exec.Cmd) ([]byte, error) { return cmd.Output() }
