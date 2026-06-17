// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"io"
	"log/slog"

	"github.com/uber-go/tally/v4"
)

// Logging level re-exports so callers do not need to import log/slog
// separately. Aligned with slog.Level values.
const (
	DebugLevel = slog.LevelDebug
	InfoLevel  = slog.LevelInfo
	WarnLevel  = slog.LevelWarn
	ErrorLevel = slog.LevelError
)

// discardLogger drops all records; the default logger when none is
// supplied via NewObservability / context.
var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// DriverTracer wraps the slog logger + tally metrics scope used across the
// driver so call sites can fire structured logs and counters without
// branching on whether either is enabled.
type DriverTracer struct {
	logger *slog.Logger
	scope  tally.Scope
	config *Config
}

// NewObservability builds a tracer with the supplied logger and metrics
// scope. Either may be nil; in that case the discard logger / noop scope
// is used.
func NewObservability(config *Config, logger *slog.Logger, scope tally.Scope) *DriverTracer {
	if logger == nil {
		logger = discardLogger
	}
	if scope == nil {
		scope = tally.NoopScope
	}
	return &DriverTracer{logger: logger, scope: scope, config: config}
}

// NewDefaultObservability returns a tracer that discards logs and uses the
// tally noop scope. Used when no observability values are supplied via
// context or constructor.
func NewDefaultObservability(config *Config) *DriverTracer {
	return &DriverTracer{logger: discardLogger, scope: tally.NoopScope, config: config}
}

// NewNoOpsObservability is for testing purpose.
func NewNoOpsObservability() *DriverTracer {
	return &DriverTracer{logger: discardLogger, scope: tally.NoopScope, config: NewNoOpsConfig()}
}

// Logger returns the slog logger, or a discard logger if logging is
// disabled in Config.
func (c *DriverTracer) Logger() *slog.Logger {
	if !c.config.IsLoggingEnabled() {
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

// Scope returns the tally scope, or noop if metrics are disabled in Config.
func (c *DriverTracer) Scope() tally.Scope {
	if !c.config.IsMetricsEnabled() {
		return tally.NoopScope
	}
	return c.scope
}

// SetScope replaces the underlying tally scope.
func (c *DriverTracer) SetScope(scope tally.Scope) {
	c.scope = scope
}

// Log fires a structured log record at the given slog level. Panic / fatal
// levels intentionally have no analogue; the driver never wants a DB error
// to terminate the host process.
func (c *DriverTracer) Log(lvl slog.Level, msg string, attrs ...slog.Attr) {
	if !c.config.IsLoggingEnabled() {
		return
	}
	c.logger.LogAttrs(context.Background(), lvl, msg, attrs...)
}
