// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/athena"
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
	assert.Equal(t, "'0000-00-00'", q)
	assert.Nil(t, err)
	q, err = c.interpolateParams("?", []driver.Value{time.Now()})
	assert.NotEqual(t, q, "'0000-00-00'")
	assert.Nil(t, err)
	q, err = c.interpolateParams("?", []driver.Value{[]byte{'0'}})
	assert.Equal(t, "_binary'0'", q)
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

// We don't support placeholder in string literal for now.
// https://github.com/go-sql-driver/mysql/pull/490
func TestInterpolateParamsPlaceholderInString(t *testing.T) {
	c := createTestConnection(t)

	q, err := c.interpolateParams("SELECT 'abc?xyz',?", []driver.Value{int64(42)})
	// When InterpolateParams support string literal, this should return `"SELECT 'abc?xyz', 42`
	if err != ErrInvalidQuery {
		t.Errorf("Expected err=ErrInvalidQuery, got err=%#v, q=%#v", err, q)
	}
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
			expected:    []string{"'0000-00-00'"}, // Special-cased. Matches interpolateParams behavior.
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
			name:        "Byte Slice - Caller must use utils.go/FormatBytes before passing in query args",
			inputArgs:   []driver.Value{[]byte{'0'}},
			expectedErr: nil,
			expected:    []string{"0"}, // No change
		},
		{
			name:        "Byte Slice - After FormatBytes",
			inputArgs:   []driver.Value{FormatBytes([]byte{'0'})},
			expectedErr: nil,
			expected:    []string{"_binary'0'"},
		},
		{
			name:        "String - Caller must use utils.go/FormatString before passing in query args",
			inputArgs:   []driver.Value{"This is a string"},
			expectedErr: nil,
			expected:    []string{"This is a string"}, // No change
		},
		{
			name:        "String - After FormatString",
			inputArgs:   []driver.Value{FormatString("This is a string with ' single quotes and \n chars")},
			expectedErr: nil,
			expected:    []string{"'This is a string with '' single quotes and \\n chars'"},
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
			expected:    []string{"-10", "42", "1.23", "true", "'2024-07-01 00:00:00'", "This is a slice of bytes", "This is a string"},
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
	driverRows, err := c.QueryContext(context.Background(), "StartQueryExecution_nil_error",
		[]driver.NamedValue{})
	assert.Nil(t, driverRows)
	assert.NotNil(t, err)

	driverRows, err = c.QueryContext(context.Background(), "StartQueryExecution_OK_GetQueryExecutionWithContext_QueryExecutionStateCancelled",
		[]driver.NamedValue{})
	assert.Nil(t, driverRows)
	assert.Equal(t, context.Canceled, err)

	driverRows, err = c.QueryContext(context.Background(), "StartQueryExecution_OK_GetQueryExecutionWithContext_QueryExecutionStateFailed",
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
		ping   error // expected db.Ping result; nil to skip the Ping check
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
			ping: driver.ErrBadConn,
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
			if tc.ping != nil {
				assert.Equal(t, tc.ping, c.Ping(context.Background()))
			}
			driverRows, err := c.QueryContext(context.Background(), "StartQueryExecution_nil_error",
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

	driverRows, err := c.QueryContext(context.Background(), "StartQueryExecution_nil_error",
		[]driver.NamedValue{})
	assert.Nil(t, driverRows)
	assert.NotNil(t, err)

	dr, er := c.ExecContext(context.Background(), "StartQueryExecution_nil_error",
		[]driver.NamedValue{})
	assert.Nil(t, dr)
	assert.NotNil(t, er)

	driverRows, err = c.QueryContext(context.Background(), "StartQueryExecution_OK_GetQueryExecutionWithContext_QueryExecutionStateCancelled",
		[]driver.NamedValue{})
	assert.Nil(t, driverRows)
	assert.Equal(t, context.Canceled, err)

	driverRows, err = c.QueryContext(context.Background(), "StartQueryExecution_OK_GetQueryExecutionWithContext_QueryExecutionStateFailed",
		[]driver.NamedValue{})
	assert.Nil(t, driverRows)
	assert.Equal(t, ErrTestMockFailedByAthena, err)

	query := "SELECTExecContext_OK"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, er)
	assert.NotNil(t, dr)

	query = "SELECTQueryContext_OK"
	driverRows, err = c.QueryContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, err)
	assert.NotNil(t, driverRows)

	query = "SELECTQueryContext_OK"
	value := driver.NamedValue{Value: uint64(0)}
	driverRows, err = c.QueryContext(context.Background(), query, []driver.NamedValue{value})
	assert.NotNil(t, err)
	assert.Nil(t, driverRows)

	query = "SELECTQueryContext_?"
	value = driver.NamedValue{Value: "OK"}
	driverRows, err = c.QueryContext(context.Background(), query, []driver.NamedValue{value})
	assert.Nil(t, err)
	assert.NotNil(t, driverRows)

	query = "SELECTQueryContext_CANCEL_OK"
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	driverRows, err = c.QueryContext(ctx, query, []driver.NamedValue{})
	assert.NotNil(t, err)
	assert.Nil(t, driverRows)

	query = "SELECTQueryContext_CANCEL_FAIL"
	ctx, cancel = context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	driverRows, err = c.QueryContext(ctx, query, []driver.NamedValue{})
	assert.NotNil(t, err)
	assert.Nil(t, driverRows)

	// Query stays Queued; a 1-second service limit trips the timeout on
	// the first poll.
	c.connector.config.ServiceLimit = &ServiceLimitOverride{DDLQueryTimeout: 1, DMLQueryTimeout: 1}
	query = "SELECTQueryContext_TIMEOUT"
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
	query = "SELECTQueryContext_AWS_CANCEL"
	driverRows, err = c.QueryContext(context.Background(), query, []driver.NamedValue{})
	assert.NotNil(t, err)
	assert.Nil(t, driverRows)

	// failed by AWS Athena
	query = "SELECTQueryContext_AWS_FAIL"
	driverRows, err = c.QueryContext(context.Background(), query, []driver.NamedValue{})
	assert.NotNil(t, err)
	assert.Nil(t, driverRows)

	query = "SELECTQueryContext_CANCEL_OK"
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
	query := "SELECTExecContext_OK"
	dr, er := c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, er)
	assert.NotNil(t, dr)

	query = "00000000-0000-0000-0000-000000000000"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, er)
	assert.NotNil(t, dr)

	query = "SELECTQueryContext_CANCEL_OK"
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	dr2, err := c.QueryContext(ctx, query, []driver.NamedValue{})
	assert.NotNil(t, err)
	assert.Nil(t, dr2)

	query = "SELECTQueryContext_AWS_CANCEL"
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

	query = "pc:get_query_id FAILED_AFTER_GETQID"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.Equal(t, "FAILED_AFTER_GETQID_FAILED", er.Error())
	assert.Nil(t, dr)

	query = "pc:get_query_id FAILED_AFTER_GETQID2"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.Nil(t, er)
	assert.NotNil(t, dr)

	query = "pc:badcommand"
	dr, er = c.ExecContext(context.Background(), query, []driver.NamedValue{})
	assert.NotNil(t, er)
	assert.Nil(t, dr)

	query = "pc:get_query_id SELECTQueryContext_CANCEL_OK"
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
}
