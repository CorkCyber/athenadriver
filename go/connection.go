// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
)

// Connection is a connection to AWS Athena. It is not used concurrently by multiple goroutines.
// Connection is assumed to be stateful.
type Connection struct {
	athenaClient AthenaClient
	connector    *SQLConnector
	// tracer is owned per-connection so concurrent Connect() calls do not
	// race by mutating a shared tracer on the connector.
	tracer *DriverTracer
	// closed marks the conn unusable after Close(). Close leaves
	// athenaClient/connector intact: an in-flight poll dereferences them
	// without synchronization, so nil-ing them is a data race and a panic
	// window. Athena has no server-side session to release anyway.
	closed atomic.Bool
}

// ExecContext executes a query that doesn't return rows, such as an INSERT or UPDATE.
// Delegates to QueryContext so parameterized statements take Athena's
// ExecutionParameters path (no client-side interpolation) and workgroup /
// catalog / poll logic stays in one place. UpdateCount is available on the
// first page of results, so the extra GetQueryResults round-trip that
// NewRows makes is unavoidable for DML.
func (c *Connection) ExecContext(ctx context.Context, query string, namedArgs []driver.NamedValue) (driver.Result, error) {
	if c.closed.Load() || c.athenaClient == nil {
		return nil, driver.ErrBadConn
	}
	if len(namedArgs) > 0 {
		c.tracer.Scope().Counter(DriverName + ".execcontext").Inc(1)
	}
	rows, err := c.QueryContext(ctx, query, namedArgs)
	if err != nil {
		return nil, err
	}
	var rowAffected int64
	if r, ok := rows.(*Rows); ok && r != nil && r.ResultOutput != nil && r.ResultOutput.UpdateCount != nil {
		rowAffected = *r.ResultOutput.UpdateCount
	}
	return AthenaResult{rowAffected: rowAffected}, nil
}

func (c *Connection) cachedQuery(ctx context.Context, QID string) (driver.Rows, error) {
	if c.connector.config.MoneyWise {
		dataScanned := int64(0)
		printCost(c.connector.config.RegionOrEnv(), &athena.GetQueryExecutionOutput{
			QueryExecution: &athenatypes.QueryExecution{
				QueryExecutionId: &QID,
				Statistics: &athenatypes.QueryExecutionStatistics{
					DataScannedInBytes: &dataScanned,
				},
			},
		})
	}
	return NewRows(ctx, c.athenaClient, QID, c.connector.config, c.tracer)
}

func (c *Connection) getHeaderlessSingleRowResultPage(ctx context.Context, qid string) (driver.Rows, error) {
	r := &Rows{
		athena:    c.athenaClient,
		ctx:       ctx,
		queryID:   qid,
		config:    c.connector.config,
		tracer:    c.tracer,
		pageCount: -1,
	}
	r.ResultOutput = newHeaderlessResultPage([]string{"_col0"}, []string{"string"}, [][]*string{{&qid}})
	return r, nil
}

// QueryContext is implemented to be called by `DB.Query` (QueryerContext interface).
//
// "QueryerContext is an optional interface that may be implemented by a Conn.
// If a Conn does not implement QueryerContext, the sql package's DB.Query
// will fall back to Queryer; if the Conn does not implement Queryer either,
// DB.Query will first prepare a query, execute the statement, and then
// close the statement."
//
// With QueryContext implemented, we don't need Queryer.
// QueryerContext must honor the context timeout and return when the context is canceled.
func (c *Connection) QueryContext(ctx context.Context, query string, namedArgs []driver.NamedValue) (driver.Rows, error) {
	if c.closed.Load() || c.athenaClient == nil {
		return nil, driver.ErrBadConn
	}
	var obs = c.tracer
	pseudoCommand, remaining, immediate, err := c.parsePseudoCommand(ctx, query)
	if err != nil {
		return nil, err
	}
	if immediate != nil {
		return immediate, nil
	}
	query = remaining
	if c.connector.config.ReadOnly {
		if !isReadOnlyStatement(query) {
			obs.Scope().Counter(DriverName + ".failure.querycontext.writeviolation").Inc(1)
			obs.Log(WarnLevel, "write db violation", slog.String("query", query))
			return nil, errors.New("writing to Athena database is disallowed in read-only mode")
		}
	}
	now := time.Now()
	args := namedValueToValue(namedArgs)
	// Athena receives the placeholder-form query plus ExecutionParameters —
	// but only for statement shapes the API supports (SELECT / INSERT /
	// CTAS / UNLOAD / WITH). Anything else (ALTER, MSCK, DROP, plain
	// CREATE, ...) is rejected server-side with ExecutionParameters set,
	// so those are interpolated client-side and submitted parameterless.
	// Length validation gates on whichever form actually goes to Athena
	// (what counts against the 262 KiB query cap).
	if len(namedArgs) > 0 {
		// Only interpolate when the interpolated form is what goes to
		// Athena. On the ExecutionParameters path Athena validates the
		// placeholder count server-side; running interpolateParams there
		// would both waste an allocation and reject valid queries, since
		// its `?`-count check also counts `?` inside string literals.
		if !executionParamsSupported(query) {
			interpolated, ierr := c.interpolateParams(query, args)
			if ierr != nil {
				return nil, ierr
			}
			query = interpolated
			args = nil
		}
		obs.Scope().Counter(DriverName + ".prepared.querycontext").Inc(1)
	}
	if !isQueryValid(query) {
		return nil, ErrInvalidQuery
	}
	wg, err := c.resolveWorkgroup(ctx)
	if err != nil {
		return nil, err
	}

	timeWorkgroup := time.Since(now)
	startOfStartQueryExecution := time.Now()
	obs.Scope().Timer(DriverName + ".query.workgroup").Record(timeWorkgroup)

	// case 1 - query directly using QID
	if IsQID(query) {
		if pseudoCommand == PCGetQIDStatus {
			statusResp, err := c.athenaClient.GetQueryExecution(ctx, &athena.GetQueryExecutionInput{
				QueryExecutionId: aws.String(query),
			})
			if err != nil {
				obs.Log(ErrorLevel, "GetQueryExecutionWithContext failed",
					slog.String("workgroup", wg.Name),
					slog.String("queryID", query),
					slog.String("error", err.Error()))
				obs.Scope().Counter(DriverName + ".failure.querycontext.getqueryexecutionwithcontext").Inc(1)
				return nil, err
			}
			if statusResp == nil || statusResp.QueryExecution == nil ||
				statusResp.QueryExecution.Status == nil {
				obs.Scope().Counter(DriverName + ".failure.querycontext.getqueryexecutionwithcontext").Inc(1)
				return nil, fmt.Errorf("GetQueryExecution returned no status for query ID %q", query)
			}
			return c.getHeaderlessSingleRowResultPage(ctx, string(statusResp.QueryExecution.Status.State))
		}
		if pseudoCommand == PCStopQID {
			_, err := c.athenaClient.StopQueryExecution(ctx, &athena.StopQueryExecutionInput{
				QueryExecutionId: aws.String(query),
			})
			if err != nil {
				obs.Log(ErrorLevel, "StopQueryExecution failed",
					slog.String("workgroup", wg.Name),
					slog.String("queryID", query),
					slog.String("query", query))
				obs.Scope().Counter(DriverName + ".failure.querycontext.stopqueryexecution.failed").Inc(1)
				return nil, err
			}
			return c.getHeaderlessSingleRowResultPage(ctx, "OK")
		}
		return c.cachedQuery(ctx, query)
	}

	executionParams, err := c.buildExecutionParams(args)
	if err != nil {
		return nil, err
	}
	catalog := stringFromContext(ctx, CatalogKey)
	if catalog == "" {
		catalog = c.connector.config.CatalogOrDefault()
	}
	queryExecCtx := &athenatypes.QueryExecutionContext{
		Database: aws.String(c.connector.config.DB),
		Catalog:  aws.String(catalog),
	}
	requestToken := stringFromContext(ctx, ClientRequestTokenKey)
	if requestToken == "" {
		requestToken = newClientRequestToken()
	}
	resultCfg := &athenatypes.ResultConfiguration{
		OutputLocation: aws.String(c.connector.config.OutputBucket()),
	}
	if enc := resultEncryptionFromContext(ctx); enc != nil {
		resultCfg.EncryptionConfiguration = enc
	} else if enc := c.connector.config.ResultEncryption; enc != nil {
		resultCfg.EncryptionConfiguration = enc
	}
	bucketOwner := stringFromContext(ctx, ExpectedBucketOwnerKey)
	if bucketOwner == "" {
		bucketOwner = c.connector.config.ExpectedBucketOwner
	}
	if bucketOwner != "" {
		resultCfg.ExpectedBucketOwner = aws.String(bucketOwner)
	}
	resp, err := c.athenaClient.StartQueryExecution(ctx, &athena.StartQueryExecutionInput{
		QueryString:              aws.String(query),
		ExecutionParameters:      executionParams,
		ClientRequestToken:       aws.String(requestToken),
		QueryExecutionContext:    queryExecCtx,
		ResultConfiguration:      resultCfg,
		ResultReuseConfiguration: resultReuseFromContext(ctx),
		WorkGroup:                aws.String(wg.Name),
	})
	if err != nil {
		if pseudoCommand == PCGetQID {
			var re *awshttp.ResponseError
			if errors.As(err, &re) {
				return c.getHeaderlessSingleRowResultPage(ctx, re.ServiceRequestID())
			}
		}
		return nil, err
	}
	// QueryExecutionId is *string because it can be absent: a 200 with an
	// unexpectedly empty body (proxy, LocalStack, a custom AthenaClient).
	// Dereferencing it unguarded turns that into a panic inside the caller's
	// query path.
	if resp == nil || resp.QueryExecutionId == nil {
		obs.Scope().Counter(DriverName + ".failure.querycontext.startqueryexecution.noqid").Inc(1)
		return nil, errors.New("StartQueryExecution returned no query execution ID")
	}

	timeStartQueryExecution := time.Since(startOfStartQueryExecution)
	now = time.Now()
	obs.Scope().Timer(DriverName + ".query.startqueryexecution").Record(timeStartQueryExecution)

	queryID := *resp.QueryExecutionId
	if pseudoCommand == PCGetQID {
		return c.getHeaderlessSingleRowResultPage(ctx, queryID)
	}
	finalExec, err := c.awaitQueryCompletion(ctx, queryID, wg.Name, query, startOfStartQueryExecution, now)
	if err != nil {
		return nil, err
	}
	rows, err := NewRows(ctx, c.athenaClient, queryID, c.connector.config, obs)
	if err != nil {
		return nil, err
	}
	rows.queryExecution = finalExec
	return rows, nil
}

// parsePseudoCommand strips the optional `pc:<cmd> ` prefix from a query
// and reports the pseudo-command name plus the remainder of the query
// string. Returns immediate != nil for pseudo-commands whose entire effect
// is to produce a one-shot result row (currently only PCGetDriverVersion).
// Returns (`"`, query, nil, nil) when the query has no `pc:` prefix.
func (c *Connection) parsePseudoCommand(ctx context.Context, query string) (cmd, remaining string, immediate driver.Rows, err error) {
	if !strings.HasPrefix(query, "pc:") {
		return "", query, nil, nil
	}
	body := strings.Trim(query[3:], " ")
	for _, pc := range []string{PCGetQID, PCGetQIDStatus, PCStopQID} {
		if strings.HasPrefix(body, pc+" ") {
			return pc, strings.Trim(body[len(pc):], " "), nil, nil
		}
	}
	if strings.HasPrefix(body, PCGetDriverVersion) {
		rows, err := c.getHeaderlessSingleRowResultPage(ctx, DriverVersion())
		return PCGetDriverVersion, "", rows, err
	}
	return "", body, nil, fmt.Errorf("pseudo command %q doesn't exist", body)
}

// resolveWorkgroup returns the workgroup the next query should run in,
// remote-creating it via Athena if the configured workgroup does not exist
// and the Config opts into remote creation. The default workgroup
// (DefaultWGName) is assumed to exist and is not validated.
func (c *Connection) resolveWorkgroup(ctx context.Context) (Workgroup, error) {
	obs := c.tracer
	var wg Workgroup
	if c.connector.config.WorkGroup != nil {
		wg = *c.connector.config.WorkGroup
	}
	if wg.Name == "" {
		wg.Name = DefaultWGName
		return wg, nil
	}
	if wg.Name == DefaultWGName {
		return wg, nil
	}
	// The workgroup identity is fixed at connector construction, so the
	// existence/enabled check is worth exactly one GetWorkGroup call per
	// connector rather than one per query. Only success is cached.
	if c.connector.wgVerified.Load() {
		return wg, nil
	}
	// Single-flight the verification, mirroring sharedClient's
	// double-checked lock: without it a cold-start burst fires one
	// GetWorkGroup (and CreateWorkGroup) per pooled connection and trips
	// Athena's API throttle, surfacing as failed user queries.
	c.connector.wgMu.Lock()
	defer c.connector.wgMu.Unlock()
	if c.connector.wgVerified.Load() {
		return wg, nil
	}
	athenaWG, err := getWG(ctx, c.athenaClient, wg.Name)
	if err != nil {
		obs.Scope().Counter(DriverName + ".failure.querycontext.getwg").Inc(1)
		obs.Log(WarnLevel, "Didn't find workgroup "+wg.Name+" due to: "+err.Error())
		if !isWGNotFound(err) {
			// Anything that is not Athena telling us the workgroup does
			// not exist (network blip, credentials, throttling) must be
			// surfaced, never treated as "absent, go create it".
			return wg, err
		}
		if !c.connector.config.WGRemoteCreation {
			obs.Log(WarnLevel, "workgroup "+DefaultWGName+" is used for "+wg.Name+".")
			return wg, fmt.Errorf("workgroup %q doesn't exist and workgroup remote creation is disabled: %w", wg.Name, err)
		}
		if cerr := wg.CreateWGRemotely(ctx, c.athenaClient); cerr != nil {
			// Concurrent cold start: several connections can observe the
			// same miss and race to create. Losing that race is success,
			// so re-check before reporting a failure.
			if _, gerr := getWG(ctx, c.athenaClient, wg.Name); gerr != nil {
				obs.Scope().Counter(DriverName + ".failure.querycontext.createwgremotely").Inc(1)
				return wg, cerr
			}
			obs.Log(DebugLevel, "workgroup "+wg.Name+" was created concurrently.")
		} else {
			obs.Log(DebugLevel, "workgroup "+wg.Name+" is created successfully.")
		}
		c.connector.wgVerified.Store(true)
		return wg, nil
	}
	if athenaWG.State != athenatypes.WorkGroupStateEnabled {
		obs.Log(WarnLevel, "workgroup "+wg.Name+" is disabled.")
		obs.Scope().Counter(DriverName + ".failure.querycontext.wgdisabled").Inc(1)
		return wg, fmt.Errorf("workgroup %q is disabled", wg.Name)
	}
	obs.Log(DebugLevel, "workgroup "+wg.Name+" is enabled.")
	c.connector.wgVerified.Store(true)
	return wg, nil
}

// isWGNotFound reports whether err is Athena's modeled "this workgroup
// does not exist" response, as opposed to any other failure.
func isWGNotFound(err error) bool {
	var ire *athenatypes.InvalidRequestException
	return errors.As(err, &ire) && strings.Contains(aws.ToString(ire.Message), "is not found")
}

// Ping implements driver.Pinger interface.
// Ping is a good first step in a health check: If the Ping succeeds,
// make a simple query, then make a complex query which depends on proper
// DB scheme. This will make troubleshooting simpler as the error now is:
// "We've got network connectivity, we can Ping the DB, so we have valid
// credentials for a SELECT xxx; but ...".
func (c *Connection) Ping(ctx context.Context) error {
	rows, err := c.QueryContext(ctx, "SELECT 1", nil)
	if err != nil {
		// database/sql retries an ErrBadConn Ping on two more connections
		// and discards the real error, so reserve it for a conn that is
		// genuinely unusable — QueryContext's entry guard already returns
		// ErrBadConn for that, and it passes through here untouched. A
		// cancelled/expired ctx reports itself; everything else (disabled
		// workgroup, bad credentials, syntax) is surfaced verbatim so one
		// health check costs one Athena query.
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
		return err
	}
	defer rows.Close()
	return nil
}

// Prepare is inherited from Conn interface.
func (c *Connection) Prepare(query string) (driver.Stmt, error) {
	if !isQueryValid(query) {
		return nil, ErrInvalidQuery
	}
	stmt := &Statement{
		connection: c,
		query:      query,
		closed:     false,
	}
	return stmt, nil
}

// Begin is from Conn interface, but no implementation for AWS Athena.
func (c *Connection) Begin() (driver.Tx, error) {
	return nil, ErrAthenaTransactionUnsupported
}

// Close is from Conn interface, but no implementation for AWS Athena.
// Because the sql package maintains a free pool of
// connections and only calls Close when there's a surplus of
// idle connections, it shouldn't be necessary for drivers to
// do their own connection caching.
func (c *Connection) Close() error {
	c.closed.Store(true)
	return nil
}

// ResetSession implements driver.SessionResetter. database/sql calls this
// before reusing a pooled connection. Athena has no persistent server-side
// connection state (each StartQueryExecution is independent), so the only
// meaningful response is to honor a cancelled ctx and return ErrBadConn so
// the pool discards the conn.
func (c *Connection) ResetSession(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return driver.ErrBadConn
	}
	return nil
}

// IsValid implements driver.Validator on the Conn (separate from
// SQLDriver.IsValid). Returning false here tells database/sql to discard
// the connection rather than return it to the pool. We treat a Connection
// whose Close() has already run as invalid.
func (c *Connection) IsValid() bool {
	return !c.closed.Load() && c.athenaClient != nil
}

var _ driver.QueryerContext = (*Connection)(nil)
var _ driver.ExecerContext = (*Connection)(nil)
var _ driver.SessionResetter = (*Connection)(nil)
var _ driver.Validator = (*Connection)(nil)
