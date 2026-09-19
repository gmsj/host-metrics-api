//go:build linux

package collector

import (
	"context"
	"errors"

	"github.com/shirou/gopsutil/v4/sensors"
)

// cpuSensorPriority lists the hwmon sensor keys that represent the CPU
// package/die temperature, most trustworthy first. The keys are the ones
// gopsutil v4 builds from hwmon: "<driver>_<label lowercased, spaces to _>".
//
//   - k10temp (AMD): Tdie is the die temperature; Tctl is the same reading
//     plus a control offset that is zero on most desktop Ryzen parts, and on
//     many boards Tctl is the only label exposed.
//   - coretemp (Intel): "Package id 0" is the package temperature.
//
// A generic "first sensor found" heuristic is wrong on purpose: hwmon also
// exposes NVMe, Wi-Fi and NIC temperatures, and picking one of those would
// look plausible and be meaningless.
var cpuSensorPriority = []string{
	"k10temp_tdie",
	"k10temp_tctl",
	"zenpower_tdie",
	"coretemp_package_id_0",
	"cpu_thermal", // ARM SoCs, in case the agent ever runs on one
}

// cpuTemperature reads the CPU temperature from hwmon. In gopsutil v4 this
// lives in the sensors package (it was host.SensorsTemperatures in v3).
func cpuTemperature(ctx context.Context) (*float64, error) {
	readings, err := sensors.TemperaturesWithContext(ctx)
	// gopsutil returns partial results together with a *sensors.Warnings
	// error when some hwmon files are unreadable. Use whatever came back.
	if err != nil && len(readings) == 0 {
		return nil, err
	}
	return selectCPUTemp(readings)
}

// selectCPUTemp picks the CPU temperature out of every hwmon reading using
// cpuSensorPriority. Pure: it is the part worth testing, with readings that
// look like the machines this agent runs on.
func selectCPUTemp(readings []sensors.TemperatureStat) (*float64, error) {
	byKey := make(map[string]float64, len(readings))
	for _, r := range readings {
		byKey[r.SensorKey] = r.Temperature
	}
	for _, key := range cpuSensorPriority {
		if t, ok := byKey[key]; ok {
			return ptr(t), nil
		}
	}
	return nil, errors.New("no known CPU temperature sensor in hwmon")
}
