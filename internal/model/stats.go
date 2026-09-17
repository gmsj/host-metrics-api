// Package model defines the JSON contract served by the agent.
//
// Everything a consumer can see lives here, and nothing else. The structs are
// deliberately flat (no nesting, no arrays) in Stats because the consumer may
// be an embedded device with a limited JSON parser.
package model

import "time"

// Stats is the payload of GET /stats.
//
// Pointer fields serialize as JSON null when nil. That is how a metric that is
// unavailable on the current platform, or that failed to collect this tick,
// keeps its key in the payload: the contract stays byte-for-byte identical on
// Linux and Windows and the consumer never branches by OS.
//
// encoding/json emits fields in declaration order, so the order below is the
// order on the wire. Keep it grouped and stable.
//
// Units: capacities (RAM, swap, disk, VRAM) are binary (GiB / MiB), which is
// what Task Manager, `free -h` and `nvidia-smi` show. Rates are decimal:
// network in megabits per second (10^6 bit/s), disk in megabytes per second
// (10^6 byte/s). All floats are rounded to one decimal place by the sampler.
type Stats struct {
	TS           int64  `json:"ts"`
	AgentVersion string `json:"agent_version"`
	Host         string `json:"host"`
	OS           string `json:"os"`
	UptimeS      *int64 `json:"uptime_s"`

	CPUPct     *float64 `json:"cpu_pct"`
	CPUFreqMHz *float64 `json:"cpu_freq_mhz"`
	CPUTempC   *float64 `json:"cpu_temp_c"`
	Load1      *float64 `json:"load1"`
	Load5      *float64 `json:"load5"`
	Load15     *float64 `json:"load15"`

	RAMUsedGB  *float64 `json:"ram_used_gb"`
	RAMTotalGB *float64 `json:"ram_total_gb"`
	RAMPct     *float64 `json:"ram_pct"`
	SwapUsedGB *float64 `json:"swap_used_gb"`
	SwapPct    *float64 `json:"swap_pct"`

	DiskUsedGB   *float64 `json:"disk_used_gb"`
	DiskTotalGB  *float64 `json:"disk_total_gb"`
	DiskPct      *float64 `json:"disk_pct"`
	DiskReadMBs  *float64 `json:"disk_read_mb_s"`
	DiskWriteMBs *float64 `json:"disk_write_mb_s"`

	NetRxMbps *float64 `json:"net_rx_mbps"`
	NetTxMbps *float64 `json:"net_tx_mbps"`

	GPUPresent     bool     `json:"gpu_present"`
	GPUPct         *float64 `json:"gpu_pct"`
	GPUTempC       *float64 `json:"gpu_temp_c"`
	GPUVRAMUsedMB  *float64 `json:"gpu_vram_used_mb"`
	GPUVRAMTotalMB *float64 `json:"gpu_vram_total_mb"`
	GPUVRAMPct     *float64 `json:"gpu_vram_pct"`
	GPUPowerW      *float64 `json:"gpu_power_w"`
	GPUFanPct      *float64 `json:"gpu_fan_pct"`
	GPUClockMHz    *float64 `json:"gpu_clock_mhz"`
}

// Cores is the payload of GET /stats/cores. It is the only endpoint with an
// array, kept apart so the main payload stays flat.
type Cores struct {
	TS       int64     `json:"ts"`
	CoresPct []float64 `json:"cores_pct"`
}

// Snapshot is one complete, immutable result of a sampler tick. The sampler
// publishes a new *Snapshot atomically; the HTTP layer only reads it.
type Snapshot struct {
	TakenAt time.Time
	Stats   Stats
	Cores   Cores
}
