package sampler

import (
	"math"
	"time"
)

// Rate turns two readings of a cumulative byte counter into bytes per second.
//
// ok is false when no honest number exists: a reading is missing, no time
// elapsed, or the counter went backwards. Counters go backwards when an
// interface is re-created, a driver reloads or a 32-bit counter wraps; a
// naive subtraction would then publish a huge bogus value (or a negative
// one). The caller publishes null for that tick and the next tick has a
// fresh baseline.
func Rate(prev, cur *uint64, elapsed time.Duration) (bytesPerSec float64, ok bool) {
	if prev == nil || cur == nil || elapsed <= 0 || *cur < *prev {
		return 0, false
	}
	return float64(*cur-*prev) / elapsed.Seconds(), true
}

// Unit conversions. Rates are decimal (SI), capacities are binary, matching
// what the OS tools the user already knows display. See model.Stats.
const (
	mib = 1024 * 1024
	gib = 1024 * 1024 * 1024
)

func bytesToMbps(bytesPerSec float64) float64 { return bytesPerSec * 8 / 1e6 }
func bytesToMBs(bytesPerSec float64) float64  { return bytesPerSec / 1e6 }
func bytesToGiB(b uint64) float64             { return float64(b) / gib }

// round1 rounds to one decimal place. It keeps the payload short for the
// embedded consumer and hides float noise like 23.400000000000002.
func round1(v float64) float64 { return math.Round(v*10) / 10 }

// pct returns part/total as a percentage, or 0 when total is 0 (a machine
// without swap is "0% swap used", not "undefined").
func pct(part, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total) * 100
}
