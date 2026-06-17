// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"errors"
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
	obs := c.connector.tracer
	pollInterval := c.connector.config.GetResultPollIntervalSeconds()
	maxPoll := c.connector.config.GetResultPollMaxInterval()
	multiplier := c.connector.config.GetResultPollBackoffMultiplier()
	// One timer reused across poll iterations. Avoids the per-iteration
	// allocation + leak of `time.After`, which keeps a timer alive until
	// it fires even if the select picked ctx.Done() instead.
	timer := time.NewTimer(pollInterval)
	defer timer.Stop()
	for {
		// Short-circuit if the caller has already cancelled before we issue
		// the next GetQueryExecution. Avoids a wasted round-trip and lets us
		// reach the cancellation path that also issues StopQueryExecution.
		if err := ctx.Err(); err != nil {
			c.stopQueryOnCancel(queryID, wgName, query, obs, loopStart)
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
			if ctx.Err() != nil {
				c.stopQueryOnCancel(queryID, wgName, query, obs, loopStart)
				return nil, ctx.Err()
			}
			return nil, err
		}
		switch statusResp.QueryExecution.Status.State {
		case athenatypes.QueryExecutionStateCancelled:
			obs.Log(ErrorLevel, "QueryExecutionStateCancelled",
				slog.String("workgroup", wgName),
				slog.String("queryID", queryID))
			obs.Scope().Timer(DriverName + ".query.canceled").Record(time.Since(loopStart))
			if c.connector.config.IsMoneyWise() {
				printCost(c.connector.config.GetRegion(), statusResp)
			}
			return statusResp.QueryExecution, context.Canceled
		case athenatypes.QueryExecutionStateFailed:
			reason := aws.ToString(statusResp.QueryExecution.Status.StateChangeReason)
			if reason == "" {
				reason = "query failed with no reason reported"
			}
			obs.Log(ErrorLevel, "QueryExecutionStateFailed",
				slog.String("workgroup", wgName),
				slog.String("queryID", queryID),
				slog.String("reason", reason))
			obs.Scope().Timer(DriverName + ".query.queryexecutionstatefailed").Record(time.Since(loopStart))
			return statusResp.QueryExecution, errors.New(reason)
		case athenatypes.QueryExecutionStateSucceeded:
			if c.connector.config.IsMoneyWise() {
				printCost(c.connector.config.GetRegion(), statusResp)
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
			if err := c.stopQueryOnCancel(queryID, wgName, query, obs, loopStart); err != nil {
				return nil, err
			}
			return nil, ctx.Err()
		case <-timer.C:
			if isQueryTimeOut(queryStart, statusResp.QueryExecution.StatementType, c.connector.config.GetServiceLimitOverride()) {
				obs.Log(ErrorLevel, "Query timeout failure",
					slog.String("workgroup", wgName),
					slog.String("queryID", queryID),
					slog.String("query", query))
				obs.Scope().Counter(DriverName + ".failure.querycontext.timeout").Inc(1)
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
// cancelled mid-query. It issues StopQueryExecution using a fresh
// (non-cancelled) Background context so the server-side query is actually
// stopped, optionally records the final cost, and updates metrics/logs.
// Returns a non-nil error only if StopQueryExecution itself failed.
func (c *Connection) stopQueryOnCancel(queryID, wgName, query string, obs *DriverTracer, now time.Time) error {
	_, err := c.athenaClient.StopQueryExecution(context.Background(), &athena.StopQueryExecutionInput{
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
	if c.connector.config.IsMoneyWise() {
		statusRespFinal, _ := c.athenaClient.GetQueryExecution(context.Background(), &athena.GetQueryExecutionInput{
			QueryExecutionId: aws.String(queryID),
		})
		printCost(c.connector.config.GetRegion(), statusRespFinal)
	}
	obs.Scope().Counter(DriverName + ".failure.querycontext.stopqueryexecution.succeeded").Inc(1)
	obs.Scope().Timer(DriverName + ".query.StopQueryExecution").Record(time.Since(now))
	obs.Log(ErrorLevel, "query canceled", slog.String("queryID", queryID))
	return nil
}
