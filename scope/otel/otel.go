// SPDX-License-Identifier: MIT

// Package otel adapts OpenTelemetry to the driver's Scope (metrics) and
// Tracer (spans) interfaces.
//
// Metrics: metric.Meter -> athenadriver.Scope (counters -> Int64Counter,
// timers -> Float64Histogram, unit ms). Instruments created lazily, cached
// by name.
//
//	meter := otel.Meter("athenadriver")
//	ctx = context.WithValue(ctx, drv.MetricsKey, otelscope.New(meter))
//
// Tracing: trace.Tracer -> athenadriver.Tracer. Every span is tagged
// SpanKindClient (database call) so OTel-aware backends render it as a DB
// span.
//
//	tracer := otel.Tracer("athenadriver")
//	ctx = context.WithValue(ctx, drv.TracerKey, otelscope.NewTracer(tracer))
package otel

import (
	"context"
	"fmt"
	"sync"
	"time"

	drv "github.com/CorkCyber/athenadriver/v2/go"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// New wraps an otel metric.Meter as drv.Scope. Nil meter -> drv.NoopScope,
// not a panicking adapter.
func New(meter metric.Meter) drv.Scope {
	if meter == nil {
		return drv.NoopScope
	}
	return &scope{
		meter:    meter,
		counters: map[string]drv.Counter{},
		timers:   map[string]drv.Timer{},
	}
}

// scope caches the wrapped drv.Counter/drv.Timer, not the raw instrument.
// The driver calls Scope().Counter(name) per call site, so a cache hit
// must not allocate.
type scope struct {
	meter    metric.Meter
	mu       sync.RWMutex
	counters map[string]drv.Counter
	timers   map[string]drv.Timer
}

func (s *scope) Counter(name string) drv.Counter {
	s.mu.RLock()
	c, ok := s.counters[name]
	s.mu.RUnlock()
	if ok {
		return c
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.counters[name]; ok { // another goroutine won the race
		return c
	}
	ic, err := s.meter.Int64Counter(name)
	if err != nil {
		c = drv.NoopScope.Counter(name)
	} else {
		c = counter{ic}
	}
	s.counters[name] = c
	return c
}

func (s *scope) Timer(name string) drv.Timer {
	s.mu.RLock()
	t, ok := s.timers[name]
	s.mu.RUnlock()
	if ok {
		return t
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.timers[name]; ok {
		return t
	}
	h, err := s.meter.Float64Histogram(name, metric.WithUnit("ms"))
	if err != nil {
		t = drv.NoopScope.Timer(name)
	} else {
		t = timer{h}
	}
	s.timers[name] = t
	return t
}

type counter struct{ c metric.Int64Counter }

func (a counter) Inc(delta int64) { a.c.Add(context.Background(), delta) }

type timer struct{ h metric.Float64Histogram }

func (a timer) Record(d time.Duration) {
	a.h.Record(context.Background(), float64(d)/float64(time.Millisecond))
}

// NewTracer wraps an otel trace.Tracer as drv.Tracer. Nil -> drv.NoopTracer,
// same as New.
func NewTracer(tracer trace.Tracer) drv.Tracer {
	if tracer == nil {
		return drv.NoopTracer
	}
	return tracerAdapter{tracer}
}

type tracerAdapter struct{ tracer trace.Tracer }

// StartSpan always starts a CLIENT-kind span. OTel's database semantic
// conventions require it for a backend to render this as a DB call.
func (t tracerAdapter) StartSpan(ctx context.Context, name string) (context.Context, drv.Span) {
	ctx, span := t.tracer.Start(ctx, name, trace.WithSpanKind(trace.SpanKindClient))
	return ctx, spanAdapter{span}
}

type spanAdapter struct{ span trace.Span }

func (s spanAdapter) SetAttr(key string, value any) {
	switch v := value.(type) {
	case string:
		s.span.SetAttributes(attribute.String(key, v))
	case bool:
		s.span.SetAttributes(attribute.Bool(key, v))
	case int64:
		s.span.SetAttributes(attribute.Int64(key, v))
	case float64:
		s.span.SetAttributes(attribute.Float64(key, v))
	default:
		s.span.SetAttributes(attribute.String(key, fmt.Sprint(v)))
	}
}

func (s spanAdapter) RecordError(err error) {
	if err == nil {
		return
	}
	s.span.RecordError(err)
	s.span.SetStatus(codes.Error, err.Error())
}

func (s spanAdapter) End() { s.span.End() }
