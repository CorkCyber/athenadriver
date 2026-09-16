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
