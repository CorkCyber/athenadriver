// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
)

// awaitQueryCompletion polls GetQueryExecution until the query reaches a
// terminal state (Succeeded / Failed / Cancelled), the caller's context is
// cancelled, or the per-statement-type timeout elapses. On any non-success
// outcome it best-effort issues StopQueryExecution so the server-side query
// is not left running off-ctx, and returns an appropriate error. queryStart
// is the time we issued StartQueryExecution (used for the per-statement
// timeout); loopStart is used for "time since polling began" metrics.
func (c *Connection) awaitQueryCompletion(ctx context.Context, queryID, wgName, query string, queryStart, loopStart time.Time) (*athenatypes.QueryExecution, error) {
	obs := c.tracer
	pollInterval := c.connector.config.PollInterval()
	maxPoll := c.connector.config.PollMaxInterval()
	multiplier := c.connector.config.PollBackoffMultiplier()
	// One timer reused across poll iterations. Avoids the per-iteration
	// allocation + leak of `time.After`, which keeps a timer alive until
	// it fires even if the select picked ctx.Done() instead.
	// Start it stopped/drained: the first GetQueryExecution can outlast
	// pollInterval, and under go1.22 timer semantics the stale fired value
	// survives the later Reset and would skip the first backoff wait.
	timer := time.NewTimer(pollInterval)
	defer timer.Stop()
	if !timer.Stop() {
		<-timer.C
	}
	for {
		// Short-circuit if the caller has already cancelled before we issue
		// the next GetQueryExecution. Avoids a wasted round-trip and lets us
		// reach the cancellation path that also issues StopQueryExecution.
		// stopQueryOnCancel already logs + counts its own failure; we must
		// return the bare ctx.Err() so callers doing `err == context.Canceled`
		// (or DeadlineExceeded) keep working.
		if err := ctx.Err(); err != nil {
			c.stopQueryOnCancel(ctx, queryID, wgName, query, obs, loopStart)
			return nil, err
		}
		statusResp, err := c.athenaClient.GetQueryExecution(ctx, &athena.GetQueryExecutionInput{
			QueryExecutionId: aws.String(queryID),
		})
		if err != nil {
			obs.Log(ErrorLevel, "GetQueryExecutionWithContext failed",
				slog.String("workgroup", wgName),
				slog.String("queryID", queryID),
				slog.String("error", err.Error()))
			obs.Scope().Counter(DriverName + ".failure.querycontext.getqueryexecutionwithcontext").Inc(1)
			// If the GetQueryExecution failure is itself caused by ctx
			// cancellation, the server-side query is still running and will
			// keep scanning bytes until it finishes. Stop it explicitly so
			// the caller is not billed for work they cancelled.
			if ctxErr := ctx.Err(); ctxErr != nil {
				c.stopQueryOnCancel(ctx, queryID, wgName, query, obs, loopStart)
				return nil, ctxErr
			}
			return nil, err
		}
		if statusResp == nil || statusResp.QueryExecution == nil ||
			statusResp.QueryExecution.Status == nil {
			obs.Scope().Counter(DriverName + ".failure.querycontext.getqueryexecutionwithcontext").Inc(1)
			return nil, fmt.Errorf("GetQueryExecution returned no status for query ID %q", queryID)
		}
		switch statusResp.QueryExecution.Status.State {
		case athenatypes.QueryExecutionStateCancelled:
			obs.Log(ErrorLevel, "QueryExecutionStateCancelled",
				slog.String("workgroup", wgName),
				slog.String("queryID", queryID))
			obs.Scope().Timer(DriverName + ".query.canceled").Record(time.Since(loopStart))
			if c.connector.config.MoneyWise {
				printCost(c.connector.config.RegionOrEnv(), statusResp)
			}
			return statusResp.QueryExecution, context.Canceled
		case athenatypes.QueryExecutionStateFailed:
			qErr := &QueryFailureError{
				Message: aws.ToString(statusResp.QueryExecution.Status.StateChangeReason),
			}
			if ae := statusResp.QueryExecution.Status.AthenaError; ae != nil {
				qErr.ErrorCategory = aws.ToInt32(ae.ErrorCategory)
				qErr.ErrorType = aws.ToInt32(ae.ErrorType)
				qErr.Retryable = ae.Retryable
				if qErr.Message == "" {
					qErr.Message = aws.ToString(ae.ErrorMessage)
				}
			}
			if qErr.Message == "" {
				qErr.Message = "query failed with no reason reported"
			}
			obs.Log(ErrorLevel, "QueryExecutionStateFailed",
				slog.String("workgroup", wgName),
				slog.String("queryID", queryID),
				slog.String("reason", qErr.Message))
			obs.Scope().Timer(DriverName + ".query.queryexecutionstatefailed").Record(time.Since(loopStart))
			return statusResp.QueryExecution, qErr
		case athenatypes.QueryExecutionStateSucceeded:
			if c.connector.config.MoneyWise {
				printCost(c.connector.config.RegionOrEnv(), statusResp)
			}
			obs.Scope().Timer(DriverName + ".query.queryexecutionstatesucceeded").Record(time.Since(loopStart))
			return statusResp.QueryExecution, nil
		}
		// Queued or Running — wait one poll interval, honoring ctx and the
		// per-statement timeout.
		timer.Reset(pollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			c.stopQueryOnCancel(ctx, queryID, wgName, query, obs, loopStart)
			return nil, ctx.Err()
		case <-timer.C:
			if isQueryTimeOut(queryStart, statusResp.QueryExecution.StatementType, c.connector.config.ServiceLimit) {
				obs.Log(ErrorLevel, "Query timeout failure",
					slog.String("workgroup", wgName),
					slog.String("queryID", queryID),
					slog.String("query", query))
				obs.Scope().Counter(DriverName + ".failure.querycontext.timeout").Inc(1)
				// Abandoning the query without stopping it leaves it running
				// and billing. Best-effort, per the doc comment.
				c.stopQueryOnCancel(ctx, queryID, wgName, query, obs, loopStart)
				return statusResp.QueryExecution, ErrQueryTimeout
			}
			if multiplier > 1 {
				next := min(time.Duration(float64(pollInterval)*multiplier), maxPoll)
				pollInterval = next
			}
		}
	}
}

// stopQueryOnCancel is the cleanup path when the caller's context is
// cancelled or the query is otherwise abandoned mid-flight. It issues
// StopQueryExecution on a detached copy of ctx (cancellation of ctx does not
// cancel the cleanup) bounded by a short timeout so it cannot hang for the
// full SDK retry budget, optionally records the final cost, and updates
// metrics/logs. Returns a non-nil error only if StopQueryExecution failed.
func (c *Connection) stopQueryOnCancel(ctx context.Context, queryID, wgName, query string, obs *DriverTracer, now time.Time) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	_, err := c.athenaClient.StopQueryExecution(cleanupCtx, &athena.StopQueryExecutionInput{
		QueryExecutionId: aws.String(queryID),
	})
	if err != nil {
		obs.Log(ErrorLevel, "StopQueryExecution failed",
			slog.String("workgroup", wgName),
			slog.String("queryID", queryID),
			slog.String("query", query))
		obs.Scope().Counter(DriverName + ".failure.querycontext.stopqueryexecution.failed").Inc(1)
		return err
	}
	if c.connector.config.MoneyWise {
		statusRespFinal, _ := c.athenaClient.GetQueryExecution(cleanupCtx, &athena.GetQueryExecutionInput{
			QueryExecutionId: aws.String(queryID),
		})
		printCost(c.connector.config.RegionOrEnv(), statusRespFinal)
	}
	obs.Scope().Counter(DriverName + ".failure.querycontext.stopqueryexecution.succeeded").Inc(1)
	obs.Scope().Timer(DriverName + ".query.StopQueryExecution").Record(time.Since(now))
	obs.Log(ErrorLevel, "query canceled", slog.String("queryID", queryID))
	return nil
}
