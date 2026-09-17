package config

import (
	"errors"
	"flag"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func envOf(m map[string]string) LookupEnv {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse(nil, envOf(nil), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 9900 || cfg.Bind != "0.0.0.0" || cfg.Interval != time.Second {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.Disk != "" || cfg.NetIface != "" {
		t.Errorf("disk/net-iface should default to auto (empty): %+v", cfg)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("log level = %v, want info", cfg.LogLevel)
	}
	if got := cfg.Addr(); got != "0.0.0.0:9900" {
		t.Errorf("Addr() = %q", got)
	}
}

func TestParseEnvThenFlagPrecedence(t *testing.T) {
	env := envOf(map[string]string{
		"HOSTMETRICS_PORT":      "8080",
		"HOSTMETRICS_INTERVAL":  "2s",
		"HOSTMETRICS_LOG_LEVEL": "DEBUG",
		"HOSTMETRICS_DISK":      "/data",
	})
	cfg, err := Parse([]string{"--port=7000"}, env, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 7000 {
		t.Errorf("flag must win over env: port = %d", cfg.Port)
	}
	if cfg.Interval != 2*time.Second {
		t.Errorf("env interval not applied: %s", cfg.Interval)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("env log level not applied: %v", cfg.LogLevel)
	}
	if cfg.Disk != "/data" {
		t.Errorf("env disk not applied: %q", cfg.Disk)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{"port too high", []string{"--port=70000"}, nil, "out of range"},
		{"port zero", []string{"--port=0"}, nil, "out of range"},
		{"interval too short", []string{"--interval=100ms"}, nil, "shorter than the minimum"},
		{"bad bind", []string{"--bind=localhost"}, nil, "not an IP"},
		{"bad log level", []string{"--log-level=loud"}, nil, "log-level"},
		{"positional arg", []string{"extra"}, nil, "unexpected argument"},
		{"malformed env int", nil, map[string]string{"HOSTMETRICS_PORT": "abc"}, "not an integer"},
		{"malformed env duration", nil, map[string]string{"HOSTMETRICS_INTERVAL": "1 sec"}, "not a duration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.args, envOf(tt.env), io.Discard)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestParseHelp(t *testing.T) {
	var out strings.Builder
	_, err := Parse([]string{"-h"}, envOf(nil), &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("want flag.ErrHelp, got %v", err)
	}
	if !strings.Contains(out.String(), "HOSTMETRICS_PORT") {
		t.Error("usage text should mention the environment variables")
	}
}
