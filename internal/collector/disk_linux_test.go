//go:build linux

package collector

import (
	"slices"
	"testing"
)

func TestPhysicalBlockDevices(t *testing.T) {
	// A typical desktop /sys/block: one NVMe, one SATA disk, snap loop
	// devices, an LVM volume and a zram swap. Only the first two have a
	// "device" symlink.
	sysBlock := []string{"loop0", "loop1", "dm-0", "zram0", "nvme0n1", "sda"}
	hasDevice := func(name string) bool { return name == "nvme0n1" || name == "sda" }

	got, err := physicalBlockDevices(sysBlock, hasDevice)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"nvme0n1", "sda"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPhysicalBlockDevicesNoneIsAnError(t *testing.T) {
	// A container or VM where /sys/block shows only virtual devices: better
	// to disable disk I/O rates than to double count partitions.
	_, err := physicalBlockDevices([]string{"loop0", "dm-0"}, func(string) bool { return false })
	if err == nil {
		t.Fatal("expected an error when nothing is physical")
	}
	if _, err := physicalBlockDevices(nil, func(string) bool { return true }); err == nil {
		t.Fatal("expected an error for an empty /sys/block")
	}
}
