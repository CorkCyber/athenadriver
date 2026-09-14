// SPDX-License-Identifier: MIT

// Package statsd adapts a github.com/cactus/go-statsd-client/v5 Statter to
// the driver's athenadriver.Scope interface. Counters map to statsd
// increments; timers map to statsd timings (millisecond resolution).
//
// Usage:
//
//	statter, _ := statsd.NewBufferedClient("127.0.0.1:8125",
//	    "myapp", 100*time.Millisecond, 1440)
//	defer statter.Close()
//	ctx = context.WithValue(ctx, drv.MetricsKey, statsdscope.New(statter))
package statsd

import (
	"time"

	drv "github.com/CorkCyber/athenadriver/v2/go"
	"github.com/cactus/go-statsd-client/v5/statsd"
)

// SampleRate is the statsd sample rate applied to every observation.
// Callers who need fractional sampling can supply their own adapter or
// use NewWithRate.
const SampleRate float32 = 1.0

// New wraps a statsd Statter so it satisfies drv.Scope. Uses sample
// rate 1.0 (report every observation). A nil statter returns
// drv.NoopScope instead of an adapter that panics on first use,
// mirroring the driver's own nil handling in NewObservability.
func New(s statsd.Statter) drv.Scope {
	return NewWithRate(s, SampleRate)
}

// NewWithRate is New with an explicit sample rate in [0, 1].
func NewWithRate(s statsd.Statter, rate float32) drv.Scope {
	if s == nil {
		return drv.NoopScope
	}
	return scope{s: s, rate: rate}
}

type scope struct {
	s    statsd.Statter
	rate float32
}

func (a scope) Counter(name string) drv.Counter { return counter{name: name, s: a.s, rate: a.rate} }
func (a scope) Timer(name string) drv.Timer     { return timer{name: name, s: a.s, rate: a.rate} }

type counter struct {
	name string
	s    statsd.Statter
	rate float32
}

func (c counter) Inc(delta int64) { _ = c.s.Inc(c.name, delta, c.rate) }

type timer struct {
	name string
	s    statsd.Statter
	rate float32
}

func (t timer) Record(d time.Duration) {
	_ = t.s.TimingDuration(t.name, d, t.rate)
}
