//go:build !linux && !windows

package collector

import "context"

// Linux and Windows are the only supported targets. These stubs exist so the
// package still compiles on a developer's macOS or BSD machine.

func defaultDiskPath() string { return "/" }

func ioDiskNames(_ context.Context) ([]string, error) { return nil, nil }
