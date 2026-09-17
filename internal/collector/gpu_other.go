//go:build !windows

package collector

import (
	"context"
	"os/exec"
)

// nvidiaSMICommand builds the command. Nothing special is needed outside
// Windows: there is no console window to hide.
func nvidiaSMICommand(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, "nvidia-smi", nvidiaSMIArgs...)
}
