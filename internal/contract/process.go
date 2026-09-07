package contract

import (
	"context"
	"os/exec"
	"time"
)

const commandWaitDelay = time.Second

func newBoundedCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = nil
	cmd.WaitDelay = commandWaitDelay
	configureProcessGroup(cmd)
	return cmd
}

func stopCommand(cmd *exec.Cmd) {
	if cmd.Cancel != nil {
		_ = cmd.Cancel()
		return
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
