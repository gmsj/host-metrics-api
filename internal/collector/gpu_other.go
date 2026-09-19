//go:build !windows

package collector

import (
	"context"
	"os/exec"
	"syscall"
)

// nvidiaSMICommand builds the command. There is no console window to hide
// outside Windows; the only platform detail is how the process is stopped
// when the context ends: SIGTERM first, so nvidia-smi can release the driver
// handle cleanly, and SIGKILL only if it is still there after Cmd.WaitDelay.
// The default Cancel goes straight to SIGKILL.
func nvidiaSMICommand(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "nvidia-smi", nvidiaSMIArgs...)
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	return cmd
}
