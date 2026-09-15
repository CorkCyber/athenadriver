// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
)

var regions = []string{"ap-east-1", "eu-central-1", "eu-north-1", "eu-west-1", "eu-west-2", "eu-west-3",
	"me-south-1", "us-east-1", "us-west-1", "ap-northeast-1", "ap-northeast-2", "ap-southeast-1",
	"ca-central-1", "us-east-2", "ap-south-1", "ap-southeast-2", "us-west-2",
}

// newTestConnWithWG builds a *Connection wired to a fresh mock client
// and a Config preset with the "henry_wu" workgroup, us-east-1 region,
// and a fake S3 bucket. Optional mutators tweak the config or mock for
// each test.
func newTestConnWithWG(mut ...func(*Config, *mockAthenaClient)) *Connection {
	nm := newMockAthenaClient()
	cfg := NewNoOpsConfig()
	_ = cfg.SetOutputBucket("s3://fake-query-results-arbitrary-bucket/")
	_ = cfg.SetRegion("us-east-1")
	cfg.User = "henry.wu"
	cfg.DB = "default"

	wgTags := NewWGTags()
	wgTags.AddTag("Uber User", "henry.wu")
	wgTags.AddTag("Uber Asset", "abc.efg")
	_ = cfg.SetWorkGroup(NewWG("henry_wu", nil, wgTags))

	for _, m := range mut {
		m(cfg, nm)
	}

	c := &Connection{
		athenaClient: nm,
		connector:    NoopsSQLConnector(),
		tracer:       NewObservability(NewNoOpsConfig(), nil, nil),
	}
	c.connector.config = cfg
	return c
}

func TestReadOnly_WriteRejection(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.ReadOnly = true
	db, _ := sql.Open(DriverName, testConf.Stringify())
	const wantErr = "writing to Athena database is disallowed in read-only mode"

	cases := []struct {
		name, query string
	}{
		{"CTAS uppercase", "CREATE TABLE sampledb.elb_logs_new AS SELECT * FROM sampledb.elb_logs limit 10;"},
		{"CTAS leading space + upper", " CREATE TABLE sampledb.elb_logs_new AS SELECT * FROM sampledb.elb_logs limit 10;"},
		{"CTAS mixed case", " cReate TABLE sampledb.elb_logs_new AS SELECT * FROM sampledb.elb_logs limit 10;"},
		{"DROP", " drop table test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.QueryContext(context.Background(), tc.query)
			assert.EqualError(t, err, wantErr)
		})
	}

	// Pathological / invalid queries are also rejected by ExecContext +
	// Ping under read-only mode.
	t.Run("oversize query with args", func(t *testing.T) {
		query := randString(MAXQueryStringLength*10) + "?"
		args := []driver.Value{query}
		r, err := db.ExecContext(context.Background(), query, args)
		assert.NotNil(t, err)
		assert.Nil(t, r)

		r, err = db.ExecContext(context.Background(), query, "")
		assert.NotNil(t, err)
		assert.Nil(t, r)

		r, err = db.ExecContext(context.Background(), query)
		assert.NotNil(t, err)
		assert.Nil(t, r)
	})

	t.Run("standalone placeholder", func(t *testing.T) {
		r, err := db.ExecContext(context.Background(), "?", "")
		assert.NotNil(t, err)
		assert.Nil(t, r)
	})

	t.Run("ping under read-only", func(t *testing.T) {
		assert.NotNil(t, db.Ping())
	})
}

func TestConnection_Prepare(t *testing.T) {
	testConf := NewNoOpsConfig()
	connector := &SQLConnector{
		config: testConf,
	}

	conn, err := connector.Connect(context.Background())
	assert.Nil(t, err)
	assert.NotNil(t, conn)
	prepStatement, err := conn.Prepare("select 123")
	assert.NotNil(t, prepStatement)
	assert.Nil(t, err)

	query := randString(MAXQueryStringLength * 10)
	prepStatement, err = conn.Prepare(query)
	assert.NotNil(t, err)
	assert.Nil(t, prepStatement)
}

func TestConnection_Close(t *testing.T) {
	testConf := NewNoOpsConfig()
	connector := &SQLConnector{
		config: testConf,
	}

	conn, err := connector.Connect(context.Background())
	assert.Nil(t, err)
	assert.NotNil(t, conn)
	assert.Nil(t, conn.Close())

}

// TestConnection_CloseRace guards the Close()/in-flight-query data race:
// Close must only flip an atomic flag, never nil out fields a concurrent
// poll dereferences. Run under -race; a closed conn must report
// driver.ErrBadConn, not panic.
func TestConnection_CloseRace(t *testing.T) {
	c := newTestConnWithWG()
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Go(func() {
		for range 200 {
			// Errors are the mock's business; what matters here is that a
			// concurrent Close neither panics nor races. ErrBadConn after
			// Close is asserted deterministically below.
			rows, err := c.QueryContext(ctx, "select 1", nil)
			if err == nil {
				rows.Close()
			}
		}
	})
	time.Sleep(time.Millisecond)
	assert.NoError(t, c.Close())
	wg.Wait()

	assert.False(t, c.IsValid())
	_, err := c.QueryContext(ctx, "select 1", nil)
	assert.ErrorIs(t, err, driver.ErrBadConn)
	_, err = c.ExecContext(ctx, "select 1", nil)
	assert.ErrorIs(t, err, driver.ErrBadConn)
}

// TestQueryContext_WhitespaceFreeSQLIsNotMisroutedAsQID pins the fix for a
// real Trino ambiguity: "SELECT(1)" has no whitespace at all (valid syntax -
// SQL tokenizes on punctuation, not just spaces), so it also satisfies
// IsQID's \S{1,128} contract. Without the looksLikeSQL guard this query
// never reaches StartQueryExecution at all: it's misrouted straight to a
// (bogus) cached-QID lookup instead of being executed.
func TestQueryContext_WhitespaceFreeSQLIsNotMisroutedAsQID(t *testing.T) {
	t.Parallel()
	c := newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
		cfg.WorkGroup = nil
	})
	nm := c.athenaClient.(*mockAthenaClient)

	_, err := c.QueryContext(context.Background(), "SELECT(1)", nil)
	assert.Nil(t, err)
	assert.NotNil(t, nm.lastStartInput, "StartQueryExecution must be called; the query was misrouted as a QID lookup instead")
	assert.Equal(t, "SELECT(1)", *nm.lastStartInput.QueryString)
}

// TestQueryContext_LiteralQuestionMark is the regression guard for
// running interpolateParams (and its naive `?`-counting check) on the
// ExecutionParameters path: a `?` inside a string literal is not a
// placeholder, and Athena — not the driver — validates placeholder count.
func TestQueryContext_LiteralQuestionMark(t *testing.T) {
	t.Parallel()
	c := newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
		cfg.WorkGroup = nil // skip remote workgroup resolution
	})
	nm := c.athenaClient.(*mockAthenaClient)

	_, err := c.QueryContext(context.Background(), "select ? , 'what?'",
		[]driver.NamedValue{{Value: int64(1)}})
	assert.Nil(t, err)
	assert.Equal(t, "select ? , 'what?'", *nm.lastStartInput.QueryString)
	assert.Equal(t, []string{"1"}, nm.lastStartInput.ExecutionParameters)
}

// TestQueryContext_StringArgIsQuoted is the SQL-injection regression guard.
// Athena evaluates every ExecutionParameters entry as a SQL EXPRESSION, so an
// unquoted string argument is executable SQL. A hostile value must reach
// Athena as an inert, quote-escaped string literal.
func TestQueryContext_StringArgIsQuoted(t *testing.T) {
	t.Parallel()
	c := newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
		cfg.WorkGroup = nil // skip remote workgroup resolution
	})
	nm := c.athenaClient.(*mockAthenaClient)

	_, err := c.QueryContext(context.Background(), "select ?",
		[]driver.NamedValue{{Value: "1 OR 1=1 --"}})
	assert.Nil(t, err)
	assert.Equal(t, "select ?", *nm.lastStartInput.QueryString)
	assert.Equal(t, []string{"'1 OR 1=1 --'"}, nm.lastStartInput.ExecutionParameters)

	// Embedded quotes are doubled, so the payload cannot close the literal.
	_, err = c.QueryContext(context.Background(), "select ?",
		[]driver.NamedValue{{Value: "x' OR '1'='1' --"}})
	assert.Nil(t, err)
	assert.Equal(t, []string{"'x'' OR ''1''=''1'' --'"}, nm.lastStartInput.ExecutionParameters)

	// []byte takes the same path.
	_, err = c.QueryContext(context.Background(), "select ?",
		[]driver.NamedValue{{Value: []byte("a' OR 1=1")}})
	assert.Nil(t, err)
	assert.Equal(t, []string{"'a'' OR 1=1'"}, nm.lastStartInput.ExecutionParameters)

	// Raw is the explicit, opt-in escape hatch for expression arguments.
	_, err = c.QueryContext(context.Background(), "select ?",
		[]driver.NamedValue{{Value: Raw("TIMESTAMP '2024-07-01 00:00:00'")}})
	assert.Nil(t, err)
	assert.Equal(t, []string{"TIMESTAMP '2024-07-01 00:00:00'"}, nm.lastStartInput.ExecutionParameters)
}

// TestQueryContext_TracesAsDatabaseSpan proves a successful query is
// wrapped in exactly one span carrying the OpenTelemetry database-client
// attributes a backend needs to recognize it as a DB call, plus the
// athena.* extension attributes, and that the span is always ended.
func TestQueryContext_TracesAsDatabaseSpan(t *testing.T) {
	t.Parallel()
	c := newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
		cfg.WorkGroup = nil // skip remote workgroup resolution
	})
	tracer := &recordingTracer{}
	c.tracer.SetTracer(tracer)

	_, err := c.QueryContext(context.Background(), "select 1", nil)
	assert.Nil(t, err)

	assert.Equal(t, "SELECT default", tracer.startedName)
	assert.Equal(t, "aws.athena", tracer.span.attrs[otelDBSystemName])
	assert.Equal(t, "default", tracer.span.attrs[otelDBNamespace])
	assert.Equal(t, "SELECT", tracer.span.attrs[otelDBOperationName])
	assert.Equal(t, "PING_OK_QID", tracer.span.attrs[athenaAttrQueryID])
	assert.Contains(t, tracer.span.attrs, athenaAttrStmtType)
	assert.Empty(t, tracer.span.errs)
	assert.True(t, tracer.span.ended)
}

// TestDBSpanNameAndOperation pins the low-cardinality contract for QID-shaped
// input: a bare query ID or a QID-targeting pseudo-command must never put
// the ID itself into the span name or db.operation.name.
func TestDBSpanNameAndOperation(t *testing.T) {
	cases := []struct {
		name          string
		pseudoCommand string
		query         string
		wantOperation string
	}{
		{"plain select", "", "select 1", "SELECT"},
		{"bare query ID", "", "a44f8e61-4cbb-429a-b7ab-bea2c4a5caed", "GET_QUERY_ID"},
		{"get_query_id_status", PCGetQIDStatus, "a44f8e61-4cbb-429a-b7ab-bea2c4a5caed", "GET_QUERY_ID_STATUS"},
		{"stop_query_id", PCStopQID, "a44f8e61-4cbb-429a-b7ab-bea2c4a5caed", "STOP_QUERY_ID"},
		{"get_query_id submits real SQL", PCGetQID, "select 1", "SELECT"},
		{"get_query_id misused with a QID", PCGetQID, "a44f8e61-4cbb-429a-b7ab-bea2c4a5caed", "GET_QUERY_ID"},
		{"empty query", "", "", ""},
	}
	const uuid = "a44f8e61-4cbb-429a-b7ab-bea2c4a5caed"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spanName, op := dbSpanNameAndOperation(tc.pseudoCommand, tc.query, "mydb")
			assert.Equal(t, tc.wantOperation, op)
			assert.NotContains(t, strings.ToLower(spanName), uuid,
				"a UUID must never appear in the span name")
			assert.NotContains(t, strings.ToLower(op), uuid,
				"a UUID must never appear in db.operation.name")
		})
	}
}

// TestQueryContext_TracesErrorsOnSpan proves a failed query records the
// error on its span (RecordError + error.type) instead of the span
// silently ending as if the call had succeeded.
func TestQueryContext_TracesErrorsOnSpan(t *testing.T) {
	t.Parallel()
	c := newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
		cfg.WorkGroup = nil
		cfg.ReadOnly = true
	})
	tracer := &recordingTracer{}
	c.tracer.SetTracer(tracer)

	_, err := c.QueryContext(context.Background(), "drop table t", nil)
	assert.Error(t, err)

	assert.Len(t, tracer.span.errs, 1)
	assert.Equal(t, err, tracer.span.errs[0])
	assert.Equal(t, dbErrorType(err), tracer.span.attrs[otelErrorType])
	assert.True(t, tracer.span.ended)
}

// TestQueryContext_ParseErrorGetsASpan proves a query that fails before
// dbSpanNameAndOperation can even run (bad pseudo-command syntax) still
// records a span, instead of returning span-less and invisible to tracing.
func TestQueryContext_ParseErrorGetsASpan(t *testing.T) {
	t.Parallel()
	c := newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
		cfg.WorkGroup = nil
	})
	tracer := &recordingTracer{}
	c.tracer.SetTracer(tracer)

	_, err := c.QueryContext(context.Background(), "pc:bogus_command", nil)
	assert.Error(t, err)

	assert.Equal(t, "athena.query", tracer.startedName)
	assert.Equal(t, "aws.athena", tracer.span.attrs[otelDBSystemName])
	assert.Len(t, tracer.span.errs, 1)
	assert.Equal(t, dbErrorType(err), tracer.span.attrs[otelErrorType])
	assert.True(t, tracer.span.ended)
}

func TestDbErrorType(t *testing.T) {
	assert.Equal(t, "*errors.errorString", dbErrorType(context.Canceled))
	assert.Equal(t, "*fmt.wrapError", dbErrorType(fmt.Errorf("wrapped: %w", context.Canceled)))

	apiErr := &smithy.GenericAPIError{Code: "ThrottlingException"}
	assert.Equal(t, "ThrottlingException", dbErrorType(apiErr))
	assert.Equal(t, "ThrottlingException", dbErrorType(fmt.Errorf("wrapped: %w", apiErr)))
}

// countingTracer/countingSpan are a concurrency-safe recording double: real
// pooled usage shares ONE Tracer instance (set once via WithTracer) across
// every connection database/sql opens, so a chaos test simulating that pool
// needs a double that itself tolerates concurrent StartSpan calls — unlike
// recordingTracer, which stores one shared, unsynchronized *recordingSpan.
type countingTracer struct {
	started atomic.Int64
	ended   atomic.Int64
}

func (t *countingTracer) StartSpan(ctx context.Context, _ string) (context.Context, Span) {
	t.started.Add(1)
	return ctx, &countingSpan{ended: &t.ended}
}

type countingSpan struct {
	ended *atomic.Int64
}

func (s *countingSpan) SetAttr(string, any) {}
func (s *countingSpan) RecordError(error)   {}
func (s *countingSpan) End()                { s.ended.Add(1) }

// TestQueryContext_ConcurrentPooledQueries_RaceFree chaos-tests the pooled
// scenario every prior finding in this area was about: many connections
// (as database/sql's pool would open under load), all sharing the SAME
// Tracer instance via WithTracer/SetTracer, hit with a concurrent mix of
// successful queries, query failures, and pseudo-command parse failures.
// Run with -race, this is the completeness check for every span-lifecycle
// fix in this file: no data race, and every started span is ended exactly
// once — including on every error path.
func TestQueryContext_ConcurrentPooledQueries_RaceFree(t *testing.T) {
	shared := &countingTracer{}
	const goroutines = 50

	queries := []string{
		"select 1",         // success
		"drop table t",     // Athena-side failure (read-only guard)
		"pc:bogus_command", // pseudo-command parse failure
	}

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Go(func() {
			c := newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
				cfg.WorkGroup = nil
				cfg.ReadOnly = true
			})
			c.tracer.SetTracer(shared)
			q := queries[i%len(queries)]
			_, _ = c.QueryContext(context.Background(), q, nil)
		})
	}
	wg.Wait()

	assert.Equal(t, int64(goroutines), shared.started.Load())
	assert.Equal(t, int64(goroutines), shared.ended.Load())
}

// CheckNamedValue must keep Raw intact; database/sql's default converter
// would otherwise flatten it to a plain string and the value would be quoted.
func TestConnection_CheckNamedValue(t *testing.T) {
	t.Parallel()
	c := &Connection{}
	nv := driver.NamedValue{Value: Raw("CAST(1 AS BIGINT)")}
	assert.Nil(t, c.CheckNamedValue(&nv))
	assert.Equal(t, Raw("CAST(1 AS BIGINT)"), nv.Value)

	nv = driver.NamedValue{Value: "plain"}
	assert.Equal(t, driver.ErrSkip, c.CheckNamedValue(&nv))
}

func TestConnection_Begin(t *testing.T) {
	c := createTestConnection(t)

	tx, err := c.Begin()
	assert.Nil(t, tx)
	assert.EqualError(t, err, "Athena doesn't support transaction statements")
}

func TestConnection_Transaction(t *testing.T) {
	db, _ := sql.Open(DriverName, NewNoOpsConfig().Stringify())
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
	assert.Nil(t, tx)
	assert.Equal(t, "sql: driver does not support non-default isolation level", err.Error())
}

func TestConnection_InterpolateParams(t *testing.T) {
	c := createTestConnection(t)
	q, err := c.interpolateParams("SELECT ?+?", []driver.Value{int64(42), "gopher"})
	if err != nil {
		t.Errorf("Expected err=nil, got %#v", err)
		return
	}
	expected := `SELECT 42+'gopher'`
	if q != expected {
		t.Errorf("Expected: %q\nGot: %q", expected, q)
	}
}

func TestInterpolateParamsTooManyPlaceholders(t *testing.T) {
	c := createTestConnection(t)
	q, err := c.interpolateParams("SELECT ?+?", []driver.Value{int64(42)})
	if err != ErrInvalidQuery {
		t.Errorf("Expected err=ErrInvalidQuery, got err=%#v, q=%#v", err, q)
	}
}

func TestConnection_InterpolateParams_Query(t *testing.T) {
	c := createTestConnection(t)
	query := randString(MAXQueryStringLength*10) + "?"
	q, err := c.interpolateParams(query, []driver.Value{query})
	assert.Equal(t, "", q)
	assert.NotNil(t, err)
}

func TestConnection_InterpolateParams_Query2(t *testing.T) {
	c := createTestConnection(t)
	q, err := c.interpolateParams("?", []driver.Value{aType{S: "abc"}})
	assert.Equal(t, "", q)
	assert.NotNil(t, err)

	q, err = c.interpolateParams("?", []driver.Value{1})
	assert.Equal(t, "", q)
	assert.NotNil(t, err)
}

func TestConnection_InterpolateParams_Bool(t *testing.T) {
	c := createTestConnection(t)
	q, err := c.interpolateParams("?", []driver.Value{true})
	assert.Equal(t, "true", q)
	assert.Nil(t, err)
	q, err = c.interpolateParams("?", []driver.Value{false})
	assert.Equal(t, "false", q)
	assert.Nil(t, err)
	q, err = c.interpolateParams("?", []driver.Value{int64(1)})
	assert.Equal(t, "1", q)
	assert.Nil(t, err)
	q, err = c.interpolateParams("?", []driver.Value{uint64(1)})
	assert.Equal(t, "1", q)
	assert.Nil(t, err)
	q, err = c.interpolateParams("?", []driver.Value{float64(1.1)})
	assert.Equal(t, "1.1", q)
	assert.Nil(t, err)
	// NaN/Inf render as the bare words NaN/+Inf/-Inf, not valid Trino
	// numeric-literal syntax: must error client-side, not send malformed SQL.
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err = c.interpolateParams("?", []driver.Value{f})
		assert.Error(t, err, "want error for non-finite float %v", f)
	}
	q, err = c.interpolateParams("?", []driver.Value{time.Time{}})
	assert.Equal(t, "NULL", q)
	assert.Nil(t, err)
	q, err = c.interpolateParams("?", []driver.Value{time.Now()})
	assert.NotEqual(t, q, "'0000-00-00'")
	assert.Nil(t, err)
	q, err = c.interpolateParams("?", []driver.Value{[]byte{'0'}})
	assert.Equal(t, "'0'", q)
	assert.Nil(t, err)
	q, err = c.interpolateParams("?", []driver.Value{nil})
	assert.Equal(t, "NULL", q)
	assert.Nil(t, err)
	q, err = c.interpolateParams("123?4", []driver.Value{nil})
	assert.Equal(t, "123NULL4", q)
	assert.Nil(t, err)
	q, err = c.interpolateParams("?", []driver.Value{time.Time{}.Add(1 * time.Nanosecond)})
	assert.Equal(t, "'0001-01-01 00:00:00'", q)
	assert.Nil(t, err)
}

// A `?` inside a string literal is data, not a placeholder: substituting it
// would splice the argument's quotes into the middle of the literal and flip
// quoting parity for the rest of the statement.
func TestInterpolateParamsPlaceholderInString(t *testing.T) {
	c := createTestConnection(t)

	q, err := c.interpolateParams("SELECT 'abc?xyz',?", []driver.Value{int64(42)})
	assert.Nil(t, err)
	assert.Equal(t, "SELECT 'abc?xyz',42", q)

	// Doubled quotes inside a literal do not end the literal.
	q, err = c.interpolateParams("SELECT 'it''s ?',?", []driver.Value{int64(1)})
	assert.Nil(t, err)
	assert.Equal(t, "SELECT 'it''s ?',1", q)

	// An argument cannot escape its own literal: its quotes are doubled, so
	// the trailing `--` stays inside the string value.
	q, err = c.interpolateParams("SELECT * FROM t WHERE name = ? AND note = 'why?'",
		[]driver.Value{"x' OR '1'='1' --"})
	assert.Nil(t, err)
	assert.Equal(t, "SELECT * FROM t WHERE name = 'x'' OR ''1''=''1'' --' AND note = 'why?'", q)

	// The count check is quote-aware too: still one real placeholder here.
	_, err = c.interpolateParams("SELECT 'abc?xyz',?", []driver.Value{int64(1), int64(2)})
	assert.Equal(t, ErrInvalidQuery, err)
}

func TestInterpolateParamsUint64(t *testing.T) {
	c := createTestConnection(t)

	q, err := c.interpolateParams("SELECT ?", []driver.Value{uint64(42)})
	assert.Nil(t, err)
	assert.Equal(t, "SELECT 42", q)
}

func TestInterpolateParamsDateOnly(t *testing.T) {
	testTime, err := time.Parse(time.DateOnly, "2024-07-01")
	assert.Nil(t, err)

	c := createTestConnection(t)
	q, err := c.interpolateParams("SELECT ?", []driver.Value{testTime})
	assert.Nil(t, err)
	assert.Equal(t, "SELECT '2024-07-01 00:00:00'", q)
}

func TestInterpolateParamsTime(t *testing.T) {
	testTime, err := time.Parse(time.RFC3339, "2024-07-01T01:02:03Z")
	assert.Nil(t, err)

	c := createTestConnection(t)
	q, err := c.interpolateParams("SELECT ?", []driver.Value{testTime})
	assert.Nil(t, err)
	assert.Equal(t, "SELECT '2024-07-01 01:02:03'", q)
}

func TestInterpolateParamsTimeMicro(t *testing.T) {
	testTimeMicro, err := time.Parse(time.RFC3339, "2024-07-02T01:02:03Z")
	assert.Nil(t, err)
	testTimeMicro = testTimeMicro.Add(time.Microsecond * 123456)

	c := createTestConnection(t)
	q, err := c.interpolateParams("SELECT ?", []driver.Value{testTimeMicro})
	assert.Nil(t, err)
	assert.Equal(t, "SELECT '2024-07-02 01:02:03.123456'", q)
}

func TestBuildExecutionParams(t *testing.T) {
	testTime, err := time.Parse(time.RFC3339, "2024-07-01T00:00:00Z")
	assert.Nil(t, err)
	testTimeMicro, err := time.Parse(time.RFC3339, "2024-07-02T01:02:03Z")
	assert.Nil(t, err)
	testTimeMicro = testTimeMicro.Add(time.Microsecond * 123456)

	testCases := []struct {
		name        string
		inputArgs   []driver.Value
		expectedErr error
		expected    []string
	}{
		{
			// buildExecutionParams must return a nil slice (not an empty
			// slice) when there are no arguments, so the resulting
			// StartQueryExecution call leaves ExecutionParameters unset.
			// Athena rejects non-parameterized queries that carry an empty
			// ExecutionParameters array.
			name:        "No arguments",
			inputArgs:   []driver.Value{},
			expectedErr: nil,
			expected:    nil,
		},
		{
			name:        "Bool",
			inputArgs:   []driver.Value{true, false},
			expectedErr: nil,
			expected:    []string{"true", "false"},
		},
		{
			name:        "Zero-value time",
			inputArgs:   []driver.Value{time.Time{}},
			expectedErr: nil,
			expected:    []string{"NULL"}, // Zero time has no Trino equivalent. Matches interpolateParams behavior.
		},
		{
			// Like interpolateParams(), buildExecutionParams() adds an additional 500 nanoseconds.
			name:        "501 nanoseconds is still < 1 microsecond", // From TestConnection_InterpolateParams_Bool
			inputArgs:   []driver.Value{time.Time{}.Add(time.Nanosecond)},
			expectedErr: nil,
			expected:    []string{"'0001-01-01 00:00:00'"}, // Matches interpolateParams behavior.
		},
		{
			name:        "For non-zero-value time.Times, Date and time are present, even if time is zero-value",
			inputArgs:   []driver.Value{testTime},
			expectedErr: nil,
			expected:    []string{"'2024-07-01 00:00:00'"},
		},
		{
			name:        "Datetime with Microseconds",
			inputArgs:   []driver.Value{testTimeMicro},
			expectedErr: nil,
			expected:    []string{"'2024-07-02 01:02:03.123456'"},
		},
		{
			// Athena evaluates each execution parameter as a SQL
			// expression, so values must arrive quoted.
			name:        "Byte Slice is quoted",
			inputArgs:   []driver.Value{[]byte{'0'}},
			expectedErr: nil,
			expected:    []string{"'0'"},
		},
		{
			name:        "String is quoted",
			inputArgs:   []driver.Value{"This is a string"},
			expectedErr: nil,
			expected:    []string{"'This is a string'"},
		},
		{
			name:        "String with quotes and control chars",
			inputArgs:   []driver.Value{"This is a string with ' single quotes and \n chars"},
			expectedErr: nil,
			expected:    []string{"'This is a string with '' single quotes and \n chars'"},
		},
		{
			// Raw is the explicit opt-out for expression arguments.
			name:        "Raw passes through unquoted",
			inputArgs:   []driver.Value{Raw("TIMESTAMP " + FormatString("2024-07-01 00:00:00"))},
			expectedErr: nil,
			expected:    []string{"TIMESTAMP '2024-07-01 00:00:00'"},
		},
		{
			name:        "Nil -> NULL",
			inputArgs:   []driver.Value{nil},
			expectedErr: nil,
			expected:    []string{"NULL"},
		},
		{
			name: "Every supported type",
			inputArgs: []driver.Value{int64(-10), uint64(42), 1.23, true, testTime, []byte("This is a slice of bytes"),
				"This is a string"},
			expectedErr: nil,
			expected:    []string{"-10", "42", "1.23", "true", "'2024-07-01 00:00:00'", "'This is a slice of bytes'", "'This is a string'"},
		},
	}
	c := createTestConnection(t)
	for _, tc := range testCases {
		// https://go.dev/blog/loopvar-preview
		// Pre-Go-1.22, the loop variable `tc` is shared between each loop iteration. Because t.Parallel() is called
		// in createTestConnection(), the test cases run concurrently, and `tc` is likely to change mid-test. We can
		// avoid that by creating a local copy.
		t.Run(tc.name, func(t *testing.T) {
			actual, err := c.buildExecutionParams(tc.inputArgs)
			assert.Equal(t, tc.expectedErr, err)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

// TestBuildExecutionParams_RejectsNonFiniteFloats pins a client-side error
// for NaN/+Inf/-Inf: these render as bare words that aren't valid Trino
// numeric literals, so letting them through would send malformed SQL.
func TestBuildExecutionParams_RejectsNonFiniteFloats(t *testing.T) {
	c := createTestConnection(t)
	for _, f := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := c.buildExecutionParams([]driver.Value{f})
		assert.Error(t, err, "want error for non-finite float %v", f)
	}
}

func createTestConnection(t *testing.T) *Connection {
	t.Parallel()
	testConf := NewNoOpsConfig()
	staticCredentials := credentials.NewStaticCredentialsProvider(testConf.AccessIDOrEnv(),
		testConf.SecretAccessKeyOrEnv(),
		testConf.SessionTokenOrEnv())
	awsConfig := aws.Config{
		Region:      testConf.RegionOrEnv(),
		Credentials: staticCredentials,
	}
	athenaClient := athena.NewFromConfig(awsConfig)
	c := &Connection{
		athenaClient: athenaClient,
		connector:    NoopsSQLConnector(),
		tracer:       NewObservability(NewNoOpsConfig(), nil, nil),
	}
	return c
}

func TestConnection_QueryContext2(t *testing.T) {
	t.Parallel()
	c := &Connection{
		athenaClient: newMockAthenaClient(),
		connector:    NoopsSQLConnector(),
		tracer:       NewObservability(NewNoOpsConfig(), nil, nil),
	}
	driverRows, err := c.QueryContext(context.Background(), "StartQueryExecution_nil_error x",
		[]driver.NamedValue{})
	assert.Nil(t, driverRows)
	assert.NotNil(t, err)

	driverRows, err = c.QueryContext(context.Background(), "StartQueryExecution_OK_GetQueryExecutionWithContext_QueryExecutionStateCancelled x",
		[]driver.NamedValue{})
	assert.Nil(t, driverRows)
	assert.Equal(t, context.Canceled, err)

	driverRows, err = c.QueryContext(context.Background(), "StartQueryExecution_OK_GetQueryExecutionWithContext_QueryExecutionStateFailed x",
		[]driver.NamedValue{})
	assert.Nil(t, driverRows)
	assert.Equal(t, ErrTestMockFailedByAthena, err)

}

// TestConnection_QueryContext_WorkgroupVariants drives QueryContext with
// the four workgroup-resolution permutations the driver has to handle:
// remote-WG-unknown (default), remote-WG-found-and-enabled,
// remote-WG-found-but-disabled, and remote-WG-creation-disallowed.
// In every variant a bogus query is expected to surface an error.
func TestConnection_QueryContext_WorkgroupVariants(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config, *mockAthenaClient)
		// ping is a substring of the expected Ping error; "" skips the
		// check. Ping surfaces the real failure rather than ErrBadConn.
		ping string
	}{
		{name: "WG remote create attempted"},
		{
			name:   "WG found, enabled",
			mutate: func(_ *Config, nm *mockAthenaClient) { nm.GetWGStatus = true },
		},
		{
			name: "WG found, disabled",
			mutate: func(_ *Config, nm *mockAthenaClient) {
				nm.GetWGStatus = true
				nm.WGDisabled = true
			},
		},
		{
			name: "remote WG creation disallowed",
			mutate: func(cfg *Config, _ *mockAthenaClient) {
				cfg.WGRemoteCreation = false
			},
			ping: "workgroup remote creation is disabled",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var mut []func(*Config, *mockAthenaClient)
			if tc.mutate != nil {
				mut = append(mut, tc.mutate)
			}
			c := newTestConnWithWG(mut...)
			if tc.ping != "" {
				err := c.Ping(context.Background())
				assert.ErrorContains(t, err, tc.ping)
				assert.NotErrorIs(t, err, driver.ErrBadConn)
			}
			driverRows, err := c.QueryContext(context.Background(), "StartQueryExecution_nil_error x",
				[]driver.NamedValue{})
			assert.Nil(t, driverRows)
			assert.NotNil(t, err)
		})
	}
}

func TestConnection_QueryContext7(t *testing.T) {
	t.Parallel()
	c := createConnectionFixture()

	e := c.Ping(context.Background())
	assert.Nil(t, e)

	driverRows, err := c.QueryContext(context.Background(), "StartQueryExecution_nil_error x",
		[]driver.NamedValue{})
	assert.Nil(t, driverRows)
	assert.NotNil(t, err)

	dr, er := c.ExecContext(context.Background(), "StartQueryExecution_nil_error x",
		[]driver.NamedValue{})
	assert.Nil(t, dr)
	assert.NotNil(t, er)

	driverRows, err = c.QueryContext(context.Background(), "StartQueryExecution_OK_GetQueryExecutionWithContext_QueryExecutionStateCancelled x",
		[]driver.NamedValue{})
	assert.Nil(t, driverRows)
	assert.Equal(t, context.Canceled, err)

	driverRows, err = c.QueryContext(context.Background(), "StartQueryExecution_OK_GetQueryExecutionWithContext_QueryExecutionStateFailed x",
		[]driver.NamedValue{})
	assert.Nil(t, driverRows)
	assert.Equal(t, ErrTestMockFailedByAthena, err)

	query := "SELECTExecContext_OK x"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, er)
	assert.NotNil(t, dr)

	query = "SELECTQueryContext_OK x"
	driverRows, err = c.QueryContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, err)
	assert.NotNil(t, driverRows)

	query = "SELECTQueryContext_OK x"
	value := driver.NamedValue{Value: uint64(0)}
	driverRows, err = c.QueryContext(context.Background(), query, []driver.NamedValue{value})
	assert.NotNil(t, err)
	assert.Nil(t, driverRows)

	query = "SELECTQueryContext_? x"
	value = driver.NamedValue{Value: "OK"}
	driverRows, err = c.QueryContext(context.Background(), query, []driver.NamedValue{value})
	assert.Nil(t, err)
	assert.NotNil(t, driverRows)

	query = "SELECTQueryContext_CANCEL_OK x"
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	driverRows, err = c.QueryContext(ctx, query, []driver.NamedValue{})
	assert.NotNil(t, err)
	assert.Nil(t, driverRows)

	query = "SELECTQueryContext_CANCEL_FAIL x"
	ctx, cancel = context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	driverRows, err = c.QueryContext(ctx, query, []driver.NamedValue{})
	assert.NotNil(t, err)
	assert.Nil(t, driverRows)

	// Query stays Queued; a 1-second service limit trips the timeout on
	// the first poll.
	c.connector.config.ServiceLimit = &ServiceLimitOverride{DDLQueryTimeout: 1, DMLQueryTimeout: 1}
	query = "SELECTQueryContext_TIMEOUT x"
	ctx, cancel = context.WithTimeout(context.Background(), 1*time.Hour)
	defer cancel()
	driverRows, err = c.QueryContext(ctx, query, []driver.NamedValue{})
	assert.NotNil(t, err)
	assert.Nil(t, driverRows)

	query = randString(MAXQueryStringLength * 10)
	driverRows, err = c.QueryContext(context.Background(), query, []driver.NamedValue{})
	assert.Equal(t, ErrInvalidQuery, err)
	assert.Nil(t, driverRows)

	// Cancelled by AWS Athena
	query = "SELECTQueryContext_AWS_CANCEL x"
	driverRows, err = c.QueryContext(context.Background(), query, []driver.NamedValue{})
	assert.NotNil(t, err)
	assert.Nil(t, driverRows)

	// failed by AWS Athena
	query = "SELECTQueryContext_AWS_FAIL x"
	driverRows, err = c.QueryContext(context.Background(), query, []driver.NamedValue{})
	assert.NotNil(t, err)
	assert.Nil(t, driverRows)

	query = "SELECTQueryContext_CANCEL_OK x"
	ctx, cancel = context.WithTimeout(context.Background(), PoolInterval*time.Second*2)
	defer cancel()
	driverRows, err = c.QueryContext(ctx, query, []driver.NamedValue{})
	assert.NotNil(t, err)
	assert.Nil(t, driverRows)

	query = "00000000-0000-0000-0000-000000000000"
	driverRows, err = c.QueryContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, err)
	assert.NotNil(t, driverRows)
}

func BenchmarkConnection_QueryContext(b *testing.B) {
	c := createConnectionFixture()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.Ping(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func createConnectionFixture() *Connection {
	nm := newMockAthenaClient()
	c := &Connection{
		athenaClient: nm,
		connector:    NoopsSQLConnector(),
		tracer:       NewObservability(NewNoOpsConfig(), nil, nil),
	}
	var s3bucket string = "s3://fake-query-results-arbitrary-bucket/"
	wgTags := NewWGTags()
	wgTags.AddTag("Uber Author", "henry.wu")
	wgTags.AddTag("Uber Role", "Engineer")
	wg := NewWG("henry_wu", nil, wgTags)
	testConf := NewNoOpsConfig()
	_ = testConf.SetOutputBucket(s3bucket)
	_ = testConf.SetRegion(regions[rand.Int31n(int32(len(regions)))])
	testConf.User = "henry.wu"
	testConf.DB = randString(8) // default
	testConf.WGRemoteCreation = true
	nm.CreateWGStatus = true

	_ = testConf.SetWorkGroup(wg)
	c.connector.config = testConf
	return c
}

func TestMoneyWise(t *testing.T) {
	t.Parallel()
	c := newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
		// Replace the default "henry_wu" WG with the default WG so the
		// driver short-circuits resolveWorkgroup.
		wgTags := NewWGTags()
		wgTags.AddTag("Uber User", "henry.wu")
		wgTags.AddTag("Uber Asset", "abc.efg")
		_ = cfg.SetWorkGroup(NewWG(DefaultWGName, nil, wgTags))
		cfg.MoneyWise = true
	})
	query := "SELECTExecContext_OK x"
	dr, er := c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, er)
	assert.NotNil(t, dr)

	query = "00000000-0000-0000-0000-000000000000"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, er)
	assert.NotNil(t, dr)

	query = "SELECTQueryContext_CANCEL_OK x"
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	dr2, err := c.QueryContext(ctx, query, []driver.NamedValue{})
	assert.NotNil(t, err)
	assert.Nil(t, dr2)

	query = "SELECTQueryContext_AWS_CANCEL x"
	ctx, cancel = context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	dr2, err = c.QueryContext(ctx, query, []driver.NamedValue{})
	assert.NotNil(t, err)
	assert.Nil(t, dr2)
}

func TestConnection_CachedQuery(t *testing.T) {
	t.Parallel()
	c := newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
		// CachedQuery's resolveWorkgroup path uses the empty/default WG,
		// so reset the WG name to fall through quickly. Moneywise mode is
		// what this test really exercises.
		_ = cfg.SetWorkGroup(NewWG("", nil, nil))
		cfg.MoneyWise = true
	})
	dr, er := c.ExecContext(context.Background(),
		"00000000-0000-0000-0000-000000000000", []driver.NamedValue{})
	assert.Nil(t, er)
	assert.NotNil(t, dr)
}

func Test_PseudoCommand(t *testing.T) {
	t.Parallel()
	c := newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
		wgTags := NewWGTags()
		wgTags.AddTag("Uber User", "henry.wu")
		wgTags.AddTag("Uber Asset", "abc.efg")
		_ = cfg.SetWorkGroup(NewWG(DefaultWGName, nil, wgTags))
		cfg.MoneyWise = true
	})

	query := "pc:get_query_id"
	dr, er := c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.NotNil(t, er)
	assert.Nil(t, dr)

	query = "pc:get_query_id FAILED_AFTER_GETQID x"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.Equal(t, "FAILED_AFTER_GETQID_FAILED", er.Error())
	assert.Nil(t, dr)

	query = "pc:get_query_id FAILED_AFTER_GETQID2 x"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, er)
	assert.NotNil(t, dr)

	query = "pc:badcommand"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.NotNil(t, er)
	assert.Nil(t, dr)

	query = "pc:get_query_id SELECTQueryContext_CANCEL_OK x"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, er)
	assert.NotNil(t, dr)

	query = "pc:stop_query_id c89088ab-595d-4ee6-a9ce-73b55aeb8954"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, er)
	assert.NotNil(t, dr)

	query = "pc:stop_query_id c89088ab-595d-4ee6-a9ce-73b55aeb8955"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.NotNil(t, er)
	assert.Nil(t, dr)

	query = "pc:stop_query_id"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.NotNil(t, er)
	assert.Nil(t, dr)

	query = "pc:stop_query_id 123"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.NotNil(t, er)
	assert.Nil(t, dr)

	query = "pc:get_query_id_status c89088ab-595d-4ee6-a9ce-73b55aeb8900"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, er)
	assert.NotNil(t, dr)

	query = "pc:get_query_id_status c89088ab-595d-4ee6-a9ce-73b55aeb8111"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.NotNil(t, er)
	assert.Nil(t, dr)

	query = "pc:get_query_id_status"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.NotNil(t, er)
	assert.Nil(t, dr)

	// GetQueryExecution returns a non-nil output with a nil Status: must
	// error, not panic on nil deref.
	query = "pc:get_query_id_status c89088ab-595d-4ee6-a9ce-73b55aeb8nil"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.NotNil(t, er)
	assert.Nil(t, dr)

	query = "pc:get_query_id_status 123"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.NotNil(t, er)
	assert.Nil(t, dr)

	query = "pc:get_driver_version"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, er)
	assert.NotNil(t, dr)

	// A command that merely starts with get_driver_version is not that
	// command: it must fall through to the "doesn't exist" error.
	for _, query := range []string{"pc:get_driver_version_history", "pc:get_driver_versionn"} {
		dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
		assert.NotNil(t, er, query)
		assert.Contains(t, er.Error(), "doesn't exist", query)
		assert.Nil(t, dr, query)
	}
}

// --- workgroup resolution: AWS error classification -----------------------

// TestResolveWorkgroup_NonNotFoundErrorIsSurfaced is the counter-direction
// of the isWGNotFound check: a throttling response is NOT "the workgroup is
// absent", so it must come back to the caller untouched and must never
// trigger a CreateWorkGroup.
func TestResolveWorkgroup_NonNotFoundErrorIsSurfaced(t *testing.T) {
	t.Parallel()
	throttle := &athenatypes.TooManyRequestsException{
		Message: aws.String("Rate exceeded"),
	}
	cases := []struct {
		name string
		err  error
	}{
		{"modeled throttling", throttle},
		{"smithy ThrottlingException", &smithy.GenericAPIError{
			Code: "ThrottlingException", Message: "Rate exceeded"}},
		// Same modeled type Athena uses for "not found", but a different
		// message: a substring check that is too loose would misfire here.
		{"InvalidRequest, not a miss", &athenatypes.InvalidRequestException{
			Message: aws.String("WorkGroup name is invalid.")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestConnWithWG(func(_ *Config, nm *mockAthenaClient) {
				nm.CreateWGStatus = true // would succeed if wrongly called
				nm.setErr("GetWorkGroup", tc.err)
			})
			nm := c.athenaClient.(*mockAthenaClient)

			_, err := c.resolveWorkgroup(context.Background())
			assert.ErrorIs(t, err, tc.err, "the AWS error must be surfaced verbatim")
			assert.Zero(t, nm.callCount("CreateWorkGroup"),
				"a non-not-found error must never be read as 'absent, go create it'")
			assert.False(t, c.connector.wgOnce.done())
		})
	}
}

// TestResolveWorkgroup_ConcurrentCreationRecovery covers the lost-the-race
// path: GetWorkGroup misses, our CreateWorkGroup fails, but a re-check finds
// the workgroup another connection created meanwhile. Losing that race is
// success.
func TestResolveWorkgroup_ConcurrentCreationRecovery(t *testing.T) {
	t.Parallel()
	c := newTestConnWithWG(func(_ *Config, nm *mockAthenaClient) {
		nm.setErr("CreateWorkGroup", &athenatypes.InvalidRequestException{
			Message: aws.String("WorkGroup already exists")})
		nm.getWGFn = func(n int) (*athena.GetWorkGroupOutput, error) {
			if n == 1 {
				return nil, &athenatypes.InvalidRequestException{
					Message: aws.String("WorkGroup is not found.")}
			}
			return &athena.GetWorkGroupOutput{WorkGroup: &athenatypes.WorkGroup{
				State: athenatypes.WorkGroupStateEnabled}}, nil
		}
	})
	nm := c.athenaClient.(*mockAthenaClient)

	wg, err := c.resolveWorkgroup(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "henry_wu", wg.Name)
	assert.True(t, c.connector.wgOnce.done())
	assert.Equal(t, 2, nm.callCount("GetWorkGroup"), "the miss must be re-checked")
	assert.Equal(t, 1, nm.callCount("CreateWorkGroup"))
}

// TestResolveWorkgroup_ConcurrentHammer runs resolveWorkgroup from 32
// goroutines on one connector (run under -race). Cold start: every caller
// must succeed even though they all observe the same miss. Warm: the
// verified flag must make every later call a no-op — zero AWS traffic.
func TestResolveWorkgroup_ConcurrentHammer(t *testing.T) {
	t.Parallel()
	const n = 32
	c := newTestConnWithWG(func(_ *Config, nm *mockAthenaClient) {
		nm.CreateWGStatus = true // GetWGStatus stays false => always a miss
	})
	nm := c.athenaClient.(*mockAthenaClient)

	hammer := func() {
		var wg sync.WaitGroup
		for range n {
			wg.Go(func() {
				w, err := c.resolveWorkgroup(context.Background())
				assert.NoError(t, err)
				assert.Equal(t, "henry_wu", w.Name)
			})
		}
		wg.Wait()
	}

	hammer()
	assert.True(t, c.connector.wgOnce.done())
	// Cold start is single-flighted under wgOnce: one GetWorkGroup and one
	// CreateWorkGroup total, no matter how many callers race.
	assert.Equal(t, 1, nm.callCount("GetWorkGroup"))
	assert.Equal(t, 1, nm.callCount("CreateWorkGroup"))

	nm.resetCalls()
	hammer()
	assert.Zero(t, nm.callCount("GetWorkGroup"), "verified workgroup must not be re-checked")
	assert.Zero(t, nm.callCount("CreateWorkGroup"))
}

// --- StartQueryExecution input assembly -----------------------------------

var testCtxEncryption = &athenatypes.EncryptionConfiguration{
	EncryptionOption: athenatypes.EncryptionOptionSseKms,
	KmsKey:           aws.String("ctx-key"),
}

// TestQueryContext_StartQueryExecutionInput pins every security- and
// correctness-relevant field the driver assembles for StartQueryExecution,
// including the ctx-beats-config precedence rules.
func TestQueryContext_StartQueryExecutionInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		cfg   func(*Config)
		ctx   func(context.Context) context.Context
		check func(*testing.T, *athena.StartQueryExecutionInput)
	}{
		{
			name: "ctx encryption beats config encryption",
			cfg: func(c *Config) {
				c.ResultEncryption = &athenatypes.EncryptionConfiguration{
					EncryptionOption: athenatypes.EncryptionOptionSseS3}
			},
			ctx: func(ctx context.Context) context.Context {
				return context.WithValue(ctx, ResultEncryptionKey, testCtxEncryption)
			},
			check: func(t *testing.T, in *athena.StartQueryExecutionInput) {
				assert.Equal(t, testCtxEncryption, in.ResultConfiguration.EncryptionConfiguration)
			},
		},
		{
			name: "WithResultEncryption helper",
			ctx: func(ctx context.Context) context.Context {
				return WithResultEncryption(ctx, athenatypes.EncryptionOptionCseKms, "helper-key")
			},
			check: func(t *testing.T, in *athena.StartQueryExecutionInput) {
				enc := in.ResultConfiguration.EncryptionConfiguration
				assert.Equal(t, athenatypes.EncryptionOptionCseKms, enc.EncryptionOption)
				assert.Equal(t, "helper-key", aws.ToString(enc.KmsKey))
			},
		},
		{
			name: "config encryption used when ctx has none",
			cfg: func(c *Config) {
				c.ResultEncryption = &athenatypes.EncryptionConfiguration{
					EncryptionOption: athenatypes.EncryptionOptionSseS3}
			},
			check: func(t *testing.T, in *athena.StartQueryExecutionInput) {
				assert.Equal(t, athenatypes.EncryptionOptionSseS3,
					in.ResultConfiguration.EncryptionConfiguration.EncryptionOption)
			},
		},
		{
			name: "neither set leaves encryption nil",
			check: func(t *testing.T, in *athena.StartQueryExecutionInput) {
				assert.Nil(t, in.ResultConfiguration.EncryptionConfiguration)
				assert.Nil(t, in.ResultReuseConfiguration)
				assert.Nil(t, in.ResultConfiguration.ExpectedBucketOwner)
			},
		},
		{
			name: "WithResultReuse lands in ResultReuseConfiguration",
			ctx:  func(ctx context.Context) context.Context { return WithResultReuse(ctx, 42*time.Minute) },
			check: func(t *testing.T, in *athena.StartQueryExecutionInput) {
				byAge := in.ResultReuseConfiguration.ResultReuseByAgeConfiguration
				assert.True(t, byAge.Enabled)
				assert.Equal(t, int32(42), aws.ToInt32(byAge.MaxAgeInMinutes))
			},
		},
		{
			name: "result reuse is clamped to Athena's 7-day maximum",
			ctx:  func(ctx context.Context) context.Context { return WithResultReuse(ctx, 30*24*time.Hour) },
			check: func(t *testing.T, in *athena.StartQueryExecutionInput) {
				assert.Equal(t, int32(10080),
					aws.ToInt32(in.ResultReuseConfiguration.ResultReuseByAgeConfiguration.MaxAgeInMinutes))
			},
		},
		{
			name: "ctx bucket owner beats config bucket owner",
			cfg:  func(c *Config) { c.ExpectedBucketOwner = "111111111111" },
			ctx: func(ctx context.Context) context.Context {
				return context.WithValue(ctx, ExpectedBucketOwnerKey, "222222222222")
			},
			check: func(t *testing.T, in *athena.StartQueryExecutionInput) {
				assert.Equal(t, "222222222222", aws.ToString(in.ResultConfiguration.ExpectedBucketOwner))
			},
		},
		{
			name: "config bucket owner used when ctx has none",
			cfg:  func(c *Config) { c.ExpectedBucketOwner = "111111111111" },
			check: func(t *testing.T, in *athena.StartQueryExecutionInput) {
				assert.Equal(t, "111111111111", aws.ToString(in.ResultConfiguration.ExpectedBucketOwner))
			},
		},
		{
			name: "ctx catalog beats CatalogOrDefault",
			cfg:  func(c *Config) { c.Catalog = "cfg_catalog" },
			ctx: func(ctx context.Context) context.Context {
				return context.WithValue(ctx, CatalogKey, "ctx_catalog")
			},
			check: func(t *testing.T, in *athena.StartQueryExecutionInput) {
				assert.Equal(t, "ctx_catalog", aws.ToString(in.QueryExecutionContext.Catalog))
			},
		},
		{
			name: "catalog falls back to the package default",
			check: func(t *testing.T, in *athena.StartQueryExecutionInput) {
				assert.Equal(t, DefaultCatalog, aws.ToString(in.QueryExecutionContext.Catalog))
			},
		},
		{
			name: "ctx client request token is used verbatim",
			ctx: func(ctx context.Context) context.Context {
				return context.WithValue(ctx, ClientRequestTokenKey, "caller-supplied-idempotency-token")
			},
			check: func(t *testing.T, in *athena.StartQueryExecutionInput) {
				assert.Equal(t, "caller-supplied-idempotency-token", aws.ToString(in.ClientRequestToken))
			},
		},
		{
			name: "a fresh token is generated when ctx has none",
			check: func(t *testing.T, in *athena.StartQueryExecutionInput) {
				tok := aws.ToString(in.ClientRequestToken)
				assert.Len(t, tok, 36)
				assert.True(t, IsQID(tok), "generated token should be a UUIDv4")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newTestConnWithWG(func(cfg *Config, nm *mockAthenaClient) {
				nm.GetWGStatus = true
				if tc.cfg != nil {
					tc.cfg(cfg)
				}
			})
			nm := c.athenaClient.(*mockAthenaClient)
			ctx := context.Background()
			if tc.ctx != nil {
				ctx = tc.ctx(ctx)
			}

			_, err := c.QueryContext(ctx, "select 1", nil)
			assert.NoError(t, err)
			in := nm.lastStartInput
			// Fields that must hold on every single query.
			assert.Equal(t, "default", aws.ToString(in.QueryExecutionContext.Database))
			assert.Equal(t, "henry_wu", aws.ToString(in.WorkGroup))
			assert.Equal(t, "s3://fake-query-results-arbitrary-bucket/",
				aws.ToString(in.ResultConfiguration.OutputLocation))
			tc.check(t, in)
		})
	}
}

// --- end-to-end through database/sql --------------------------------------

// mockDBConnector hands database/sql a Connection wired to the mock client,
// so a test can drive the real db.Query / db.QueryContext path without AWS.
type mockDBConnector struct{ conn *Connection }

func (m mockDBConnector) Connect(context.Context) (driver.Conn, error) { return m.conn, nil }
func (m mockDBConnector) Driver() driver.Driver                        { return &SQLDriver{} }

func newTestDB(mut ...func(*Config, *mockAthenaClient)) (*sql.DB, *mockAthenaClient) {
	c := newTestConnWithWG(append([]func(*Config, *mockAthenaClient){
		func(_ *Config, nm *mockAthenaClient) { nm.GetWGStatus = true },
	}, mut...)...)
	db := sql.OpenDB(mockDBConnector{c})
	db.SetMaxOpenConns(1)
	return db, c.athenaClient.(*mockAthenaClient)
}

// TestDB_SQLInjectionArgIsQuoted is the end-to-end injection guard: a hostile
// string handed to db.Query must reach Athena as one inert, quote-escaped SQL
// string literal in ExecutionParameters, never as raw executable text.
func TestDB_SQLInjectionArgIsQuoted(t *testing.T) {
	t.Parallel()
	db, nm := newTestDB()
	defer db.Close()

	const payload = "x' OR 1=1 --"
	rows, err := db.QueryContext(context.Background(), "select ?", payload)
	assert.NoError(t, err)
	defer rows.Close()

	assert.Equal(t, "select ?", aws.ToString(nm.lastStartInput.QueryString),
		"the payload must never be spliced into the query text")
	assert.Equal(t, []string{"'x'' OR 1=1 --'"}, nm.lastStartInput.ExecutionParameters)
	// The quoting is what makes it a value: the payload's own quote is
	// doubled, so it cannot terminate the literal.
	assert.NotContains(t, nm.lastStartInput.ExecutionParameters[0], "x' OR")
}

// TestDB_MaskedColumnIsCaseInsensitive pins the masking fix: Athena reports
// unquoted identifiers lowercased, so a mask registered in any casing must
// still apply to the column the service actually names.
func TestDB_MaskedColumnIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	db, _ := newTestDB(func(cfg *Config, _ *mockAthenaClient) {
		// Mock reports this column as "_col0"; register it shouting.
		cfg.SetMaskedColumnValue("_COL0", "***masked***")
	})
	defer db.Close()

	var got string
	assert.NoError(t, db.QueryRow("select 1").Scan(&got))
	assert.Equal(t, "***masked***", got)
}

// A mask registered via the DSN survives the same casing mismatch.
func TestDSN_MaskedColumnIsCaseInsensitive(t *testing.T) {
	t.Parallel()
	cfg, err := NewConfig("s3://bucket/?region=us-east-1&masked_SSN=xxx")
	assert.NoError(t, err)
	v, ok := cfg.CheckColumnMasked("ssn")
	assert.True(t, ok)
	assert.Equal(t, "xxx", v)
}

// TestQueryContext_NilQueryExecutionID covers a StartQueryExecution that
// returns 200 with no QueryExecutionId (proxy / LocalStack / a custom
// AthenaClient): the driver must report an error, never panic on the
// dereference.
func TestQueryContext_NilQueryExecutionID(t *testing.T) {
	t.Parallel()
	newConn := func() *Connection {
		return newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
			cfg.WorkGroup = nil // skip remote workgroup resolution
		})
	}

	// nil response: the mock answers (nil, nil) for any unmapped query.
	c := newConn()
	rows, err := c.QueryContext(context.Background(), "select nil_qid_probe", nil)
	assert.Nil(t, rows)
	assert.ErrorContains(t, err, "no query execution ID")

	// non-nil response, nil QueryExecutionId.
	c = newConn()
	c.athenaClient = nilQIDClient{c.athenaClient.(*mockAthenaClient)}
	rows, err = c.QueryContext(context.Background(), "select 1", nil)
	assert.Nil(t, rows)
	assert.ErrorContains(t, err, "no query execution ID")
}

// nilQIDClient answers StartQueryExecution with a 200-shaped response whose
// QueryExecutionId is absent.
type nilQIDClient struct{ *mockAthenaClient }

func (nilQIDClient) StartQueryExecution(context.Context, *athena.StartQueryExecutionInput, ...func(*athena.Options)) (*athena.StartQueryExecutionOutput, error) {
	return &athena.StartQueryExecutionOutput{}, nil
}

// TestResolveWorkgroup_ConcurrentQueryContext is the pooled-connection
// version of the hammer: N distinct Connections sharing one SQLConnector
// all issue their first query at once. The workgroup check must be
// single-flighted across the whole pool — one GetWorkGroup, one
// CreateWorkGroup — or an Athena API throttle surfaces as failed queries.
func TestResolveWorkgroup_ConcurrentQueryContext(t *testing.T) {
	t.Parallel()
	const n = 32
	seed := newTestConnWithWG(func(_ *Config, nm *mockAthenaClient) {
		nm.CreateWGStatus = true // GetWGStatus stays false => always a miss
		// Fail at StartQueryExecution: the workgroup check has already run
		// by then, and stopping there keeps the goroutines off the shared
		// (test-only, non-concurrent) result-page fixtures.
		nm.setErr("StartQueryExecution", ErrTestMockGeneric)
	})
	nm := seed.athenaClient.(*mockAthenaClient)

	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			// A fresh Connection per goroutine, same connector and client —
			// exactly what a database/sql pool cold start looks like.
			c := &Connection{
				athenaClient: nm,
				connector:    seed.connector,
				tracer:       NewObservability(NewNoOpsConfig(), nil, nil),
			}
			_, err := c.QueryContext(context.Background(), "SELECTQueryContext_OK x", nil)
			assert.ErrorIs(t, err, ErrTestMockGeneric,
				"every caller must get past workgroup resolution")
		})
	}
	wg.Wait()

	assert.True(t, seed.connector.wgOnce.done())
	assert.Equal(t, 1, nm.callCount("GetWorkGroup"))
	assert.Equal(t, 1, nm.callCount("CreateWorkGroup"))
	assert.Equal(t, n, nm.callCount("StartQueryExecution"))
}

// TestPing_ContextErrors pins the Pinger contract: database/sql retries an
// ErrBadConn Ping on two more connections and throws the real error away,
// so a cancelled or expired context must come back as itself.
func TestPing_ContextErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{"cancelled", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, func() {}
		}, context.Canceled},
		{"deadline exceeded", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			return ctx, cancel
		}, context.DeadlineExceeded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
				cfg.WorkGroup = nil
			})
			ctx, cancel := tc.ctx()
			defer cancel()
			err := c.Ping(ctx)
			assert.ErrorIs(t, err, tc.want)
			assert.NotErrorIs(t, err, driver.ErrBadConn)
		})
	}

	// A conn that really is unusable still reports ErrBadConn.
	closed := newTestConnWithWG()
	assert.NoError(t, closed.Close())
	assert.ErrorIs(t, closed.Ping(context.Background()), driver.ErrBadConn)
}
