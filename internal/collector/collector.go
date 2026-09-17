// Package collector reads raw metrics from the host and the GPU.
//
// Collectors never return errors to the caller. A metric that could not be
// read is left nil and the failure is logged (once, not every tick). The
// sampler turns nil into JSON null and keeps going with whatever it got, so a
// broken sensor never takes the agent down or hides the other fields.
package collector

import (
	"context"
	"log/slog"
	"sync"
)

// System produces host metrics (CPU, memory, disk, network, uptime).
//
// The sampler depends on this interface instead of on gopsutil directly so it
// can be tested with a fake that never touches hardware. In Go an interface is
// satisfied implicitly: any type with a matching Collect method is a System,
// no "implements" declaration needed.
type System interface {
	Collect(ctx context.Context) Sample
}

// GPU produces NVIDIA GPU metrics.
type GPU interface {
	Collect(ctx context.Context) GPUSample
}

// Sample holds the raw readings of one collection pass.
//
// Byte counters (DiskReadB, NetRxB, ...) are cumulative since boot. Turning
// them into rates requires the previous reading, and that state belongs to the
// sampler, not here.
type Sample struct {
	UptimeS *uint64

	CPUPct   *float64
	CoresPct []float64
	CPUTempC *float64
	Load1    *float64
	Load5    *float64
	Load15   *float64

	RAMTotalB  *uint64
	RAMAvailB  *uint64
	SwapTotalB *uint64
	SwapUsedB  *uint64

	DiskTotalB *uint64
	DiskUsedB  *uint64
	DiskReadB  *uint64
	DiskWriteB *uint64

	NetRxB *uint64
	NetTxB *uint64
}

// GPUSample holds one reading of nvidia-smi. When Present is false every other
// field is nil.
type GPUSample struct {
	Present      bool
	UtilPct      *float64
	TempC        *float64
	VRAMUsedMiB  *float64
	VRAMTotalMiB *float64
	PowerW       *float64
	FanPct       *float64
	ClockMHz     *float64
}

// Static holds values that do not change while the agent runs. They are read
// once at startup: on Windows they come from WMI queries that cost tens to
// hundreds of milliseconds each, far too slow to repeat every tick.
type Static struct {
	Host       string
	OS         string
	CPUFreqMHz *float64
}

// faultTracker keeps the log quiet: the first failure of a given source is a
// warning, repeats are debug, and recovery is an info line.
type faultTracker struct {
	log     *slog.Logger
	mu      sync.Mutex
	failing map[string]bool
}

func newFaultTracker(log *slog.Logger) *faultTracker {
	return &faultTracker{log: log, failing: make(map[string]bool)}
}

func (f *faultTracker) fail(source string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing[source] {
		f.log.Debug("collector still failing", "source", source, "err", err)
		return
	}
	f.failing[source] = true
	f.log.Warn("collector failed; field will be null until it recovers", "source", source, "err", err)
}

func (f *faultTracker) ok(source string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing[source] {
		delete(f.failing, source)
		f.log.Info("collector recovered", "source", source)
	}
}

func ptr[T any](v T) *T { return &v }
