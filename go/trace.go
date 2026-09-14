// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"io"
	"log/slog"
	"time"
)

// Logging level re-exports so callers do not need to import log/slog
// separately. Aligned with slog.Level values.
const (
	DebugLevel = slog.LevelDebug
	InfoLevel  = slog.LevelInfo
	WarnLevel  = slog.LevelWarn
	ErrorLevel = slog.LevelError
)

// Scope is the minimal metrics surface the driver emits into. Bridge to
// tally, prometheus, OpenTelemetry, etc. by wrapping their types in a
// small adapter — the driver only calls Counter and Timer.
type Scope interface {
	Counter(name string) Counter
	Timer(name string) Timer
}

// Counter is a monotonically increasing integer. Inc is called with 1
// throughout the driver; delta is exposed for adapter flexibility.
type Counter interface {
	Inc(delta int64)
}

// Timer records a duration observation. Record is the only method the
// driver calls.
type Timer interface {
	Record(d time.Duration)
}

// NoopScope discards all metrics. Used as the default when metrics are
// disabled or no scope is supplied.
var NoopScope Scope = noopScope{}

type noopScope struct{}
type noopCounter struct{}
type noopTimer struct{}

func (noopScope) Counter(string) Counter { return noopCounter{} }
func (noopScope) Timer(string) Timer     { return noopTimer{} }
func (noopCounter) Inc(int64)            {}
func (noopTimer) Record(time.Duration)   {}

// discardLogger drops all records; the default logger when none is
// supplied via NewObservability / context.
var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// DriverTracer wraps the slog logger + metrics scope used across the
// driver so call sites can fire structured logs and counters without
// branching on whether either is enabled.
type DriverTracer struct {
	logger *slog.Logger
	scope  Scope
	config *Config
}

// NewObservability builds a tracer with the supplied logger and metrics
// scope. Either may be nil; in that case the discard logger / noop scope
// is used.
func NewObservability(config *Config, logger *slog.Logger, scope Scope) *DriverTracer {
	if logger == nil {
		logger = discardLogger
	}
	if scope == nil {
		scope = NoopScope
	}
	return &DriverTracer{logger: logger, scope: scope, config: config}
}

// Logger returns the slog logger, or a discard logger if logging is
// disabled in Config.
func (c *DriverTracer) Logger() *slog.Logger {
	if !c.config.LoggingEnabled {
		return discardLogger
	}
	return c.logger
}

// SetLogger replaces the underlying slog logger.
func (c *DriverTracer) SetLogger(logger *slog.Logger) {
	if logger == nil {
		logger = discardLogger
	}
	c.logger = logger
}

// Scope returns the metrics scope, or NoopScope if metrics are disabled
// in Config.
func (c *DriverTracer) Scope() Scope {
	if !c.config.MetricsEnabled {
		return NoopScope
	}
	return c.scope
}

// SetScope replaces the underlying metrics scope.
func (c *DriverTracer) SetScope(scope Scope) {
	if scope == nil {
		scope = NoopScope
	}
	c.scope = scope
}

// Log fires a structured log record at the given slog level. Panic / fatal
// levels intentionally have no analogue; the driver never wants a DB error
// to terminate the host process.
func (c *DriverTracer) Log(lvl slog.Level, msg string, attrs ...slog.Attr) {
	if !c.config.LoggingEnabled {
		return
	}
	c.logger.LogAttrs(context.Background(), lvl, msg, attrs...)
}
