// Command hostmetrics exposes host and GPU metrics as JSON over HTTP.
//
// This file is the composition root: it parses configuration, builds the
// collectors, the sampler and the HTTP server, wires them together and
// handles shutdown. No business logic lives here.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gmsj/host-metrics-api/internal/collector"
	"github.com/gmsj/host-metrics-api/internal/config"
	"github.com/gmsj/host-metrics-api/internal/httpapi"
	"github.com/gmsj/host-metrics-api/internal/sampler"
)

// version is injected at build time: go build -ldflags "-X main.version=v0.1.0".
// It is the only package-level variable, and it is written once by the linker.
var version = "dev"

// shutdownTimeout bounds how long in-flight requests may take to finish, and
// how long main waits for the sampler afterwards.
const shutdownTimeout = 5 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hostmetrics:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Parse(os.Args[1:], os.LookupEnv, os.Stderr)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}))
	log.Info("starting", "version", version, "config", fmt.Sprintf("%+v", cfg))

	// signal.NotifyContext turns SIGINT/SIGTERM into context cancellation.
	// Everything long-lived (sampler, server) hangs off this context. On
	// Windows, Ctrl+C and a console close arrive as os.Interrupt.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	static := collector.ReadStatic(ctx, log)
	log.Info("host", "name", static.Host, "os", static.OS)
	sys := collector.NewSystem(ctx, log, cfg.Disk, cfg.NetIface)
	gpu := collector.NewNvidia(log)
	smp := sampler.New(log, sys, gpu, sampler.Config{
		Interval: cfg.Interval,
		Version:  version,
		Static:   static,
	})

	srv := &http.Server{
		Addr: cfg.Addr(),
		// /healthz reports stale after sampler.StaleTicks intervals without a
		// new snapshot: the same rule the sampler applies to its GPU reading.
		Handler: httpapi.NewHandler(smp, sampler.StaleTicks*cfg.Interval, log),
		// Timeouts keep a slow or malicious client from holding a connection
		// (and a goroutine) open forever. The payload is tiny, so these are
		// generous.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	samplerDone := make(chan struct{})
	go func() {
		defer close(samplerDone)
		smp.Run(ctx)
	}()

	serverErr := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", srv.Addr)
		serverErr <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case err := <-serverErr:
		stop()
		waitSampler(log, samplerDone)
		return fmt.Errorf("http server: %w", err)
	}

	// Graceful shutdown: stop accepting, let in-flight requests finish (bounded),
	// then wait for the sampler goroutine to observe the cancelled context.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Warn("http server shutdown", "err", err)
	}
	waitSampler(log, samplerDone)
	log.Info("stopped")
	return nil
}

// waitSampler waits for the sampler to stop, but not forever. Most of what a
// tick does ignores the context (gopsutil reads, a nvidia-smi stuck inside a
// wedged driver that even SIGKILL cannot reap), so an unbounded wait here is
// exactly how an agent ends up printing "shutdown signal received" and never
// exiting. Past the limit the process exits with the goroutine still running;
// the OS reclaims it.
func waitSampler(log *slog.Logger, samplerDone <-chan struct{}) {
	select {
	case <-samplerDone:
	case <-time.After(shutdownTimeout):
		log.Warn("sampler did not stop in time; exiting anyway", "waited", shutdownTimeout)
	}
}
