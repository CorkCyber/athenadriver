// SPDX-License-Identifier: MIT

// Package tally adapts a github.com/uber-go/tally/v4 Scope to the driver's
// athenadriver.Scope interface. Usage:
//
//	rootScope, _ := tally.NewRootScope(tally.ScopeOptions{...}, time.Second)
//	adapter := tallyscope.New(rootScope)
//	ctx = context.WithValue(ctx, drv.MetricsKey, adapter)
package tally

import (
	"time"

	drv "github.com/CorkCyber/athenadriver/v2/go"
	"github.com/uber-go/tally/v4"
)

// New wraps a tally.Scope so it satisfies drv.Scope. A nil scope returns
// drv.NoopScope instead of an adapter that panics on first use, mirroring
// the driver's own nil handling in NewObservability.
func New(s tally.Scope) drv.Scope {
	if s == nil {
		return drv.NoopScope
	}
	return scope{s}
}

type scope struct{ s tally.Scope }

func (a scope) Counter(name string) drv.Counter { return counter{a.s.Counter(name)} }
func (a scope) Timer(name string) drv.Timer     { return timer{a.s.Timer(name)} }

type counter struct{ c tally.Counter }

func (a counter) Inc(delta int64) { a.c.Inc(delta) }

type timer struct{ t tally.Timer }

func (a timer) Record(d time.Duration) { a.t.Record(d) }
