//go:build linux

package collector

import (
	"testing"

	"github.com/shirou/gopsutil/v4/sensors"
)

func readings(kv ...any) []sensors.TemperatureStat {
	var out []sensors.TemperatureStat
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, sensors.TemperatureStat{SensorKey: kv[i].(string), Temperature: kv[i+1].(float64)})
	}
	return out
}

func TestSelectCPUTemp(t *testing.T) {
	tests := []struct {
		name    string
		in      []sensors.TemperatureStat
		want    *float64
		wantErr bool
	}{
		{
			// A Ryzen desktop: k10temp plus NVMe, NIC and Wi-Fi sensors that
			// must never be picked even though they come first.
			name: "AMD: Tctl when Tdie is absent, ignoring other hwmon chips",
			in:   readings("r8169_0_700:00_temp1", 40.5, "nvme_composite", 43.8, "k10temp_tctl", 42.6, "mt7921_phy0_temp1", 46.0),
			want: f(42.6),
		},
		{
			name: "AMD: Tdie wins over Tctl",
			in:   readings("k10temp_tctl", 52.0, "k10temp_tdie", 42.0),
			want: f(42),
		},
		{
			name: "Intel: package temperature, not a core",
			in:   readings("coretemp_core_0", 61.0, "coretemp_core_1", 59.0, "coretemp_package_id_0", 63.0),
			want: f(63),
		},
		{
			name: "ARM SoC",
			in:   readings("cpu_thermal", 48.2),
			want: f(48.2),
		},
		{
			name:    "only unrelated sensors: error, never a guess",
			in:      readings("nvme_composite", 43.8, "acpitz", 27.8),
			wantErr: true,
		},
		{
			name:    "no readings at all",
			in:      nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectCPUTemp(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", deref(got))
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !eqPtr(got, tt.want) {
				t.Errorf("got %v, want %v", deref(got), deref(tt.want))
			}
		})
	}
}
