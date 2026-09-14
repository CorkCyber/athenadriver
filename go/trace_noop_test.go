// SPDX-License-Identifier: MIT

package athenadriver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// NoopScope must satisfy the Scope interface AND its returned Counter /
// Timer must accept calls without panicking or allocating observable state.
// Callers that inject NoopScope rely on both properties in hot paths.

func TestNoopScope_ImplementsScope(t *testing.T) {
	var _ Scope = NoopScope
}

func TestNoopScope_Counter_Inc(t *testing.T) {
	c := NoopScope.Counter("any.name")
	assert.NotNil(t, c)
	// Multiple calls with a variety of deltas must not panic.
	c.Inc(0)
	c.Inc(1)
	c.Inc(-5)
	c.Inc(1 << 30)
}

func TestNoopScope_Timer_Record(t *testing.T) {
	tm := NoopScope.Timer("any.timing")
	assert.NotNil(t, tm)
	tm.Record(0)
	tm.Record(500 * time.Microsecond)
	tm.Record(-1) // negative durations are nonsense but should not panic
}
