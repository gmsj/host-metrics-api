package collector

import (
	"testing"

	gnet "github.com/shirou/gopsutil/v4/net"
)

func iface(name string, flags ...string) gnet.InterfaceStat {
	return gnet.InterfaceStat{Name: name, Flags: flags}
}

func counter(name string, rx, tx uint64) gnet.IOCountersStat {
	return gnet.IOCountersStat{Name: name, BytesRecv: rx, BytesSent: tx}
}

func TestBusiestInterface(t *testing.T) {
	tests := []struct {
		name     string
		ifaces   []gnet.InterfaceStat
		counters []gnet.IOCountersStat
		want     string
	}{
		{
			// Loopback has moved the most bytes (local dashboards, docker
			// registries); it must never be chosen.
			name:     "loopback is skipped even when busiest",
			ifaces:   []gnet.InterfaceStat{iface("lo", "up", "loopback"), iface("enp7s0", "up", "broadcast")},
			counters: []gnet.IOCountersStat{counter("lo", 900e9, 900e9), counter("enp7s0", 40e9, 3e9)},
			want:     "enp7s0",
		},
		{
			name:   "wifi vs ethernet: cumulative rx+tx decides",
			ifaces: []gnet.InterfaceStat{iface("lo", "loopback"), iface("wlp6s0"), iface("enp7s0")},
			counters: []gnet.IOCountersStat{
				counter("enp7s0", 10e9, 1e9),
				counter("wlp6s0", 6e9, 6e9),
			},
			want: "wlp6s0",
		},
		{
			name:     "windows names work the same",
			ifaces:   []gnet.InterfaceStat{iface("Loopback Pseudo-Interface 1", "loopback"), iface("Ethernet"), iface("Wi-Fi")},
			counters: []gnet.IOCountersStat{counter("Ethernet", 5e9, 5e9), counter("Wi-Fi", 1e9, 1e9)},
			want:     "Ethernet",
		},
		{
			// Tie or all zeros right after boot: the first one listed wins,
			// which is stable across runs.
			name:     "all idle picks the first non-loopback",
			ifaces:   []gnet.InterfaceStat{iface("lo", "loopback")},
			counters: []gnet.IOCountersStat{counter("lo", 0, 0), counter("eth0", 0, 0), counter("eth1", 0, 0)},
			want:     "eth0",
		},
		{
			name:     "only loopback: nothing to monitor",
			ifaces:   []gnet.InterfaceStat{iface("lo", "loopback")},
			counters: []gnet.IOCountersStat{counter("lo", 1e6, 1e6)},
			want:     "",
		},
		{
			name: "no counters at all",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := busiestInterface(tt.ifaces, tt.counters); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
