// Package sampler runs the collection loop and publishes immutable snapshots.
//
// This is the heart of the design: one goroutine ticks at a fixed interval,
// asks the collectors for raw readings, derives rates from the previous
// readings, and stores a finished *model.Snapshot in an atomic.Pointer. HTTP
// handlers only Load() that pointer. Consequences:
//
//   - cpu_pct and the rates always cover exactly one interval, regardless of
//     how many clients ask or how late they ask;
//   - the cost of collection is constant and independent of the number of
//     consumers;
//   - a request never waits on nvidia-smi or any syscall.
package sampler

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gmsj/host-metrics-api/internal/collector"
	"github.com/gmsj/host-metrics-api/internal/model"
)

// Config is what the sampler needs besides its collectors.
type Config struct {
	Interval time.Duration
	Version  string
	Static   collector.Static
}

// Sampler owns the collection loop. Create it with New, run it with Run.
type Sampler struct {
	log *slog.Logger
	sys collector.System
	gpu collector.GPU
	cfg Config
	now func() time.Time

	// snap is the published state. atomic.Pointer gives lock-free reads for
	// any number of HTTP handlers while the sampler swaps in a new pointer
	// once per tick. The pointed-to Snapshot is never mutated after Store,
	// which is what makes this safe without a mutex.
	snap atomic.Pointer[model.Snapshot]

	// prev holds the previous tick's cumulative counters. Only the sampler
	// goroutine touches it, so no synchronization is needed.
	prev counters
}

type counters struct {
	at        time.Time
	diskRead  *uint64
	diskWrite *uint64
	netRx     *uint64
	netTx     *uint64
}

// New wires a sampler to its collectors. Nothing runs until Run is called.
func New(log *slog.Logger, sys collector.System, gpu collector.GPU, cfg Config) *Sampler {
	return &Sampler{log: log, sys: sys, gpu: gpu, cfg: cfg, now: time.Now}
}

// Snapshot returns the latest published snapshot, or nil before the first
// tick completes. The returned value must be treated as read-only.
func (s *Sampler) Snapshot() *model.Snapshot {
	return s.snap.Load()
}

// Run blocks until ctx is cancelled.
func (s *Sampler) Run(ctx context.Context) {
	// Priming pass. cpu.Percent needs a baseline call, and the rates need a
	// previous counter reading; without this the first snapshot would show
	// cpu_pct = 0 and null rates. The result is discarded, so the first
	// published snapshot, one interval later, is already correct. Until then
	// /stats and /healthz answer 503.
	s.prime(ctx)

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	s.log.Info("sampler started", "interval", s.cfg.Interval)
	for {
		select {
		case <-ctx.Done():
			s.log.Info("sampler stopped")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Sampler) prime(ctx context.Context) {
	now := s.now()
	sys, _ := s.collect(ctx)
	s.remember(now, sys)
}

// tick performs one full collection and publishes the result.
func (s *Sampler) tick(ctx context.Context) {
	now := s.now()
	sys, gpu := s.collect(ctx)
	snap := s.build(now, sys, gpu)
	s.remember(now, sys)
	s.snap.Store(snap)
}

// collect runs the system and GPU collectors concurrently, so the 100-300 ms
// of a nvidia-smi spawn overlap with the (much cheaper) gopsutil reads
// instead of adding to them.
func (s *Sampler) collect(ctx context.Context) (collector.Sample, collector.GPUSample) {
	var (
		sys collector.Sample
		gpu collector.GPUSample
		wg  sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		sys = s.sys.Collect(ctx)
	}()
	go func() {
		defer wg.Done()
		gpu = s.gpu.Collect(ctx)
	}()
	wg.Wait()
	return sys, gpu
}

func (s *Sampler) remember(now time.Time, sys collector.Sample) {
	s.prev = counters{
		at:        now,
		diskRead:  sys.DiskReadB,
		diskWrite: sys.DiskWriteB,
		netRx:     sys.NetRxB,
		netTx:     sys.NetTxB,
	}
}

// build converts raw readings into the wire model: units, percentages,
// rates and rounding all happen here and nowhere else.
func (s *Sampler) build(now time.Time, sys collector.Sample, gpu collector.GPUSample) *model.Snapshot {
	st := model.Stats{
		TS:           now.Unix(),
		AgentVersion: s.cfg.Version,
		Host:         s.cfg.Static.Host,
		OS:           s.cfg.Static.OS,
		CPUFreqMHz:   roundPtr(s.cfg.Static.CPUFreqMHz),
		CPUPct:       roundPtr(sys.CPUPct),
		CPUTempC:     roundPtr(sys.CPUTempC),
		Load1:        roundPtr(sys.Load1),
		Load5:        roundPtr(sys.Load5),
		Load15:       roundPtr(sys.Load15),
		GPUPresent:   gpu.Present,
	}

	if sys.UptimeS != nil {
		// uint64 seconds since boot never approaches int64 range; the guard
		// exists only so the conversion is provably safe.
		st.UptimeS = ptr(int64(min(*sys.UptimeS, math.MaxInt64)))
	}

	// RAM: used = total - available, on both platforms (see collector).
	if sys.RAMTotalB != nil && sys.RAMAvailB != nil {
		total, avail := *sys.RAMTotalB, *sys.RAMAvailB
		used := total - min(avail, total)
		st.RAMUsedGB = ptr(round1(bytesToGiB(used)))
		st.RAMTotalGB = ptr(round1(bytesToGiB(total)))
		st.RAMPct = ptr(round1(pct(used, total)))
	}
	if sys.SwapTotalB != nil && sys.SwapUsedB != nil {
		st.SwapUsedGB = ptr(round1(bytesToGiB(*sys.SwapUsedB)))
		st.SwapPct = ptr(round1(pct(*sys.SwapUsedB, *sys.SwapTotalB)))
	}

	// Disk: percentage is computed here as used/total so it always agrees
	// with the two GB fields next to it. gopsutil's UsedPercent uses
	// used/(used+free), which excludes reserved blocks and would not.
	if sys.DiskTotalB != nil && sys.DiskUsedB != nil {
		st.DiskUsedGB = ptr(round1(bytesToGiB(*sys.DiskUsedB)))
		st.DiskTotalGB = ptr(round1(bytesToGiB(*sys.DiskTotalB)))
		st.DiskPct = ptr(round1(pct(*sys.DiskUsedB, *sys.DiskTotalB)))
	}

	elapsed := now.Sub(s.prev.at)
	if r, ok := Rate(s.prev.diskRead, sys.DiskReadB, elapsed); ok {
		st.DiskReadMBs = ptr(round1(bytesToMBs(r)))
	}
	if r, ok := Rate(s.prev.diskWrite, sys.DiskWriteB, elapsed); ok {
		st.DiskWriteMBs = ptr(round1(bytesToMBs(r)))
	}
	if r, ok := Rate(s.prev.netRx, sys.NetRxB, elapsed); ok {
		st.NetRxMbps = ptr(round1(bytesToMbps(r)))
	}
	if r, ok := Rate(s.prev.netTx, sys.NetTxB, elapsed); ok {
		st.NetTxMbps = ptr(round1(bytesToMbps(r)))
	}

	if gpu.Present {
		st.GPUPct = roundPtr(gpu.UtilPct)
		st.GPUTempC = roundPtr(gpu.TempC)
		st.GPUVRAMUsedMB = roundPtr(gpu.VRAMUsedMiB)
		st.GPUVRAMTotalMB = roundPtr(gpu.VRAMTotalMiB)
		st.GPUPowerW = roundPtr(gpu.PowerW)
		st.GPUFanPct = roundPtr(gpu.FanPct)
		st.GPUClockMHz = roundPtr(gpu.ClockMHz)
		if gpu.VRAMUsedMiB != nil && gpu.VRAMTotalMiB != nil && *gpu.VRAMTotalMiB > 0 {
			st.GPUVRAMPct = ptr(round1(*gpu.VRAMUsedMiB / *gpu.VRAMTotalMiB * 100))
		}
	}

	cores := make([]float64, 0, len(sys.CoresPct)) // empty array, never null
	for _, c := range sys.CoresPct {
		cores = append(cores, round1(c))
	}

	return &model.Snapshot{
		TakenAt: now,
		Stats:   st,
		Cores:   model.Cores{TS: now.Unix(), CoresPct: cores},
	}
}

func ptr[T any](v T) *T { return &v }

func roundPtr(p *float64) *float64 {
	if p == nil {
		return nil
	}
	return ptr(round1(*p))
}
