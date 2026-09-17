package collector

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func f(v float64) *float64 { return &v }

func eqPtr(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestParseNvidiaSMI(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		want       GPUSample
		wantExtra  int
		wantErr    bool
		errContain string
	}{
		{
			name: "happy path",
			in:   "23, 61, 6144, 8192, 198.52, 41, 1815\n",
			want: GPUSample{
				Present: true, UtilPct: f(23), TempC: f(61), VRAMUsedMiB: f(6144),
				VRAMTotalMiB: f(8192), PowerW: f(198.52), FanPct: f(41), ClockMHz: f(1815),
			},
		},
		{
			name: "idle card with zero fan",
			in:   "0, 50, 806, 8192, 17.05, 0, 210",
			want: GPUSample{
				Present: true, UtilPct: f(0), TempC: f(50), VRAMUsedMiB: f(806),
				VRAMTotalMiB: f(8192), PowerW: f(17.05), FanPct: f(0), ClockMHz: f(210),
			},
		},
		{
			name: "fan not available",
			in:   "23, 61, 6144, 8192, 198.52, [N/A], 1815",
			want: GPUSample{
				Present: true, UtilPct: f(23), TempC: f(61), VRAMUsedMiB: f(6144),
				VRAMTotalMiB: f(8192), PowerW: f(198.52), FanPct: nil, ClockMHz: f(1815),
			},
		},
		{
			name: "power not supported",
			in:   "23, 61, 6144, 8192, [Not Supported], 41, 1815",
			want: GPUSample{
				Present: true, UtilPct: f(23), TempC: f(61), VRAMUsedMiB: f(6144),
				VRAMTotalMiB: f(8192), PowerW: nil, FanPct: f(41), ClockMHz: f(1815),
			},
		},
		{
			name: "unknown error marker",
			in:   "[Unknown Error], 61, 6144, 8192, 198.52, 41, 1815",
			want: GPUSample{
				Present: true, UtilPct: nil, TempC: f(61), VRAMUsedMiB: f(6144),
				VRAMTotalMiB: f(8192), PowerW: f(198.52), FanPct: f(41), ClockMHz: f(1815),
			},
		},
		{
			name: "garbage in one field becomes null, not an error",
			in:   "23, sixty-one, 6144, 8192, 198.52, 41, 1815",
			want: GPUSample{
				Present: true, UtilPct: f(23), TempC: nil, VRAMUsedMiB: f(6144),
				VRAMTotalMiB: f(8192), PowerW: f(198.52), FanPct: f(41), ClockMHz: f(1815),
			},
		},
		{
			name: "windows line endings",
			in:   "23, 61, 6144, 8192, 198.52, 41, 1815\r\n",
			want: GPUSample{
				Present: true, UtilPct: f(23), TempC: f(61), VRAMUsedMiB: f(6144),
				VRAMTotalMiB: f(8192), PowerW: f(198.52), FanPct: f(41), ClockMHz: f(1815),
			},
		},
		{
			name: "two GPUs: first line wins, extra counted",
			in:   "23, 61, 6144, 8192, 198.52, 41, 1815\n5, 40, 100, 4096, 20, 0, 300\n",
			want: GPUSample{
				Present: true, UtilPct: f(23), TempC: f(61), VRAMUsedMiB: f(6144),
				VRAMTotalMiB: f(8192), PowerW: f(198.52), FanPct: f(41), ClockMHz: f(1815),
			},
			wantExtra: 1,
		},
		{name: "empty output", in: "", wantErr: true, errContain: "no output"},
		{name: "only whitespace", in: "\n  \n", wantErr: true, errContain: "no output"},
		{name: "no devices message", in: "No devices were found\n", wantErr: true, errContain: "expected 7 fields"},
		{name: "too few fields", in: "23, 61, 6144", wantErr: true, errContain: "got 3"},
		{name: "too many fields", in: "1, 2, 3, 4, 5, 6, 7, 8", wantErr: true, errContain: "got 8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseNvidiaSMI([]byte(tt.in))
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				if tt.errContain != "" && !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("error %q does not contain %q", err, tt.errContain)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.extraLines != tt.wantExtra {
				t.Errorf("extraLines = %d, want %d", got.extraLines, tt.wantExtra)
			}
			assertGPUSample(t, got.sample, tt.want)
		})
	}
}

func assertGPUSample(t *testing.T, got, want GPUSample) {
	t.Helper()
	if got.Present != want.Present {
		t.Errorf("Present = %v, want %v", got.Present, want.Present)
	}
	pairs := []struct {
		name      string
		got, want *float64
	}{
		{"UtilPct", got.UtilPct, want.UtilPct},
		{"TempC", got.TempC, want.TempC},
		{"VRAMUsedMiB", got.VRAMUsedMiB, want.VRAMUsedMiB},
		{"VRAMTotalMiB", got.VRAMTotalMiB, want.VRAMTotalMiB},
		{"PowerW", got.PowerW, want.PowerW},
		{"FanPct", got.FanPct, want.FanPct},
		{"ClockMHz", got.ClockMHz, want.ClockMHz},
	}
	for _, p := range pairs {
		if !eqPtr(p.got, p.want) {
			t.Errorf("%s = %v, want %v", p.name, deref(p.got), deref(p.want))
		}
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// fakeSMI scripts nvidia-smi results and counts how often it was invoked.
type fakeSMI struct {
	results []func() ([]byte, error)
	calls   int
}

func (f *fakeSMI) run(_ context.Context) ([]byte, error) {
	f.calls++
	if len(f.results) == 0 {
		return nil, errors.New("exec: \"nvidia-smi\": executable file not found in $PATH")
	}
	r := f.results[0]
	if len(f.results) > 1 {
		f.results = f.results[1:]
	}
	return r()
}

func okOut() ([]byte, error) { return []byte("23, 61, 6144, 8192, 198.52, 41, 1815\n"), nil }
func failOut() ([]byte, error) {
	return nil, errors.New("nvidia-smi: NVIDIA-SMI has failed")
}

func newTestNvidia(fake *fakeSMI, clock *time.Time) *NvidiaCollector {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newNvidia(log, fake.run, func() time.Time { return *clock })
}

func TestNvidiaAbsentBacksOff(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	fake := &fakeSMI{} // always "not found"
	c := newTestNvidia(fake, &now)

	for i := 0; i < 10; i++ {
		got := c.Collect(context.Background())
		if got.Present {
			t.Fatal("GPU must not be present")
		}
		now = now.Add(time.Second)
	}
	if fake.calls != 1 {
		t.Errorf("nvidia-smi was invoked %d times in 10 s; want 1 (backoff)", fake.calls)
	}

	now = now.Add(gpuRetryInterval)
	c.Collect(context.Background())
	if fake.calls != 2 {
		t.Errorf("expected a re-probe after %s, calls = %d", gpuRetryInterval, fake.calls)
	}
}

func TestNvidiaAppearsLater(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	fake := &fakeSMI{results: []func() ([]byte, error){failOut, okOut}}
	c := newTestNvidia(fake, &now)

	if c.Collect(context.Background()).Present {
		t.Fatal("first probe should fail")
	}
	now = now.Add(gpuRetryInterval + time.Second)
	got := c.Collect(context.Background())
	if !got.Present || got.UtilPct == nil || *got.UtilPct != 23 {
		t.Errorf("GPU should be present after a successful re-probe: %+v", got)
	}
}

func TestNvidiaTransientFailureKeepsPresent(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	fake := &fakeSMI{results: []func() ([]byte, error){okOut, failOut, okOut}}
	c := newTestNvidia(fake, &now)

	if !c.Collect(context.Background()).Present {
		t.Fatal("expected present")
	}
	got := c.Collect(context.Background())
	if !got.Present {
		t.Error("one failure must not flip gpu_present to false")
	}
	if got.UtilPct != nil || got.TempC != nil {
		t.Error("fields must be null on a failed tick")
	}
	got = c.Collect(context.Background())
	if !got.Present || got.UtilPct == nil {
		t.Errorf("expected recovery: %+v", got)
	}
	if c.failures != 0 {
		t.Errorf("failure counter should reset on success, got %d", c.failures)
	}
}

func TestNvidiaRepeatedFailuresFlipToAbsent(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	fake := &fakeSMI{results: []func() ([]byte, error){okOut, failOut}} // ok once, then fail forever
	c := newTestNvidia(fake, &now)

	c.Collect(context.Background())
	for i := 1; i < gpuMaxFailures; i++ {
		if !c.Collect(context.Background()).Present {
			t.Fatalf("flipped to absent after only %d failures", i)
		}
	}
	if c.Collect(context.Background()).Present {
		t.Fatalf("still present after %d consecutive failures", gpuMaxFailures)
	}
	callsBefore := fake.calls
	now = now.Add(time.Second)
	c.Collect(context.Background())
	if fake.calls != callsBefore {
		t.Error("after flipping to absent the collector must back off, not probe every tick")
	}
}

func TestNvidiaMalformedOutputIsFailure(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	fake := &fakeSMI{results: []func() ([]byte, error){func() ([]byte, error) { return []byte("No devices were found\n"), nil }}}
	c := newTestNvidia(fake, &now)
	if c.Collect(context.Background()).Present {
		t.Error("malformed output must count as a failed probe")
	}
}
