// Package config parses command-line flags and environment variables into a
// validated Config. Precedence is flag > environment > built-in default.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"time"
)

// EnvPrefix is prepended to every environment variable name.
const EnvPrefix = "HOSTMETRICS_"

// MinInterval is the smallest sampler period accepted. nvidia-smi alone costs
// 100-300 ms per call, so anything shorter than this would make the tick
// longer than the interval and the ticker would start dropping ticks.
const MinInterval = 500 * time.Millisecond

// Config holds every runtime option of the agent.
type Config struct {
	Port     int
	Bind     string
	Interval time.Duration
	Disk     string // "" means the OS root volume
	NetIface string // "" means the busiest non-loopback interface at startup
	LogLevel slog.Level
}

// LookupEnv mirrors os.LookupEnv. It is injected so tests never touch the
// real environment.
type LookupEnv func(key string) (string, bool)

// Parse builds a Config from args (without the program name) and the
// environment. Usage text goes to usageOut. It returns flag.ErrHelp when the
// user asked for -h/--help.
func Parse(args []string, lookupEnv LookupEnv, usageOut io.Writer) (Config, error) {
	// Environment values become the flag defaults, so `--help` shows the
	// effective default and a flag still wins when both are given. Malformed
	// environment values are reported instead of silently falling back.
	var envErrs []error
	env := func(key, def string) string {
		if v, ok := lookupEnv(EnvPrefix + key); ok {
			return v
		}
		return def
	}
	envInt := func(key string, def int) int {
		s := env(key, "")
		if s == "" {
			return def
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			envErrs = append(envErrs, fmt.Errorf("%s%s: %q is not an integer", EnvPrefix, key, s))
			return def
		}
		return n
	}
	envDuration := func(key string, def time.Duration) time.Duration {
		s := env(key, "")
		if s == "" {
			return def
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			envErrs = append(envErrs, fmt.Errorf("%s%s: %q is not a duration (e.g. 1s, 500ms)", EnvPrefix, key, s))
			return def
		}
		return d
	}

	fs := flag.NewFlagSet("hostmetrics", flag.ContinueOnError)
	fs.SetOutput(usageOut)

	var cfg Config
	var logLevel string
	fs.IntVar(&cfg.Port, "port", envInt("PORT", 9900), "HTTP port (HOSTMETRICS_PORT)")
	fs.StringVar(&cfg.Bind, "bind", env("BIND", "0.0.0.0"), "bind address (HOSTMETRICS_BIND)")
	fs.DurationVar(&cfg.Interval, "interval", envDuration("INTERVAL", time.Second), "sampler period (HOSTMETRICS_INTERVAL)")
	fs.StringVar(&cfg.Disk, "disk", env("DISK", ""), "volume to monitor; default: OS root (HOSTMETRICS_DISK)")
	fs.StringVar(&cfg.NetIface, "net-iface", env("NET_IFACE", ""), "network interface; default: busiest at startup (HOSTMETRICS_NET_IFACE)")
	fs.StringVar(&logLevel, "log-level", env("LOG_LEVEL", "info"), "log level: debug, info, warn, error (HOSTMETRICS_LOG_LEVEL)")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if len(envErrs) > 0 {
		return Config{}, errors.Join(envErrs...)
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	// slog.Level knows how to parse its own names, case-insensitively.
	if err := cfg.LogLevel.UnmarshalText([]byte(logLevel)); err != nil {
		return Config{}, fmt.Errorf("log-level: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port: %d is out of range 1-65535", c.Port)
	}
	if c.Bind != "" && net.ParseIP(c.Bind) == nil {
		return fmt.Errorf("bind: %q is not an IP address", c.Bind)
	}
	if c.Interval < MinInterval {
		return fmt.Errorf("interval: %s is shorter than the minimum %s", c.Interval, MinInterval)
	}
	return nil
}

// Addr returns the host:port the HTTP server should listen on.
func (c Config) Addr() string {
	return net.JoinHostPort(c.Bind, strconv.Itoa(c.Port))
}
