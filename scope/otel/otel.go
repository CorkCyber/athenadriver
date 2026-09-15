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
	return &scope{
		meter:    meter,
		counters: map[string]drv.Counter{},
		timers:   map[string]drv.Timer{},
	}
}

// scope caches the fully-wrapped drv.Counter/drv.Timer, not the raw otel
// instrument: the driver calls Scope().Counter(name) at the call site (once
// per unconvertible cell, say), so a cache hit must not allocate a wrapper.
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
