//go:build windows

package collector

import (
	"context"
	"os/exec"
	"syscall"
)

// createNoWindow is the CREATE_NO_WINDOW process creation flag. The syscall
// package does not name it; golang.org/x/sys/windows does, but importing that
// only for one constant is not worth it.
const createNoWindow = 0x08000000

// nvidiaSMICommand builds the command without flashing a console window.
//
// When the agent itself has no console (started by Task Scheduler with
// "hidden", or by a service wrapper) a child console program creates a new
// console that pops up for ~200 ms every tick. HideWindow hides that window
// via STARTUPINFO; CREATE_NO_WINDOW prevents it from being created at all.
// Both are set because they cover different launch situations.
func nvidiaSMICommand(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "nvidia-smi", nvidiaSMIArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	return cmd
}
