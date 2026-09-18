package sampler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/gmsj/host-metrics-api/internal/collector"
)

// fakeSystem returns scripted samples, one per call, repeating the last.
type fakeSystem struct {
	samples []collector.Sample
	calls   int
}

func (f *fakeSystem) Collect(_ context.Context) collector.Sample {
	f.calls++
	i := min(f.calls-1, len(f.samples)-1)
	return f.samples[i]
}

type fakeGPU struct{ sample collector.GPUSample }

func (f fakeGPU) Collect(_ context.Context) collector.GPUSample { return f.sample }

func fl(v float64) *float64 { return &v }

func fullSample(netRx, diskRead uint64) collector.Sample {
	return collector.Sample{
		UptimeS:    u(18342),
		CPUPct:     fl(23.44),
		CoresPct:   []float64{12.11, 40.2},
		CPUTempC:   fl(55.125),
		Load1:      fl(0.5),
		RAMTotalB:  u(32 * gib),
		RAMAvailB:  u(12 * gib),
		SwapTotalB: u(0),
		SwapUsedB:  u(0),
		DiskTotalB: u(1000 * gib),
		DiskUsedB:  u(712 * gib),
		DiskReadB:  u(diskRead),
		DiskWriteB: u(0),
		NetRxB:     u(netRx),
		NetTxB:     u(0),
	}
}

func newTestSampler(sys collector.System, gpu collector.GPU, start time.Time) (*Sampler, *time.Time) {
	clock := start
	s := New(slog.New(slog.NewTextHandler(io.Discard, nil)), sys, gpu, Config{
		Interval: time.Second,
		Version:  "test",
		Static:   collector.Static{Host: "box", OS: "linux", CPUFreqMHz: fl(4268.572)},
	})
	s.now = func() time.Time { return clock }
	return s, &clock
}

func TestSamplerPublishesAfterPriming(t *testing.T) {
	start := time.Unix(1_758_067_200, 0)
	sys := &fakeSystem{samples: []collector.Sample{
		fullSample(0, 0),
		fullSample(1_250_000, 4_200_000), // +1.25 MB in 1 s = 10 Mbit/s; +4.2 MB/s
	}}
	gpu := fakeGPU{collector.GPUSample{
		Present: true, UtilPct: fl(88), TempC: fl(61),
		VRAMUsedMiB: fl(6144), VRAMTotalMiB: fl(8192), PowerW: fl(198.52), FanPct: nil, ClockMHz: fl(1815),
	}}
	s, clock := newTestSampler(sys, gpu, start)

	if s.Snapshot() != nil {
		t.Fatal("no snapshot may exist before the first tick")
	}
	s.readGPU(context.Background())
	s.prime(context.Background())
	if s.Snapshot() != nil {
		t.Fatal("priming must not publish")
	}

	*clock = start.Add(time.Second)
	s.tick(context.Background())
	snap := s.Snapshot()
	if snap == nil {
		t.Fatal("expected a snapshot after the first tick")
	}
	st := snap.Stats

	checks := []struct {
		name string
		got  *float64
		want float64
	}{
		{"cpu_pct", st.CPUPct, 23.4},
		{"cpu_freq_mhz", st.CPUFreqMHz, 4268.6},
		{"cpu_temp_c", st.CPUTempC, 55.1},
		{"ram_used_gb", st.RAMUsedGB, 20},
		{"ram_total_gb", st.RAMTotalGB, 32},
		{"ram_pct", st.RAMPct, 62.5},
		{"swap_used_gb", st.SwapUsedGB, 0},
		{"swap_pct", st.SwapPct, 0},
		{"disk_used_gb", st.DiskUsedGB, 712},
		{"disk_total_gb", st.DiskTotalGB, 1000},
		{"disk_pct", st.DiskPct, 71.2},
		{"disk_read_mb_s", st.DiskReadMBs, 4.2},
		{"disk_write_mb_s", st.DiskWriteMBs, 0},
		{"net_rx_mbps", st.NetRxMbps, 10},
		{"net_tx_mbps", st.NetTxMbps, 0},
		{"gpu_pct", st.GPUPct, 88},
		{"gpu_vram_pct", st.GPUVRAMPct, 75},
		{"gpu_power_w", st.GPUPowerW, 198.5},
	}
	for _, c := range checks {
		if c.got == nil {
			t.Errorf("%s: got null, want %v", c.name, c.want)
			continue
		}
		if *c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, *c.got, c.want)
		}
	}
	if st.GPUFanPct != nil {
		t.Error("gpu_fan_pct should stay null when nvidia-smi reports [N/A]")
	}
	if st.TS != start.Unix()+1 || st.UptimeS == nil || *st.UptimeS != 18342 {
		t.Errorf("ts/uptime wrong: %d %v", st.TS, st.UptimeS)
	}
	if st.Host != "box" || st.OS != "linux" || st.AgentVersion != "test" {
		t.Errorf("static fields wrong: %+v", st)
	}
	if len(snap.Cores.CoresPct) != 2 || snap.Cores.CoresPct[0] != 12.1 {
		t.Errorf("cores wrong: %v", snap.Cores.CoresPct)
	}
}

func TestSamplerPartialFailureYieldsNulls(t *testing.T) {
	start := time.Unix(1_000, 0)
	// Everything failed except RAM: the sampler must still publish.
	sys := &fakeSystem{samples: []collector.Sample{{RAMTotalB: u(8 * gib), RAMAvailB: u(4 * gib)}}}
	s, clock := newTestSampler(sys, fakeGPU{}, start)
	s.readGPU(context.Background())
	s.prime(context.Background())
	*clock = start.Add(time.Second)
	s.tick(context.Background())

	snap := s.Snapshot()
	if snap == nil {
		t.Fatal("partial failure must not prevent publishing")
	}
	st := snap.Stats
	if st.RAMPct == nil || *st.RAMPct != 50 {
		t.Errorf("the one working metric must be published: %v", st.RAMPct)
	}
	if st.CPUPct != nil || st.NetRxMbps != nil || st.DiskPct != nil || st.UptimeS != nil {
		t.Error("failed metrics must be null")
	}
	if st.GPUPresent || st.GPUPct != nil {
		t.Error("absent GPU: gpu_present=false and gpu_* null")
	}
	if snap.Cores.CoresPct == nil {
		t.Error("cores_pct must be an empty array, not null")
	}

	// Null rates on the wire, not zero, and the array is [] not null.
	raw, err := json.Marshal(snap.Stats)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["net_rx_mbps"]; !ok || v != nil {
		t.Errorf("net_rx_mbps must be present and null, got %v (present=%v)", v, ok)
	}
	rawCores, _ := json.Marshal(snap.Cores)
	if string(rawCores) != `{"ts":1001,"cores_pct":[]}` {
		t.Errorf("cores json = %s", rawCores)
	}
}

func TestSamplerCounterResetPublishesNullOnce(t *testing.T) {
	start := time.Unix(1_000, 0)
	sys := &fakeSystem{samples: []collector.Sample{
		fullSample(10_000_000, 0),
		fullSample(20_000_000, 0),
		fullSample(100, 0), // interface re-created: counter went backwards
		fullSample(1_000_100, 0),
	}}
	s, clock := newTestSampler(sys, fakeGPU{}, start)
	s.prime(context.Background())

	*clock = start.Add(1 * time.Second)
	s.tick(context.Background())
	if got := s.Snapshot().Stats.NetRxMbps; got == nil || *got != 80 {
		t.Errorf("tick 1: want 80 Mbit/s, got %v", got)
	}

	*clock = start.Add(2 * time.Second)
	s.tick(context.Background())
	if got := s.Snapshot().Stats.NetRxMbps; got != nil {
		t.Errorf("tick 2 (reset): want null, got %v", *got)
	}

	*clock = start.Add(3 * time.Second)
	s.tick(context.Background())
	if got := s.Snapshot().Stats.NetRxMbps; got == nil || *got != 8 {
		t.Errorf("tick 3: baseline must be fresh after a reset, want 8, got %v", got)
	}
}

// The GPU loop runs on its own; the tick publishes whatever it last stored.
// Once that reading is older than gpuStaleTicks intervals the numbers go
// null (presence stays), and a fresh reading brings them back.
func TestSamplerStaleGPUReadingYieldsNulls(t *testing.T) {
	start := time.Unix(1_000, 0)
	sys := &fakeSystem{samples: []collector.Sample{fullSample(0, 0)}}
	gpu := fakeGPU{collector.GPUSample{Present: true, UtilPct: fl(42), TempC: fl(60)}}
	s, clock := newTestSampler(sys, gpu, start)
	s.readGPU(context.Background()) // at t=0
	s.prime(context.Background())

	*clock = start.Add(1 * time.Second)
	s.tick(context.Background())
	if st := s.Snapshot().Stats; !st.GPUPresent || st.GPUPct == nil || *st.GPUPct != 42 {
		t.Fatalf("tick 1 must publish the reading taken 1 s earlier: %+v", st)
	}

	*clock = start.Add(3 * time.Second) // exactly gpuStaleTicks old: still fresh
	s.tick(context.Background())
	if st := s.Snapshot().Stats; st.GPUPct == nil {
		t.Error("a reading exactly gpuStaleTicks intervals old is still fresh")
	}

	*clock = start.Add(4 * time.Second) // older than that: numbers go null
	s.tick(context.Background())
	if st := s.Snapshot().Stats; !st.GPUPresent || st.GPUPct != nil || st.GPUTempC != nil {
		t.Errorf("stale reading: want gpu_present=true with null numbers, got %+v", st)
	}

	s.readGPU(context.Background()) // nvidia-smi answers again at t=4
	*clock = start.Add(5 * time.Second)
	s.tick(context.Background())
	if st := s.Snapshot().Stats; st.GPUPct == nil || *st.GPUPct != 42 {
		t.Errorf("fresh reading must bring the numbers back: %+v", st)
	}
}

func TestSamplerTickBeforeAnyGPUReadingIsAbsent(t *testing.T) {
	start := time.Unix(1_000, 0)
	sys := &fakeSystem{samples: []collector.Sample{fullSample(0, 0)}}
	gpu := fakeGPU{collector.GPUSample{Present: true, UtilPct: fl(42)}}
	s, clock := newTestSampler(sys, gpu, start)
	s.prime(context.Background())
	*clock = start.Add(time.Second)
	s.tick(context.Background())
	if st := s.Snapshot().Stats; st.GPUPresent || st.GPUPct != nil {
		t.Errorf("no reading yet: want gpu_present=false and nulls, got %+v", st)
	}
}

// A reading interrupted by shutdown is dropped rather than published.
func TestSamplerReadGPUDropsReadingOnCancel(t *testing.T) {
	start := time.Unix(1_000, 0)
	sys := &fakeSystem{samples: []collector.Sample{fullSample(0, 0)}}
	s, _ := newTestSampler(sys, fakeGPU{collector.GPUSample{Present: true}}, start)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.readGPU(ctx)
	if s.gpuLatest.Load() != nil {
		t.Error("a reading taken under a cancelled context must not be stored")
	}
}

func TestSamplerRunStopsOnCancel(t *testing.T) {
	sys := &fakeSystem{samples: []collector.Sample{fullSample(0, 0)}}
	s := New(slog.New(slog.NewTextHandler(io.Discard, nil)), sys, fakeGPU{}, Config{Interval: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	deadline := time.After(2 * time.Second)
	for s.Snapshot() == nil {
		select {
		case <-deadline:
			t.Fatal("sampler never published")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
