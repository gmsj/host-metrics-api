//go:build !linux

package collector

import "context"

// cpuTemperature always reports "unavailable" outside Linux.
//
// On Windows gopsutil's sensors package queries the WMI class
// MSAcpi_ThermalZoneTemperature, which on most modern desktops either needs
// elevation, reports an ACPI thermal zone (motherboard/chassis), or returns
// nothing at all. It is never the CPU core temperature. Guessing or filtering
// heuristically would produce a number that looks right and is not, so
// cpu_temp_c is null on Windows by design.
func cpuTemperature(_ context.Context) (*float64, error) { return nil, nil }
