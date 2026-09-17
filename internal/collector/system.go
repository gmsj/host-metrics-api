package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"slices"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	gnet "github.com/shirou/gopsutil/v4/net"
)

// SystemCollector implements System on top of gopsutil. It is safe to call
// Collect from a single goroutine only; the sampler is that goroutine.
type SystemCollector struct {
	log    *slog.Logger
	faults *faultTracker

	diskPath  string
	ioEnabled bool
	ioDisks   []string // devices whose I/O is summed; empty means "every device gopsutil returns"
	netIface  string   // "" when no usable interface exists
}

// NewSystem resolves the monitored volume and network interface once and
// returns a collector ready for the sampler. Empty diskPath or netIface means
// "auto".
func NewSystem(ctx context.Context, log *slog.Logger, diskPath, netIface string) *SystemCollector {
	c := &SystemCollector{log: log, faults: newFaultTracker(log)}

	c.diskPath = diskPath
	if c.diskPath == "" {
		c.diskPath = defaultDiskPath()
	}

	disks, err := ioDiskNames(ctx)
	if err != nil {
		log.Warn("disk I/O rates disabled", "err", err)
	} else {
		c.ioEnabled = true
		c.ioDisks = disks
	}

	c.netIface = netIface
	if c.netIface == "" {
		c.netIface = pickNetIface(ctx, log)
	}

	log.Info("system collector ready",
		"disk", c.diskPath, "io_disks", c.ioDisks, "net_iface", c.netIface)
	return c
}

// ReadStatic collects the values that never change during a run.
func ReadStatic(ctx context.Context, log *slog.Logger) Static {
	s := Static{OS: runtime.GOOS}

	name, err := os.Hostname()
	if err != nil {
		log.Warn("hostname unavailable", "err", err)
	}
	s.Host = name

	// cpu.Info().Mhz is the MAXIMUM (nominal) clock on both platforms:
	// Windows reads Win32_Processor.MaxClockSpeed through WMI, and gopsutil v4
	// on Linux deliberately overrides /proc/cpuinfo with
	// cpufreq/cpuinfo_max_freq "to match the behaviour of Windows". It is not
	// the current clock. Do not "fix" this by parsing /proc/cpuinfo: that
	// would reintroduce a semantic difference between the two platforms.
	// Reading it once here also matters on Windows, where the WMI query costs
	// tens to hundreds of milliseconds.
	infos, err := cpu.InfoWithContext(ctx)
	if err != nil {
		log.Warn("cpu info unavailable; cpu_freq_mhz will be null", "err", err)
		return s
	}
	var maxMHz float64
	for _, info := range infos {
		maxMHz = max(maxMHz, info.Mhz)
	}
	if maxMHz > 0 {
		s.CPUFreqMHz = ptr(maxMHz)
	}
	return s
}

// Collect reads every metric it can. See the package comment for the failure
// policy.
func (c *SystemCollector) Collect(ctx context.Context) Sample {
	var s Sample
	c.collectCPU(ctx, &s)
	c.collectTemperature(ctx, &s)
	c.collectLoad(&s)
	c.collectMemory(ctx, &s)
	c.collectDisk(ctx, &s)
	c.collectNet(ctx, &s)
	c.collectUptime(ctx, &s)
	return s
}

func (c *SystemCollector) collectCPU(ctx context.Context, s *Sample) {
	// cpu.Percent with interval 0 returns the usage since the PREVIOUS call,
	// and that baseline is package-global state inside gopsutil. This is why
	// collection must happen in exactly one place at a fixed cadence: two
	// callers (say, two HTTP clients) would steal each other's baseline and
	// both would get garbage. With interval > 0 the call would block for the
	// whole interval instead; never do that here.
	total, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil || len(total) != 1 {
		c.faults.fail("cpu", errOr(err, "no aggregate value"))
	} else {
		c.faults.ok("cpu")
		s.CPUPct = ptr(total[0])
	}

	perCore, err := cpu.PercentWithContext(ctx, 0, true)
	if err != nil {
		c.faults.fail("cpu_cores", err)
	} else {
		c.faults.ok("cpu_cores")
		s.CoresPct = perCore
	}
}

func (c *SystemCollector) collectTemperature(ctx context.Context, s *Sample) {
	// Platform-specific: hwmon on Linux, always nil on Windows. See temp_*.go.
	temp, err := cpuTemperature(ctx)
	if err != nil {
		c.faults.fail("cpu_temp", err)
		return
	}
	c.faults.ok("cpu_temp")
	s.CPUTempC = temp
}

func (c *SystemCollector) collectLoad(s *Sample) {
	// load.Avg on Windows is an emulation: on the first call gopsutil starts
	// a background goroutine that samples the processor queue length every
	// 5 s and folds it into 1/5/15-minute exponential averages. Two
	// consequences that shape this code:
	//   1. That goroutine is bound to the context of the FIRST call. Passing a
	//      per-tick timeout context would kill it after the first tick and
	//      freeze the values forever. So this call gets no context on purpose.
	//   2. Values are near zero for the first minutes (warm-up) and are not
	//      comparable with a Unix load average. Documented in the README.
	avg, err := load.Avg()
	if err != nil {
		c.faults.fail("load", err)
		return
	}
	c.faults.ok("load")
	s.Load1, s.Load5, s.Load15 = ptr(avg.Load1), ptr(avg.Load5), ptr(avg.Load15)
}

func (c *SystemCollector) collectMemory(ctx context.Context, s *Sample) {
	// Only Total and Available are used. gopsutil's Used/UsedPercent are
	// computed differently per platform (Linux subtracts buffers and cache),
	// and the Linux-only fields (Buffers, Cached, Active, ...) are zero on
	// Windows. The sampler derives used = total - available on both.
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		c.faults.fail("ram", err)
	} else {
		c.faults.ok("ram")
		s.RAMTotalB, s.RAMAvailB = ptr(vm.Total), ptr(vm.Available)
	}

	// On Windows gopsutil v4 reports page-file usage here (commit limit minus
	// physical RAM, and the "\Paging File(_Total)\% Usage" counter). That is
	// the practical equivalent of swap, so the field is exposed on both
	// platforms. Older support tables that mark swap as unsupported on
	// Windows are out of date.
	sw, err := mem.SwapMemoryWithContext(ctx)
	if err != nil {
		c.faults.fail("swap", err)
	} else {
		c.faults.ok("swap")
		s.SwapTotalB, s.SwapUsedB = ptr(sw.Total), ptr(sw.Used)
	}
}

func (c *SystemCollector) collectDisk(ctx context.Context, s *Sample) {
	usage, err := disk.UsageWithContext(ctx, c.diskPath)
	if err != nil {
		c.faults.fail("disk_usage", err)
	} else {
		c.faults.ok("disk_usage")
		s.DiskTotalB, s.DiskUsedB = ptr(usage.Total), ptr(usage.Used)
	}

	if !c.ioEnabled {
		return
	}
	// Cumulative bytes since boot, summed over the selected devices. On Linux
	// the selection matters: /proc/diskstats lists whole disks AND their
	// partitions (nvme0n1 plus nvme0n1p1..pN) plus loop devices, and summing
	// everything counts each byte twice. See ioDiskNames in disk_linux.go.
	counters, err := disk.IOCountersWithContext(ctx, c.ioDisks...)
	if err != nil {
		c.faults.fail("disk_io", err)
		return
	}
	c.faults.ok("disk_io")
	var read, write uint64
	for _, io := range counters {
		read += io.ReadBytes
		write += io.WriteBytes
	}
	s.DiskReadB, s.DiskWriteB = ptr(read), ptr(write)
}

func (c *SystemCollector) collectNet(ctx context.Context, s *Sample) {
	if c.netIface == "" {
		return
	}
	// pernic=true returns one entry per interface; false would sum them all,
	// which double counts bridges and virtual adapters (Docker, WSL, VPN).
	counters, err := gnet.IOCountersWithContext(ctx, true)
	if err != nil {
		c.faults.fail("net", err)
		return
	}
	idx := slices.IndexFunc(counters, func(io gnet.IOCountersStat) bool { return io.Name == c.netIface })
	if idx < 0 {
		c.faults.fail("net", fmt.Errorf("interface %q not found", c.netIface))
		return
	}
	c.faults.ok("net")
	s.NetRxB, s.NetTxB = ptr(counters[idx].BytesRecv), ptr(counters[idx].BytesSent)
}

func (c *SystemCollector) collectUptime(ctx context.Context, s *Sample) {
	// host.Uptime works on both platforms (GetTickCount64 on Windows,
	// /proc/uptime on Linux) even though older gopsutil support tables leave
	// the Windows cell blank.
	up, err := host.UptimeWithContext(ctx)
	if err != nil {
		c.faults.fail("uptime", err)
		return
	}
	c.faults.ok("uptime")
	s.UptimeS = ptr(up)
}

// pickNetIface chooses the non-loopback interface with the most cumulative
// traffic at startup. The choice is made once: re-evaluating every tick could
// flip between interfaces mid-run, and summing all of them double counts
// bridge/veth pairs on a host that runs Docker.
func pickNetIface(ctx context.Context, log *slog.Logger) string {
	loopback := make(map[string]bool)
	ifaces, err := gnet.InterfacesWithContext(ctx)
	if err != nil {
		log.Warn("cannot list interfaces; net rates disabled", "err", err)
		return ""
	}
	for _, iface := range ifaces {
		if slices.Contains(iface.Flags, "loopback") {
			loopback[iface.Name] = true
		}
	}

	counters, err := gnet.IOCountersWithContext(ctx, true)
	if err != nil {
		log.Warn("cannot read interface counters; net rates disabled", "err", err)
		return ""
	}
	best, bestBytes := "", uint64(0)
	for _, io := range counters {
		if loopback[io.Name] {
			continue
		}
		if total := io.BytesRecv + io.BytesSent; best == "" || total > bestBytes {
			best, bestBytes = io.Name, total
		}
	}
	if best == "" {
		log.Warn("no non-loopback interface found; net rates disabled")
	}
	return best
}

func errOr(err error, msg string) error {
	if err != nil {
		return err
	}
	return errors.New(msg)
}
