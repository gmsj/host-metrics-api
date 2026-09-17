package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gmsj/host-metrics-api/internal/model"
)

type stubSource struct{ snap *model.Snapshot }

func (s stubSource) Snapshot() *model.Snapshot { return s.snap }

func fl(v float64) *float64 { return &v }

// expectedKeys is the public contract of /stats. If this list changes, the
// README and every consumer change with it.
var expectedKeys = []string{
	"ts", "agent_version", "host", "os", "uptime_s",
	"cpu_pct", "cpu_freq_mhz", "cpu_temp_c", "load1", "load5", "load15",
	"ram_used_gb", "ram_total_gb", "ram_pct", "swap_used_gb", "swap_pct",
	"disk_used_gb", "disk_total_gb", "disk_pct", "disk_read_mb_s", "disk_write_mb_s",
	"net_rx_mbps", "net_tx_mbps",
	"gpu_present", "gpu_pct", "gpu_temp_c", "gpu_vram_used_mb", "gpu_vram_total_mb",
	"gpu_vram_pct", "gpu_power_w", "gpu_fan_pct", "gpu_clock_mhz",
}

func newHandler(snap *model.Snapshot) http.Handler {
	return NewHandler(stubSource{snap}, 3*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func sampleSnapshot(at time.Time) *model.Snapshot {
	return &model.Snapshot{
		TakenAt: at,
		Stats: model.Stats{
			TS: at.Unix(), AgentVersion: "0.1.0", Host: "desktop", OS: "windows",
			UptimeS: ptrInt(18342), CPUPct: fl(23.4), CPUFreqMHz: fl(3800),
			RAMUsedGB: fl(19.4), RAMTotalGB: fl(32), RAMPct: fl(60.6),
			GPUPresent: true, GPUPct: fl(88),
		},
		Cores: model.Cores{TS: at.Unix(), CoresPct: []float64{12.1, 40.2}},
	}
}

func ptrInt(v int64) *int64 { return &v }

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	return rec
}

func TestStatsShapeAndHeaders(t *testing.T) {
	now := time.Now()
	rec := get(t, newHandler(sampleSnapshot(now)), "/stats")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cors := rec.Header().Get("Access-Control-Allow-Origin"); cors != "*" {
		t.Errorf("CORS header = %q, want *", cors)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q", cc)
	}

	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, rec.Body)
	}
	got := make([]string, 0, len(m))
	for k := range m {
		got = append(got, k)
	}
	want := slices.Clone(expectedKeys)
	sort.Strings(got)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Errorf("key set mismatch\n got: %v\nwant: %v", got, want)
	}

	// The payload is flat: no nested objects, no arrays.
	for k, v := range m {
		switch v.(type) {
		case map[string]any, []any:
			t.Errorf("field %q is nested (%T); /stats must be flat", k, v)
		}
	}
	// Null is explicit, not omitted.
	if v, ok := m["cpu_temp_c"]; !ok || v != nil {
		t.Errorf("cpu_temp_c should be present and null, got %v", v)
	}
	if m["gpu_present"] != true || m["gpu_pct"] != 88.0 {
		t.Errorf("gpu fields wrong: %v %v", m["gpu_present"], m["gpu_pct"])
	}
}

func TestCores(t *testing.T) {
	now := time.Now()
	rec := get(t, newHandler(sampleSnapshot(now)), "/stats/cores")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var c model.Cores
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	if c.TS != now.Unix() || len(c.CoresPct) != 2 {
		t.Errorf("unexpected body: %s", rec.Body)
	}
}

func TestNoSnapshotYet(t *testing.T) {
	h := newHandler(nil)
	for _, path := range []string{"/stats", "/stats/cores", "/healthz"} {
		rec := get(t, h, path)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s before first snapshot: status = %d, want 503", path, rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Errorf("%s: CORS header must be present even on errors", path)
		}
	}
}

func TestHealthzStaleness(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	taken := time.Unix(1_000_000, 0)
	h := &handler{src: stubSource{sampleSnapshot(taken)}, maxAge: 3 * time.Second, log: log}

	h.now = func() time.Time { return taken.Add(2 * time.Second) }
	rec := httptest.NewRecorder()
	h.healthz(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != "ok" {
		t.Errorf("fresh snapshot: status %d body %q", rec.Code, rec.Body)
	}

	h.now = func() time.Time { return taken.Add(4 * time.Second) }
	rec = httptest.NewRecorder()
	h.healthz(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable || strings.TrimSpace(rec.Body.String()) != "stale" {
		t.Errorf("stale snapshot: status %d body %q", rec.Code, rec.Body)
	}
}

func TestMethodNotAllowedAndNotFound(t *testing.T) {
	now := time.Now()
	h := newHandler(sampleSnapshot(now))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/stats", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /stats: status = %d, want 405", rec.Code)
	}
	if rec := get(t, h, "/nope"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /nope: status = %d, want 404", rec.Code)
	}
}
