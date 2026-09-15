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

// Scope is the minimal metrics surface the driver emits into: bridge to
// tally, prometheus, OpenTelemetry, etc. with a small adapter (the driver
// only calls Counter and Timer). Must be safe for concurrent use: one Scope
// is shared across every pooled connection this driver produces.
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

// Tracer starts a span for one Athena operation. Bridge to OpenTelemetry,
// Sentry, Datadog APM, etc. via a small adapter; the driver only calls
// StartSpan. Every span is a database client call, so mark it accordingly
// (e.g. OTel's SpanKindClient). Must be safe for concurrent use: shared
// across every pooled connection.
type Tracer interface {
	// StartSpan starts a new span named name as a child of ctx (if ctx
	// carries a parent span) and returns a context carrying the new span
	// alongside the span itself.
	StartSpan(ctx context.Context, name string) (context.Context, Span)
}

// Span is one traced operation. SetAttr accepts string, bool, int64, and
// float64 values; an adapter may ignore or stringify any other type.
type Span interface {
	SetAttr(key string, value any)
	RecordError(err error)
	End()
}

// NoopTracer discards all spans. Used as the default when tracing is
// disabled or no tracer is supplied.
var NoopTracer Tracer = noopTracer{}

type noopTracer struct{}
type noopSpan struct{}

func (noopTracer) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	return ctx, noopSpan{}
}
func (noopSpan) SetAttr(string, any) {}
func (noopSpan) RecordError(error)   {}
func (noopSpan) End()                {}

// discardLogger drops all records; the default logger when none is
// supplied via NewObservability / context.
var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// DriverTracer wraps the slog logger + metrics scope + span tracer used
// across the driver so call sites can fire structured logs, counters, and
// spans without branching on whether any of the three is enabled.
type DriverTracer struct {
	logger *slog.Logger
	scope  Scope
	spans  Tracer
	config *Config
}

// NewObservability builds a tracer with the given logger and metrics scope.
// All three args may be nil: logger/scope fall back to the discard logger /
// noop scope, config falls back to NewNoOpsConfig() (logging/metrics/tracing
// all enabled). Span tracer defaults to NoopTracer; set one via SetTracer.
func NewObservability(config *Config, logger *slog.Logger, scope Scope) *DriverTracer {
	if config == nil {
		config = NewNoOpsConfig()
	}
	if logger == nil {
		logger = discardLogger
	}
	if scope == nil {
		scope = NoopScope
	}
	return &DriverTracer{logger: logger, scope: scope, spans: NoopTracer, config: config}
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

// StartSpan starts a span via the underlying Tracer, or a no-op span if
// tracing is disabled or no Tracer is set. ctx's TracerKey overrides the
// bound Tracer for this call only (not stored). Tolerates a Tracer/Span
// that returns nil or panics (e.g. a third-party `if !sampled { return
// ctx, nil }`): a bug we don't control must not crash the caller's query.
func (c *DriverTracer) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	if !c.config.TracingEnabled {
		return ctx, noopSpan{}
	}
	tracer := c.spans
	if override, ok := ctx.Value(TracerKey).(Tracer); ok {
		tracer = override
	}
	if tracer == nil {
		return ctx, noopSpan{}
	}
	sctx, span := safeStartSpan(tracer, ctx, name)
	if sctx == nil {
		sctx = ctx
	}
	if span == nil {
		return sctx, noopSpan{}
	}
	if _, isNoop := span.(noopSpan); isNoop {
		return sctx, span
	}
	return sctx, panicSafeSpan{span}
}

// safeStartSpan isolates a query from a panic inside a third-party Tracer:
// a bug in code this driver does not control must degrade to "no span for
// this query," not a crashed query in every caller's goroutine.
func safeStartSpan(tracer Tracer, ctx context.Context, name string) (sctx context.Context, span Span) {
	defer recoverPanic()
	return tracer.StartSpan(ctx, name)
}

// panicSafeSpan applies the same panic isolation to every Span method, so a
// misbehaving Tracer's Span implementation can't crash a query either.
type panicSafeSpan struct{ Span }

func recoverPanic() { recover() }

func (s panicSafeSpan) SetAttr(key string, value any) {
	defer recoverPanic()
	s.Span.SetAttr(key, value)
}
func (s panicSafeSpan) RecordError(err error) { defer recoverPanic(); s.Span.RecordError(err) }
func (s panicSafeSpan) End()                  { defer recoverPanic(); s.Span.End() }

// SetTracer replaces the underlying span tracer.
func (c *DriverTracer) SetTracer(tracer Tracer) {
	if tracer == nil {
		tracer = NoopTracer
	}
	c.spans = tracer
}

// Log fires a structured log record at the given slog level with a
// background context. Panic / fatal levels intentionally have no analogue;
// the driver never wants a DB error to terminate the host process. Prefer
// LogCtx where a caller context is in hand.
func (c *DriverTracer) Log(lvl slog.Level, msg string, attrs ...slog.Attr) {
	c.LogCtx(context.Background(), lvl, msg, attrs...)
}

// LogCtx is Log with a caller-supplied context, so handlers that read trace /
// span IDs off the context (e.g. an OTel bridge) can correlate driver log
// records with the surrounding request.
func (c *DriverTracer) LogCtx(ctx context.Context, lvl slog.Level, msg string, attrs ...slog.Attr) {
	if !c.config.LoggingEnabled {
		return
	}
	c.logger.LogAttrs(ctx, lvl, msg, attrs...)
}
