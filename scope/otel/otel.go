// SPDX-License-Identifier: MIT

// Package otel adapts an OpenTelemetry go.opentelemetry.io/otel/metric.Meter
// to the driver's athenadriver.Scope interface. Counters map to
// Int64Counter, timers to Float64Histogram (unit ms).
//
// Usage:
//
//	meter := otel.Meter("athenadriver")
//	adapter := otelscope.New(meter)
//	ctx = context.WithValue(ctx, drv.MetricsKey, adapter)
//
// Instruments are created lazily on first reference and cached by name.
// A shared background context is used for record calls; supply per-call
// attributes upstream if you need request-scoped labels.
package otel

import (
	"context"
	"sync"
	"time"

	drv "github.com/CorkCyber/athenadriver/v2/go"
	"go.opentelemetry.io/otel/metric"
)

// New wraps an otel metric.Meter so it satisfies drv.Scope. A nil meter
// returns drv.NoopScope instead of an adapter that panics on first use —
// mirrors the driver's own nil handling in NewObservability.
func New(meter metric.Meter) drv.Scope {
	if meter == nil {
		return drv.NoopScope
	}
	return &scope{meter: meter}
}

type scope struct {
	meter    metric.Meter
	mu       sync.Mutex
	counters map[string]metric.Int64Counter
	timers   map[string]metric.Float64Histogram
}

func (s *scope) Counter(name string) drv.Counter {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.counters == nil {
		s.counters = map[string]metric.Int64Counter{}
	}
	c, ok := s.counters[name]
	if !ok {
		var err error
		c, err = s.meter.Int64Counter(name)
		if err != nil {
			return drv.NoopScope.Counter(name)
		}
		s.counters[name] = c
	}
	return counter{c}
}

func (s *scope) Timer(name string) drv.Timer {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timers == nil {
		s.timers = map[string]metric.Float64Histogram{}
	}
	h, ok := s.timers[name]
	if !ok {
		var err error
		h, err = s.meter.Float64Histogram(name, metric.WithUnit("ms"))
		if err != nil {
			return drv.NoopScope.Timer(name)
		}
		s.timers[name] = h
	}
	return timer{h}
}

type counter struct{ c metric.Int64Counter }

func (a counter) Inc(delta int64) { a.c.Add(context.Background(), delta) }

type timer struct{ h metric.Float64Histogram }

func (a timer) Record(d time.Duration) {
	a.h.Record(context.Background(), float64(d)/float64(time.Millisecond))
}
