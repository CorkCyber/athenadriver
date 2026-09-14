// SPDX-License-Identifier: MIT

// Tests for the v2.0.0 pre-release adversarial-review fixes: statement-
// aware ExecutionParameters routing, structured QueryFailureError,
// Trino boolean literals, Config freeze-on-NewConnector, DSN round-trip
// of default-true booleans, and shape-dispatched time parsing.

package athenadriver

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	"github.com/stretchr/testify/assert"
)

func TestExecutionParamsSupported(t *testing.T) {
	supported := []string{
		"SELECT * FROM t WHERE a = ?",
		"  select 1",
		"INSERT INTO t VALUES (?)",
		"WITH x AS (SELECT ?) SELECT * FROM x",
		"UNLOAD (SELECT ?) TO 's3://b/p'",
		"CREATE TABLE t2 AS SELECT * FROM t WHERE a = ?",
		"create table t2 with (format='PARQUET') as select ?",
	}
	unsupported := []string{
		"CREATE TABLE t (a int)",
		"CREATE EXTERNAL TABLE t (a int) LOCATION 's3://b/'",
		"ALTER TABLE t SET LOCATION ?",
		"DROP TABLE t",
		"MSCK REPAIR TABLE t",
		"DESCRIBE t",
		"",
	}
	for _, q := range supported {
		assert.True(t, executionParamsSupported(q), q)
	}
	for _, q := range unsupported {
		assert.False(t, executionParamsSupported(q), q)
	}
}

// TestQueryContext_ParamRouting asserts the driver submits placeholder
// queries via ExecutionParameters only for statement shapes Athena
// accepts them on, and falls back to client-side interpolation for DDL.
func TestQueryContext_ParamRouting(t *testing.T) {
	c := newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
		cfg.WorkGroup = nil // skip remote workgroup resolution
	})
	nm := c.athenaClient.(*mockAthenaClient)

	// DML keeps placeholders + ExecutionParameters.
	_, err := c.QueryContext(context.Background(), "select ?",
		[]driver.NamedValue{{Value: int64(1)}})
	assert.Nil(t, err)
	assert.Equal(t, "select ?", *nm.lastStartInput.QueryString)
	assert.Equal(t, []string{"1"}, nm.lastStartInput.ExecutionParameters)

	// DDL is interpolated client-side and submitted parameterless:
	// Athena rejects ExecutionParameters on DDL statements.
	_, err = c.QueryContext(context.Background(), "ALTER TABLE t SET LOCATION ?",
		[]driver.NamedValue{{Value: "x"}})
	assert.Nil(t, err)
	assert.Equal(t, "ALTER TABLE t SET LOCATION 'x'", *nm.lastStartInput.QueryString)
	assert.Nil(t, nm.lastStartInput.ExecutionParameters)
}

// TestQueryContext_StructuredFailure asserts a FAILED query surfaces
// Athena's structured error info via errors.As.
func TestQueryContext_StructuredFailure(t *testing.T) {
	c := newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
		cfg.WorkGroup = nil
	})
	_, err := c.QueryContext(context.Background(),
		"SELECTQueryContext_AWS_FAIL_STRUCTURED", []driver.NamedValue{})
	assert.NotNil(t, err)

	var qErr *QueryFailureError
	assert.True(t, errors.As(err, &qErr))
	assert.Equal(t, int32(2), qErr.ErrorCategory)
	assert.Equal(t, int32(1001), qErr.ErrorType)
	assert.True(t, qErr.Retryable)
	// No StateChangeReason on this fixture, so message falls back to the
	// AthenaError message.
	assert.Equal(t, "SYNTAX_ERROR: line 1:8", qErr.Message)
	assert.Equal(t, qErr.Message, err.Error())
}

func TestConfig_CloneIsolation(t *testing.T) {
	orig := NewNoOpsConfig()
	orig.SetMaskedColumnValue("ssn", "xxx")
	orig.ServiceLimit = &ServiceLimitOverride{DMLQueryTimeout: 100}
	kms := "arn:aws:kms:us-east-1:111:key/abc"
	orig.ResultEncryption = &athenatypes.EncryptionConfiguration{
		EncryptionOption: athenatypes.EncryptionOptionSseKms,
		KmsKey:           &kms,
	}
	_ = orig.SetWorkGroup(NewWG("wg1", nil, nil))

	dup := orig.clone()
	assert.Equal(t, orig, dup)

	// Mutations of the original must not leak into the clone.
	orig.SetMaskedColumnValue("password", "yyy")
	orig.ServiceLimit.DMLQueryTimeout = 999
	orig.WorkGroup = nil
	_, hasPassword := dup.CheckColumnMasked("password")
	assert.False(t, hasPassword)
	assert.Equal(t, 100, dup.ServiceLimit.DMLQueryTimeout)
	assert.Equal(t, "wg1", dup.WorkGroup.Name)
}

// TestConfig_RoundTrip_DefaultTrueBooleans pins the DSN behavior of the
// booleans that default to true: absent key parses true, explicit false
// survives Stringify → NewConfig.
func TestConfig_RoundTrip_DefaultTrueBooleans(t *testing.T) {
	c, err := NewConfig("s3://b/?region=us-east-1")
	assert.Nil(t, err)
	assert.True(t, c.MissingAsEmptyString)
	assert.True(t, c.MetricsEnabled)
	assert.True(t, c.LoggingEnabled)
	assert.True(t, c.WGRemoteCreation)

	c.MissingAsEmptyString = false
	c.MetricsEnabled = false
	c2, err := NewConfig(c.Stringify())
	assert.Nil(t, err)
	assert.False(t, c2.MissingAsEmptyString)
	assert.False(t, c2.MetricsEnabled)
}

// TestConfig_RoundTrip_BackoffMultiplierOne pins that an explicit 1.0
// ("disable backoff") survives the DSN round-trip instead of silently
// reverting to the package default.
func TestConfig_RoundTrip_BackoffMultiplierOne(t *testing.T) {
	c, err := NewConfig("s3://b/?region=us-east-1")
	assert.Nil(t, err)
	c.ResultPollBackoffMultiplier = 1.0
	c2, err := NewConfig(c.Stringify())
	assert.Nil(t, err)
	assert.Equal(t, 1.0, c2.PollBackoffMultiplier())
}

// The former TestLikelyTimeLayout asserted the shape-sniffer's guesses;
// the sniffer is gone (parseAthenaTimeIn sweeps an ordered layout list
// directly). Its parse coverage lives in
// TestDateTime_ScanTimeFractionWidths in datetime_test.go.

// TestScanTime_NoFractionFast pins the perf fix: the most common Athena
// timestamp shape (no fractional seconds) must parse on the first,
// shape-dispatched attempt, matching the fallback sweep.
func TestScanTime_NoFractionFast(t *testing.T) {
	at, err := scanTime("2024-06-30 23:59:59")
	assert.Nil(t, err)
	assert.Equal(t,
		time.Date(2024, 6, 30, 23, 59, 59, 0, time.Local), at.Time)
}
