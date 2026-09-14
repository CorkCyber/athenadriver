// SPDX-License-Identifier: MIT

package tally

import (
	"testing"
	"time"

	drv "github.com/CorkCyber/athenadriver/v2/go"
	"github.com/uber-go/tally/v4"
)

func TestAdapterSatisfiesScope(t *testing.T) {
	var _ drv.Scope = New(tally.NoopScope)
}

// TestAdapterLiveFire uses tally's TestScope so we can Snapshot()
// counter + timer values and assert the adapter forwards them intact.
func TestAdapterLiveFire(t *testing.T) {
	test := tally.NewTestScope("", nil)
	s := New(test)

	s.Counter("driver.calls").Inc(3)
	s.Counter("driver.calls").Inc(2) // repeat name must land on the same counter
	s.Timer("driver.latency").Record(42 * time.Millisecond)
	s.Timer("driver.latency").Record(8 * time.Millisecond)

	snap := test.Snapshot()
	counters := snap.Counters()
	if got := counters["driver.calls+"].Value(); got != 5 {
		t.Errorf("counter value = %d, want 5", got)
	}
	timers := snap.Timers()
	values := timers["driver.latency+"].Values()
	if len(values) != 2 {
		t.Fatalf("timer sample count = %d, want 2", len(values))
	}
	want := []time.Duration{42 * time.Millisecond, 8 * time.Millisecond}
	// Order is insertion order in TestScope.
	for i, v := range values {
		if v != want[i] {
			t.Errorf("timer[%d] = %v, want %v", i, v, want[i])
		}
	}
}

// TestAdapterNilScope pins the nil-input contract: New(nil) degrades to
// the driver's NoopScope instead of panicking on first use.
func TestAdapterNilScope(t *testing.T) {
	s := New(nil)
	if s != drv.NoopScope {
		t.Fatalf("New(nil) = %T, want drv.NoopScope", s)
	}
	s.Counter("k").Inc(1) // must not panic
	s.Timer("t").Record(time.Millisecond)
}
