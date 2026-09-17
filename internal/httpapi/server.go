// Package httpapi serves the snapshots over HTTP. Handlers never collect
// anything; they only read the sampler's latest snapshot and serialize it.
package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gmsj/host-metrics-api/internal/model"
)

// Source is the read side of the sampler. Declared here (the consumer)
// rather than in the sampler package: in Go the package that needs an
// interface defines it, which keeps httpapi independent of the sampler and
// trivial to test with a stub.
type Source interface {
	Snapshot() *model.Snapshot
}

type handler struct {
	src    Source
	maxAge time.Duration
	now    func() time.Time
	log    *slog.Logger
}

// NewHandler returns the routed handler. maxAge is how old the latest
// snapshot may be before /healthz reports 503.
func NewHandler(src Source, maxAge time.Duration, log *slog.Logger) http.Handler {
	h := &handler{src: src, maxAge: maxAge, now: time.Now, log: log}

	// Go 1.22 ServeMux patterns carry the method: anything but GET (and HEAD,
	// which GET implies) gets a 405 from the mux itself.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats", h.stats)
	mux.HandleFunc("GET /stats/cores", h.cores)
	mux.HandleFunc("GET /healthz", h.healthz)
	return withCommonHeaders(mux)
}

// withCommonHeaders adds the headers every response needs.
//   - CORS "*": lets a dashboard served from any origin read the endpoint
//     from a browser without touching the agent. One line now, or a fork
//     later.
//   - no-store: the payload changes every second; nothing may cache it.
func withCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (h *handler) stats(w http.ResponseWriter, _ *http.Request) {
	snap := h.src.Snapshot()
	if snap == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no snapshot yet"})
		return
	}
	writeJSON(w, http.StatusOK, snap.Stats)
}

func (h *handler) cores(w http.ResponseWriter, _ *http.Request) {
	snap := h.src.Snapshot()
	if snap == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no snapshot yet"})
		return
	}
	writeJSON(w, http.StatusOK, snap.Cores)
}

// healthz answers 200 only when the sampler is alive: a snapshot exists and
// it is younger than maxAge. A stuck sampler keeps serving the same /stats
// forever (its ts stops moving); this endpoint is how a supervisor notices.
func (h *handler) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	snap := h.src.Snapshot()
	switch {
	case snap == nil:
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("no snapshot yet\n"))
	case h.now().Sub(snap.TakenAt) > h.maxAge:
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("stale\n"))
	default:
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Encoding a ~35-field struct takes microseconds; a failure here means
	// the client went away, and there is nobody left to tell.
	_ = json.NewEncoder(w).Encode(v)
}
