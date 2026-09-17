package sampler

import (
	"math"
	"testing"
	"time"
)

func u(v uint64) *uint64 { return &v }

func TestRate(t *testing.T) {
	tests := []struct {
		name    string
		prev    *uint64
		cur     *uint64
		elapsed time.Duration
		want    float64
		wantOK  bool
	}{
		{"one second, one megabyte", u(1_000_000), u(2_000_000), time.Second, 1_000_000, true},
		{"two seconds halves the rate", u(0), u(2_000_000), 2 * time.Second, 1_000_000, true},
		{"sub-second interval", u(0), u(500_000), 500 * time.Millisecond, 1_000_000, true},
		{"no change is zero, still ok", u(42), u(42), time.Second, 0, true},
		{"counter reset (reboot of iface) is not a negative number", u(5_000_000), u(1_000), time.Second, 0, false},
		{"32-bit wraparound is treated as reset", u(math.MaxUint32 - 10), u(5), time.Second, 0, false},
		{"missing previous reading", nil, u(10), time.Second, 0, false},
		{"missing current reading", u(10), nil, time.Second, 0, false},
		{"zero elapsed", u(0), u(10), 0, 0, false},
		{"negative elapsed (clock jump)", u(0), u(10), -time.Second, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Rate(tt.prev, tt.cur, tt.elapsed)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("rate = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUnitConversions(t *testing.T) {
	// 12.5 MB/s of network traffic is 100 Mbit/s: bits, decimal.
	if got := bytesToMbps(12_500_000); got != 100 {
		t.Errorf("bytesToMbps = %v, want 100", got)
	}
	if got := bytesToMBs(4_200_000); got != 4.2 {
		t.Errorf("bytesToMBs = %v, want 4.2", got)
	}
	// 32 GiB of RAM must read as 32.0, not 34.4.
	if got := round1(bytesToGiB(32 * gib)); got != 32 {
		t.Errorf("bytesToGiB = %v, want 32", got)
	}
	if got := round1(23.4499999); got != 23.4 {
		t.Errorf("round1 = %v", got)
	}
	if got := pct(0, 0); got != 0 {
		t.Errorf("pct(0,0) = %v, want 0", got)
	}
	if got := pct(1, 4); got != 25 {
		t.Errorf("pct(1,4) = %v, want 25", got)
	}
}
