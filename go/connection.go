// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
}

// ExecContext executes a query that doesn't return rows, such as an INSERT or UPDATE.
func (c *Connection) ExecContext(ctx context.Context, query string, namedArgs []driver.NamedValue) (driver.Result, error) {
	if c.athenaClient == nil {
		return nil, driver.ErrBadConn
	}
	var obs = c.connector.tracer
	var err error
	args := namedValueToValue(namedArgs)
	if len(namedArgs) > 0 {
		query, err = c.interpolateParams(query, args)
		if err != nil {
			return nil, err
		}
		obs.Scope().Counter(DriverName + ".execcontext").Inc(1)
	}
	if !isQueryValid(query) {
		return nil, ErrInvalidQuery
	}
	rows, err := c.QueryContext(ctx, query, []driver.NamedValue{})
	if err != nil {
		return nil, err
	}
	var rowAffected int64 = 0
	r := rows.(*Rows)
	if r != nil && r.ResultOutput != nil && r.ResultOutput.UpdateCount != nil {
		rowAffected = *r.ResultOutput.UpdateCount
	}
	return AthenaResult{rowAffected: rowAffected}, nil
}

func (c *Connection) cachedQuery(ctx context.Context, QID string) (driver.Rows, error) {
	if c.connector.config.IsMoneyWise() {
		dataScanned := int64(0)
		printCost(c.connector.config.GetRegion(), &athena.GetQueryExecutionOutput{
			QueryExecution: &athenatypes.QueryExecution{
				QueryExecutionId: &QID,
				Statistics: &athenatypes.QueryExecutionStatistics{
					DataScannedInBytes: &dataScanned,
				},
			},
		})
	}
	wg := c.connector.config.GetWorkgroup()
	if wg.Name == "" {
		wg.Name = DefaultWGName
	}
	return NewRows(ctx, c.athenaClient, QID, c.connector.config, c.connector.tracer)
}

func (c *Connection) getHeaderlessSingleRowResultPage(ctx context.Context, qid string) (driver.Rows, error) {
	r, err := NewNonOpsRows(ctx, c.athenaClient, qid, c.connector.config, c.connector.tracer)
	colName := "_col0"
	columnNames := []string{colName}
	columnTypes := []string{"string"}
	data := make([][]*string, 1)
	data[0] = []*string{&qid}
	r.ResultOutput = newHeaderlessResultPage(columnNames, columnTypes, data)
	return r, err
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
	if c.athenaClient == nil {
		return nil, driver.ErrBadConn
	}
	var obs = c.connector.tracer
	pseudoCommand, remaining, immediate, err := c.parsePseudoCommand(ctx, query)
	if err != nil {
		return nil, err
	}
	if immediate != nil {
		return immediate, nil
	}
	query = remaining
	if c.connector.config.IsReadOnly() {
		if !isReadOnlyStatement(query) {
			obs.Scope().Counter(DriverName + ".failure.querycontext.writeviolation").Inc(1)
			obs.Log(WarnLevel, "write db violation", slog.String("query", query))
			return nil, errors.New("writing to Athena database is disallowed in read-only mode")
		}
	}
	now := time.Now()
	args := namedValueToValue(namedArgs)
	queryWithPlaceholders := query // For parameterized queries
	if len(namedArgs) > 0 {
		query, err = c.interpolateParams(query, args)
		if err != nil {
			return nil, err
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
		catalog = c.connector.config.GetCatalog()
	}
	queryExecCtx := &athenatypes.QueryExecutionContext{
		Database: aws.String(c.connector.config.GetDB()),
		Catalog:  aws.String(catalog),
	}
	requestToken := stringFromContext(ctx, ClientRequestTokenKey)
	if requestToken == "" {
		requestToken = newClientRequestToken()
	}
	resultCfg := &athenatypes.ResultConfiguration{
		OutputLocation: aws.String(c.connector.config.GetOutputBucket()),
	}
	if enc := resultEncryptionFromContext(ctx); enc != nil {
		resultCfg.EncryptionConfiguration = enc
	} else if enc := c.connector.config.GetResultEncryption(); enc != nil {
		resultCfg.EncryptionConfiguration = enc
	}
	bucketOwner := stringFromContext(ctx, ExpectedBucketOwnerKey)
	if bucketOwner == "" {
		bucketOwner = c.connector.config.GetExpectedBucketOwner()
	}
	if bucketOwner != "" {
		resultCfg.ExpectedBucketOwner = aws.String(bucketOwner)
	}
	resp, err := c.athenaClient.StartQueryExecution(ctx, &athena.StartQueryExecutionInput{
		QueryString:              aws.String(queryWithPlaceholders),
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
	obs := c.connector.tracer
	wg := c.connector.config.GetWorkgroup()
	if wg.Name == "" {
		wg.Name = DefaultWGName
		return wg, nil
	}
	if wg.Name == DefaultWGName {
		return wg, nil
	}
	athenaWG, err := getWG(ctx, c.athenaClient, wg.Name)
	if err != nil {
		obs.Scope().Counter(DriverName + ".failure.querycontext.getwg").Inc(1)
		obs.Log(WarnLevel, "Didn't find workgroup "+wg.Name+" due to: "+err.Error())
		var re *awshttp.ResponseError
		if errors.As(err, &re) && !strings.Contains(err.Error(), "WorkGroup is not found.") {
			return wg, err
		}
		if !c.connector.config.IsWGRemoteCreationAllowed() {
			obs.Log(WarnLevel, "workgroup "+DefaultWGName+" is used for "+wg.Name+".")
			return wg, fmt.Errorf("workgroup %q doesn't exist and workgroup remote creation is disabled, due to: %v", wg.Name, err.Error())
		}
		if err := wg.CreateWGRemotely(ctx, c.athenaClient); err != nil {
			obs.Scope().Counter(DriverName + ".failure.querycontext.createwgremotely").Inc(1)
			return wg, err
		}
		obs.Log(DebugLevel, "workgroup "+wg.Name+" is created successfully.")
		return wg, nil
	}
	if athenaWG.State != athenatypes.WorkGroupStateEnabled {
		obs.Log(WarnLevel, "workgroup "+DefaultWGName+" is disabled.")
		obs.Scope().Counter(DriverName + ".failure.querycontext.wgdisabled").Inc(1)
		return wg, fmt.Errorf("workgroup %q is disabled", wg.Name)
	}
	obs.Log(DebugLevel, "workgroup "+DefaultWGName+" is enabled.")
	return wg, nil
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
		return driver.ErrBadConn // https://golang.org/pkg/database/sql/driver/#Pinger
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
		numInput:   strings.Count(query, "?"),
	}
	return stmt, nil
}

// Begin is from Conn interface, but no implementation for AWS Athena.
func (c *Connection) Begin() (driver.Tx, error) {
	return nil, ErrAthenaTransactionUnsupported
}

// BeginTx is to replace Begin as it is deprecated.
func (c *Connection) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return nil, ErrAthenaTransactionUnsupported
}

// Close is from Conn interface, but no implementation for AWS Athena.
// Because the sql package maintains a free pool of
// connections and only calls Close when there's a surplus of
// idle connections, it shouldn't be necessary for drivers to
// do their own connection caching.
func (c *Connection) Close() error {
	c.connector = nil
	c.athenaClient = nil
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
	return c.athenaClient != nil
}

var _ driver.QueryerContext = (*Connection)(nil)
var _ driver.ExecerContext = (*Connection)(nil)
var _ driver.SessionResetter = (*Connection)(nil)
var _ driver.Validator = (*Connection)(nil)
