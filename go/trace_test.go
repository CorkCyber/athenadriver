// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestObservability_Scope(t *testing.T) {
	obs := NewObservability(NewNoOpsConfig(), nil, nil)
	assert.Equal(t, obs.Scope(), NoopScope)

	config := NewNoOpsConfig()
	config.MetricsEnabled = true
	obs = NewObservability(config, nil, nil)
	assert.Equal(t, obs.Scope(), NoopScope)
}

func TestObservability_Logger(t *testing.T) {
	obs := NewObservability(NewNoOpsConfig(), nil, nil)
	assert.NotNil(t, obs.Logger())

	config := NewNoOpsConfig()
	config.LoggingEnabled = false
	obs = NewObservability(config, nil, nil)
	// Logging disabled -> Logger() returns the discard logger, which is
	// the same singleton it would return as the default.
	assert.NotNil(t, obs.Logger())
}

func TestObservability_Log(t *testing.T) {
	config := NewNoOpsConfig()
	config.LoggingEnabled = false
	obs := NewObservability(config, nil, nil)
	obs.Log(-1, "")
	config.LoggingEnabled = true
	obs = NewObservability(config, nil, nil)
	obs.Log(-1, "")
	obs.Log(ErrorLevel, "")
	obs.Log(WarnLevel, "")
	obs.Log(InfoLevel, "")
	obs.Log(DebugLevel, "")
}

func TestObservability_SetScope(t *testing.T) {
	obs := NewObservability(NewNoOpsConfig(), nil, nil)
	obs.SetScope(NoopScope)
	assert.Equal(t, obs.Scope(), NoopScope)
}

func TestObservability_SetLogger(t *testing.T) {
	obs := NewObservability(NewNoOpsConfig(), nil, nil)
	// SetLogger(nil) installs the discard logger rather than leaving the
	// field nil so callers can always invoke Logger() safely.
	obs.SetLogger(nil)
	assert.NotNil(t, obs.Logger())
}

func TestObservability_NewObservability(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	obs := NewObservability(NewNoOpsConfig(), logger, NoopScope)
	assert.NotNil(t, obs.Logger())
}

// TestObservability_NewObservability_AllNil pins the documented nil-arg
// fallback: config -> NewNoOpsConfig(), logger -> discard, scope -> NoopScope.
func TestObservability_NewObservability_AllNil(t *testing.T) {
	obs := NewObservability(nil, nil, nil)
	assert.NotNil(t, obs)
	assert.NotNil(t, obs.Logger())
	assert.Equal(t, NoopScope, obs.Scope())
	assert.NotPanics(t, func() {
		obs.Logger().Info("no panic on nil-built observability")
	})
}

// ctxCapturingHandler records the context each record is emitted with, which
// is how a real handler (e.g. an OTel bridge) picks up trace/span IDs.
type ctxCapturingHandler struct{ ctxs []context.Context }

func (h *ctxCapturingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *ctxCapturingHandler) Handle(ctx context.Context, _ slog.Record) error {
	h.ctxs = append(h.ctxs, ctx)
	return nil
}
func (h *ctxCapturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *ctxCapturingHandler) WithGroup(string) slog.Handler      { return h }

type ctxKeyT struct{}

func TestObservability_LogCtx(t *testing.T) {
	h := &ctxCapturingHandler{}
	config := NewNoOpsConfig()
	config.LoggingEnabled = true
	obs := NewObservability(config, slog.New(h), nil)

	ctx := context.WithValue(context.Background(), ctxKeyT{}, "trace-42")
	obs.LogCtx(ctx, InfoLevel, "with ctx")
	obs.Log(InfoLevel, "without ctx")

	assert.Len(t, h.ctxs, 2)
	assert.Equal(t, "trace-42", h.ctxs[0].Value(ctxKeyT{}))
	assert.Nil(t, h.ctxs[1].Value(ctxKeyT{}))

	// Logging disabled short-circuits before the handler, same as Log.
	config.LoggingEnabled = false
	obs.LogCtx(ctx, InfoLevel, "dropped")
	assert.Len(t, h.ctxs, 2)
}

// recordingTracer/recordingSpan capture what the driver reports, mirroring
// how a real adapter (scope/otel) would receive the same calls.
type recordingTracer struct {
	startedName string
	span        *recordingSpan
}

func (t *recordingTracer) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	t.startedName = name
	t.span = &recordingSpan{}
	return ctx, t.span
}

type recordingSpan struct {
	attrs map[string]any
	errs  []error
	ended bool
}

func (s *recordingSpan) SetAttr(key string, value any) {
	if s.attrs == nil {
		s.attrs = map[string]any{}
	}
	s.attrs[key] = value
}
func (s *recordingSpan) RecordError(err error) { s.errs = append(s.errs, err) }
func (s *recordingSpan) End()                  { s.ended = true }

func TestObservability_StartSpan_NoopByDefault(t *testing.T) {
	obs := NewObservability(NewNoOpsConfig(), nil, nil)
	_, span := obs.StartSpan(context.Background(), "athena.query")
	assert.Equal(t, noopSpan{}, span)
}

func TestObservability_StartSpan_TracingDisabled(t *testing.T) {
	config := NewNoOpsConfig()
	config.TracingEnabled = false
	obs := NewObservability(config, nil, nil)
	tracer := &recordingTracer{}
	obs.SetTracer(tracer)

	_, span := obs.StartSpan(context.Background(), "athena.query")
	assert.Equal(t, noopSpan{}, span)
	assert.Empty(t, tracer.startedName, "a disabled tracer must not be called at all")
}

func TestObservability_StartSpan_UsesConfiguredTracer(t *testing.T) {
	config := NewNoOpsConfig()
	config.TracingEnabled = true
	obs := NewObservability(config, nil, nil)
	tracer := &recordingTracer{}
	obs.SetTracer(tracer)

	ctx, span := obs.StartSpan(context.Background(), "athena.query")
	assert.NotNil(t, ctx)
	assert.Equal(t, "athena.query", tracer.startedName)
	span.SetAttr("db.system.name", "trino")
	span.RecordError(assert.AnError)
	span.End()
	assert.Equal(t, "trino", tracer.span.attrs["db.system.name"])
	assert.Equal(t, []error{assert.AnError}, tracer.span.errs)
	assert.True(t, tracer.span.ended)
}

func TestObservability_SetTracer_NilInstallsNoop(t *testing.T) {
	obs := NewObservability(NewNoOpsConfig(), nil, nil)
	obs.SetTracer(nil)
	_, span := obs.StartSpan(context.Background(), "athena.query")
	assert.Equal(t, noopSpan{}, span)
}

// nilTracer is a deliberately misbehaving Tracer, standing in for a
// third-party implementation with an early-return path (an unsampled call,
// say) that returns a nil Span or a nil context.
type nilTracer struct{ returnNilCtx bool }

func (t nilTracer) StartSpan(ctx context.Context, name string) (context.Context, Span) {
	if t.returnNilCtx {
		return nil, &recordingSpan{}
	}
	return ctx, nil
}

func TestObservability_StartSpan_TolerantOfMisbehavingTracer(t *testing.T) {
	config := NewNoOpsConfig()
	obs := NewObservability(config, nil, nil)

	obs.SetTracer(nilTracer{})
	ctx, span := obs.StartSpan(context.Background(), "athena.query")
	assert.NotNil(t, ctx)
	assert.NotNil(t, span)
	span.SetAttr("k", "v") // must not panic

	obs.SetTracer(nilTracer{returnNilCtx: true})
	ctx, span = obs.StartSpan(context.Background(), "athena.query")
	assert.NotNil(t, ctx)
	assert.NotNil(t, span)
}

// panickyTracer/panickySpan stand in for a third-party Tracer with a bug,
// e.g. a nil-pointer deref in its own SDK's span-start path. A bug in code
// this driver does not control must not crash the caller's query.
type panickyTracer struct{ panicOnStart bool }

func (t panickyTracer) StartSpan(context.Context, string) (context.Context, Span) {
	if t.panicOnStart {
		panic("boom: third-party Tracer bug")
	}
	return context.Background(), panickySpan{}
}

type panickySpan struct{}

func (panickySpan) SetAttr(string, any) { panic("boom: third-party Span.SetAttr bug") }
func (panickySpan) RecordError(error)   { panic("boom: third-party Span.RecordError bug") }
func (panickySpan) End()                { panic("boom: third-party Span.End bug") }

func TestObservability_StartSpan_TolerantOfPanickingTracer(t *testing.T) {
	obs := NewObservability(NewNoOpsConfig(), nil, nil)
	obs.SetTracer(panickyTracer{panicOnStart: true})

	assert.NotPanics(t, func() {
		ctx, span := obs.StartSpan(context.Background(), "athena.query")
		assert.NotNil(t, ctx)
		assert.NotNil(t, span)
	})
}

func TestObservability_StartSpan_TolerantOfPanickingSpan(t *testing.T) {
	obs := NewObservability(NewNoOpsConfig(), nil, nil)
	obs.SetTracer(panickyTracer{})

	assert.NotPanics(t, func() {
		_, span := obs.StartSpan(context.Background(), "athena.query")
		span.SetAttr("k", "v")
		span.RecordError(assert.AnError)
		span.End()
	})
}

// TestObservability_StartSpan_PerQueryTracerKeyOverride proves a Tracer
// supplied via ctx for one call wins over the Tracer bound to the
// connection, without being persisted.
func TestObservability_StartSpan_PerQueryTracerKeyOverride(t *testing.T) {
	config := NewNoOpsConfig()
	obs := NewObservability(config, nil, nil)
	bound := &recordingTracer{}
	obs.SetTracer(bound)

	override := &recordingTracer{}
	ctx := context.WithValue(context.Background(), TracerKey, Tracer(override))
	_, _ = obs.StartSpan(ctx, "athena.query")
	assert.Equal(t, "athena.query", override.startedName, "the ctx-supplied Tracer must be used for this call")
	assert.Empty(t, bound.startedName, "the connection-bound Tracer must not also fire")

	// The override is not persisted: the next call with no override reverts
	// to the connection-bound Tracer.
	_, _ = obs.StartSpan(context.Background(), "athena.query")
	assert.Equal(t, "athena.query", bound.startedName)
}

// TestObservability_StartSpan_PerQueryTracerKeyOverrideToNoop proves the
// per-query override also works to SILENCE one call on an otherwise-traced
// connection (ctx-supplied NoopTracer), and that the noop path isn't
// double-wrapped by panicSafeSpan.
func TestObservability_StartSpan_PerQueryTracerKeyOverrideToNoop(t *testing.T) {
	obs := NewObservability(NewNoOpsConfig(), nil, nil)
	bound := &recordingTracer{}
	obs.SetTracer(bound)

	ctx := context.WithValue(context.Background(), TracerKey, NoopTracer)
	_, span := obs.StartSpan(ctx, "athena.query")
	assert.Equal(t, noopSpan{}, span)
	assert.Empty(t, bound.startedName, "the connection-bound Tracer must not fire when overridden to NoopTracer")
}
