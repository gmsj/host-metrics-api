//go:build windows

package collector

import (
	"context"
	"os"
)

// defaultDiskPath returns the volume Windows booted from. It is C:\ on almost
// every machine, but not on all of them, and the OS tells us which.
func defaultDiskPath() string {
	if drive := os.Getenv("SystemDrive"); drive != "" {
		return drive + `\`
	}
	return `C:\`
}

// ioDiskNames returns nil, meaning "every device gopsutil reports". On
// Windows gopsutil enumerates logical drives (C:, D:, ...) through
// IOCTL_DISK_PERFORMANCE, so there is no whole-disk/partition double
// counting to filter out.
func ioDiskNames(_ context.Context) ([]string, error) { return nil, nil }
