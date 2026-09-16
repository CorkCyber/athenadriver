// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	"github.com/stretchr/testify/assert"
)

// TestAwaitQueryCompletion_CancelReturnsBareCtxErr pins the contract that the
// cancellation path returns the bare context sentinel (so `err ==
// context.Canceled` works), whether or not the best-effort
// StopQueryExecution cleanup also failed. The cleanup failure is surfaced via
// stopQueryOnCancel's own log + counter, not by wrapping the returned error.
func TestAwaitQueryCompletion_CancelReturnsBareCtxErr(t *testing.T) {
	t.Parallel()
	c := &Connection{
		athenaClient: newMockAthenaClient(),
		connector:    NoopsSQLConnector(),
		tracer:       NewObservability(NewNoOpsConfig(), nil, nil),
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	now := time.Now()

	// QID not in stopQueryNoError => StopQueryExecution fails.
	_, err := c.awaitQueryCompletion(ctx, "stop-will-fail", "wg", "SELECT 1", now, now)
	assert.Equal(t, context.Canceled, err, "a failed cleanup must not wrap ctx.Err()")

	// QID whose StopQueryExecution succeeds.
	_, err = c.awaitQueryCompletion(ctx, "SELECTQueryContext_CANCEL_OK_QID", "wg", "SELECT 1", now, now)
	assert.Equal(t, context.Canceled, err)
}

// nilStatusAthenaClient returns a GetQueryExecution response with no
// QueryExecution, which the AWS API shape permits but the poll loop used to
// dereference unconditionally.
type nilStatusAthenaClient struct{ AthenaClient }

func (nilStatusAthenaClient) GetQueryExecution(context.Context, *athena.GetQueryExecutionInput, ...func(*athena.Options)) (*athena.GetQueryExecutionOutput, error) {
	return &athena.GetQueryExecutionOutput{}, nil
}

// TestAwaitQueryCompletion_NilStatus guards against a nil
// QueryExecution/Status in the GetQueryExecution response (an error, not a
// panic).
func TestAwaitQueryCompletion_NilStatus(t *testing.T) {
	t.Parallel()
	c := &Connection{
		athenaClient: nilStatusAthenaClient{newMockAthenaClient()},
		connector:    NoopsSQLConnector(),
		tracer:       NewObservability(NewNoOpsConfig(), nil, nil),
	}
	now := time.Now()
	_, err := c.awaitQueryCompletion(context.Background(), "NIL_QUERY_EXECUTION_QID", "wg", "SELECT 1", now, now)
	assert.ErrorContains(t, err, "returned no status")
}

// newPollTestConn builds a Connection on a fresh mock with poll timings
// short enough to exercise the backoff loop inside a unit test.
func newPollTestConn(mut ...func(*Config, *mockAthenaClient)) (*Connection, *mockAthenaClient) {
	nm := newMockAthenaClient()
	cfg := NewNoOpsConfig()
	for _, m := range mut {
		m(cfg, nm)
	}
	c := &Connection{
		athenaClient: nm,
		connector:    NoopsSQLConnector(),
		tracer:       NewObservability(cfg, nil, nil),
	}
	c.connector.config = cfg
	return c, nm
}

// TestStopQueryOnCancel_CleanupCtxIsDetached pins the cleanup contract: the
// StopQueryExecution that runs when the caller's ctx is already cancelled
// must receive a LIVE context (context.WithoutCancel) carrying its own
// deadline, otherwise the cancellation would cancel the very call meant to
// stop the server-side query — and the caller keeps getting billed.
func TestStopQueryOnCancel_CleanupCtxIsDetached(t *testing.T) {
	t.Parallel()
	c, nm := newPollTestConn()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	now := time.Now()
	_, err := c.awaitQueryCompletion(ctx, "SELECTQueryContext_CANCEL_OK_QID", "wg", "SELECT 1", now, now)
	assert.ErrorIs(t, err, context.Canceled)

	assert.Equal(t, 1, nm.callCount("StopQueryExecution"))
	assert.NoError(t, nm.stopCtxErr, "cleanup must not inherit the caller's cancellation")
	assert.True(t, nm.stopCtxHasDL, "cleanup ctx must be bounded by its own timeout")
	assert.InDelta(t, 10*time.Second, time.Until(nm.stopCtxDeadline), float64(2*time.Second))
}

// TestStopQueryOnCancel_HangingStopIsBounded: a StopQueryExecution that never
// answers must not hang the caller forever — the cleanup timeout caps it.
func TestStopQueryOnCancel_HangingStopIsBounded(t *testing.T) {
	t.Parallel()
	c, _ := newPollTestConn(func(_ *Config, nm *mockAthenaClient) {
		nm.stopBlocks = true
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := c.awaitQueryCompletion(ctx, "hangs-forever", "wg", "SELECT 1", start, start)
	elapsed := time.Since(start)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Greater(t, elapsed, 5*time.Second, "the cleanup really did block on the mock")
	assert.Less(t, elapsed, 20*time.Second, "the cleanup timeout must cap the wait")
}

// TestAwaitQueryCompletion_BackoffGrowsAndClamps exercises the poll-interval
// arithmetic: successive waits are multiplied, then pinned at the configured
// maximum. Timing tolerances are deliberately wide — this asserts the shape
// of the sequence, not exact scheduler latency.
func TestAwaitQueryCompletion_BackoffGrowsAndClamps(t *testing.T) {
	t.Parallel()
	const (
		initial = 40 * time.Millisecond
		maxPoll = 120 * time.Millisecond
	)
	c, nm := newPollTestConn(func(cfg *Config, nm *mockAthenaClient) {
		cfg.ResultPollInterval = initial
		cfg.ResultPollBackoffMultiplier = 2
		cfg.ResultPollMaxInterval = maxPoll
		nm.getQEFn = func(n int) (*athena.GetQueryExecutionOutput, error) {
			if n <= 5 {
				return qe("BACKOFF_QID", athenatypes.QueryExecutionStateQueued), nil
			}
			return qe("BACKOFF_QID", athenatypes.QueryExecutionStateSucceeded), nil
		}
	})

	now := time.Now()
	_, err := c.awaitQueryCompletion(context.Background(), "BACKOFF_QID", "wg", "SELECT 1", now, now)
	assert.NoError(t, err)

	at := nm.getQEAt
	assert.Len(t, at, 6)
	gaps := make([]time.Duration, 0, len(at)-1)
	for i := 1; i < len(at); i++ {
		gaps = append(gaps, at[i].Sub(at[i-1]))
	}
	t.Logf("poll gaps: %v", gaps)

	// Expected waits: 40ms, 80ms, then clamped at 120ms forever.
	assert.GreaterOrEqual(t, gaps[0], initial)
	assert.Greater(t, gaps[1], gaps[0], "the interval must grow")
	for _, g := range gaps[2:] {
		assert.GreaterOrEqual(t, g, maxPoll, "clamped interval is still the max")
		assert.Less(t, g, maxPoll+400*time.Millisecond, "the clamp must cap growth")
	}
}

// TestAwaitQueryCompletion_CancelDuringBackoffWait cancels while the loop is
// parked in the poll-timer select (not before the loop starts), which is the
// branch that has to both return context.Canceled and stop the server-side
// query.
func TestAwaitQueryCompletion_CancelDuringBackoffWait(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c, nm := newPollTestConn(func(cfg *Config, nm *mockAthenaClient) {
		// Long enough that the cancel below lands while we are waiting.
		cfg.ResultPollInterval = 10 * time.Second
		nm.getQEFn = func(n int) (*athena.GetQueryExecutionOutput, error) {
			if n == 1 {
				// The first poll succeeded; cancel once the loop is parked
				// in the select on the poll timer.
				go func() {
					time.Sleep(50 * time.Millisecond)
					cancel()
				}()
			}
			return qe("CANCEL_IN_WAIT_QID", athenatypes.QueryExecutionStateQueued), nil
		}
	})

	now := time.Now()
	_, err := c.awaitQueryCompletion(ctx, "CANCEL_IN_WAIT_QID", "wg", "SELECT 1", now, now)
	assert.True(t, errors.Is(err, context.Canceled), "got %v", err)
	assert.Equal(t, 1, nm.callCount("GetQueryExecution"), "cancel must land in the backoff wait")
	assert.Equal(t, []string{"CANCEL_IN_WAIT_QID"}, nm.stopQIDs,
		"the abandoned server-side query must be stopped exactly once")
}
