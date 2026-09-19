//go:build linux

package collector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

func defaultDiskPath() string { return "/" }

// ioDiskNames returns the physical block devices whose I/O should be summed.
//
// /proc/diskstats (what gopsutil reads) lists whole disks, every partition,
// loop devices, device-mapper and md volumes. Summing all of them counts a
// write to nvme0n1p2 twice (once on the partition, once on nvme0n1), and again
// if it sits on LVM or LUKS. Real hardware is the only layer where each byte
// appears once, and a device is real hardware exactly when it has a "device"
// symlink under /sys/block. loop*, dm-*, md*, zram* do not.
func ioDiskNames(_ context.Context) ([]string, error) {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return physicalBlockDevices(names, func(name string) bool {
		_, err := os.Lstat(filepath.Join("/sys/block", name, "device"))
		return err == nil
	})
}

// physicalBlockDevices keeps the entries of /sys/block that hasDevice says
// are backed by hardware. Split from ioDiskNames so the selection can be
// tested with a scripted /sys/block instead of the real one.
func physicalBlockDevices(names []string, hasDevice func(name string) bool) ([]string, error) {
	var physical []string
	for _, name := range names {
		if hasDevice(name) {
			physical = append(physical, name)
		}
	}
	if len(physical) == 0 {
		return nil, errors.New("no physical block device found under /sys/block")
	}
	return physical, nil
}
