package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	// nvidiaSMITimeout bounds one nvidia-smi call. A wedged driver must not
	// freeze the GPU loop forever; the reading just becomes null. Generous on
	// purpose: on Windows a call can take seconds when the driver has to wake
	// a power-gated GPU, and since the sampler no longer waits for this call
	// (see sampler.runGPU) a slow one costs nothing but a slightly older
	// gpu_* reading.
	nvidiaSMITimeout = 5 * time.Second
	// gpuRetryInterval is how long to wait before probing again after the GPU
	// was found absent. Long enough not to spam the log or spawn processes
	// for nothing on a machine without NVIDIA hardware.
	gpuRetryInterval = 60 * time.Second
	// gpuMaxFailures consecutive failures flip a present GPU back to absent.
	gpuMaxFailures = 5
	// nvidiaSMIFields is the number of columns requested below.
	nvidiaSMIFields = 7
)

// nvidiaSMIArgs is identical on both platforms. "nounits" strips " MiB",
// " W", " %" so every column is a bare number (or a bracketed marker).
var nvidiaSMIArgs = []string{
	"--query-gpu=utilization.gpu,temperature.gpu,memory.used,memory.total,power.draw,fan.speed,clocks.current.graphics",
	"--format=csv,noheader,nounits",
}

// smiRunner executes nvidia-smi and returns its stdout. It is a function
// value rather than a method so tests can drive the state machine without a
// GPU. The real one is runNvidiaSMI.
type smiRunner func(ctx context.Context) ([]byte, error)

// NvidiaCollector implements GPU by shelling out to nvidia-smi.
//
// Presence state machine:
//   - absent  → probe at most once per gpuRetryInterval; success → present.
//   - present → probe every tick; a failure keeps present=true with null
//     fields; gpuMaxFailures consecutive failures → absent (with backoff).
//
// NVML bindings would avoid the process spawn but need cgo, which would break
// the CGO_ENABLED=0 cross-compile this project depends on.
//
// Collect must be called from one goroutine at a time (the sampler's).
type NvidiaCollector struct {
	log *slog.Logger
	run smiRunner
	now func() time.Time

	present        bool
	failures       int
	nextProbe      time.Time
	loggedAbsent   bool
	warnedMultiGPU bool
}

// NewNvidia returns a collector that runs the real nvidia-smi.
func NewNvidia(log *slog.Logger) *NvidiaCollector {
	return newNvidia(log, runNvidiaSMI, time.Now)
}

func newNvidia(log *slog.Logger, run smiRunner, now func() time.Time) *NvidiaCollector {
	return &NvidiaCollector{log: log, run: run, now: now}
}

// Collect runs nvidia-smi (subject to the backoff) and parses its output.
//
// When ctx itself is cancelled (shutdown) the call is abandoned quietly: the
// child gets killed, or dies first from the same Ctrl+C / SIGTERM the agent
// received, and neither says anything about the GPU.
func (c *NvidiaCollector) Collect(ctx context.Context) GPUSample {
	now := c.now()
	if !c.present && now.Before(c.nextProbe) {
		return GPUSample{}
	}

	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, nvidiaSMITimeout)
	defer cancel()

	out, err := c.run(ctx)
	if err == nil {
		var parsed parsedGPU
		if parsed, err = parseNvidiaSMI(out); err == nil {
			c.onSuccess(parsed.extraLines)
			return parsed.sample
		}
	}
	if parent.Err() != nil {
		return GPUSample{Present: c.present}
	}
	return c.onFailure(now, c.now().Sub(now), err)
}

func (c *NvidiaCollector) onSuccess(extraLines int) {
	if !c.present {
		c.log.Info("NVIDIA GPU detected; gpu_present=true")
		c.present = true
		c.loggedAbsent = false
	}
	if c.failures > 0 {
		c.log.Info("nvidia-smi recovered", "failed_ticks", c.failures)
	}
	c.failures = 0
	if extraLines > 0 && !c.warnedMultiGPU {
		// The target machine has exactly one GPU by decision; if more show up
		// we use the first line and say so once.
		c.log.Warn("nvidia-smi reported more than one GPU; using the first", "extra", extraLines)
		c.warnedMultiGPU = true
	}
}

// onFailure updates the presence state after a failed call. took is how long
// the call lasted: on a timeout it tells whether nvidia-smi was merely slow or
// hung, which the error text alone does not.
func (c *NvidiaCollector) onFailure(now time.Time, took time.Duration, err error) GPUSample {
	if !c.present {
		c.nextProbe = now.Add(gpuRetryInterval)
		if c.loggedAbsent {
			c.log.Debug("nvidia-smi still unavailable", "err", err, "took", took)
		} else {
			c.log.Info("nvidia-smi unavailable; gpu_present=false", "err", err, "took", took, "retry_every", gpuRetryInterval)
			c.loggedAbsent = true
		}
		return GPUSample{}
	}

	c.failures++
	if c.failures >= gpuMaxFailures {
		c.log.Warn("nvidia-smi failed repeatedly; marking GPU absent", "failures", c.failures, "err", err, "took", took)
		c.present = false
		c.failures = 0
		c.nextProbe = now.Add(gpuRetryInterval)
		c.loggedAbsent = true
		return GPUSample{}
	}
	if c.failures == 1 {
		c.log.Warn("nvidia-smi failed; gpu_* fields null until it recovers", "err", err, "took", took)
	} else {
		c.log.Debug("nvidia-smi failed again", "failures", c.failures, "err", err, "took", took)
	}
	return GPUSample{Present: true}
}

// runNvidiaSMI spawns the real binary. It relies on nvidia-smi being on PATH,
// which the driver installer guarantees on both platforms (System32 on
// Windows, /usr/bin on Linux).
func runNvidiaSMI(ctx context.Context) ([]byte, error) {
	cmd := nvidiaSMICommand(ctx) // platform-specific: hides the console window on Windows
	// When the context deadline kills the process, Wait would still block
	// until every inherited pipe closes. WaitDelay caps that.
	cmd.WaitDelay = 500 * time.Millisecond
	out, err := cmd.Output()
	if err != nil {
		// nvidia-smi prints its own diagnostics ("Unable to determine the
		// device handle for GPU ...", "NVIDIA-SMI has failed because ...") to
		// STDOUT, not stderr. Output still returns that stdout alongside the
		// error, so keep both: without it "exit status 1" is undiagnosable.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if detail := oneLine(out, exitErr.Stderr); detail != "" {
				return nil, fmt.Errorf("%w: %s", err, detail)
			}
		}
		return nil, err
	}
	return out, nil
}

// oneLine joins the process's stdout and stderr into a single log-friendly
// line: whitespace collapsed, capped in length.
func oneLine(chunks ...[]byte) string {
	const maxLen = 300
	var parts []string
	for _, c := range chunks {
		if s := strings.Join(strings.Fields(string(c)), " "); s != "" {
			parts = append(parts, s)
		}
	}
	s := strings.Join(parts, " | ")
	if len(s) > maxLen {
		s = s[:maxLen] + "..."
	}
	return s
}

type parsedGPU struct {
	sample     GPUSample
	extraLines int
}

// parseNvidiaSMI parses the CSV produced by nvidiaSMIArgs. It is a pure
// function so it can be tested against every known output shape.
//
// Expected: "23, 61, 6144, 8192, 198.52, 41, 1815". Individual fields may be
// "[N/A]" or "[Not Supported]" (typical for fan.speed on passively cooled
// cards, sometimes power.draw); those become nil. A line with the wrong
// number of fields (e.g. "No devices were found") is an error.
func parseNvidiaSMI(out []byte) (parsedGPU, error) {
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return parsedGPU{}, errors.New("nvidia-smi produced no output")
	}

	fields := strings.Split(lines[0], ",")
	if len(fields) != nvidiaSMIFields {
		return parsedGPU{}, fmt.Errorf("nvidia-smi: expected %d fields, got %d in %q", nvidiaSMIFields, len(fields), lines[0])
	}
	vals := make([]*float64, nvidiaSMIFields)
	for i, f := range fields {
		vals[i] = parseSMIField(f)
	}
	return parsedGPU{
		sample: GPUSample{
			Present:      true,
			UtilPct:      vals[0],
			TempC:        vals[1],
			VRAMUsedMiB:  vals[2],
			VRAMTotalMiB: vals[3],
			PowerW:       vals[4],
			FanPct:       vals[5],
			ClockMHz:     vals[6],
		},
		extraLines: len(lines) - 1,
	}, nil
}

// parseSMIField converts one CSV cell. Anything in square brackets is a
// marker nvidia-smi uses for missing data ("[N/A]", "[Not Supported]",
// "[Unknown Error]", "[Insufficient Permissions]"): all of them mean null.
func parseSMIField(raw string) *float64 {
	s := strings.TrimSpace(raw)
	if s == "" || strings.HasPrefix(s, "[") {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}
