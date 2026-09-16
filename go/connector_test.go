// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"testing"
	"time"

	"io"
	"log/slog"

	"github.com/stretchr/testify/assert"
)

func TestSQLConnector(t *testing.T) {
	testConf := NewNoOpsConfig()
	connector := &SQLConnector{
		config: testConf,
	}

	conn, err := connector.Connect(context.Background())
	assert.Nil(t, err)
	prepStatement, err := conn.Prepare("select 123")
	assert.Nil(t, err)
	assert.NotNil(t, prepStatement)
	assert.Nil(t, conn.Close())
	transaction, err := conn.Begin()
	assert.Nil(t, transaction)
	assert.Equal(t, "Athena doesn't support transaction statements", err.Error())
}

func TestSQLConnector_Connect(t *testing.T) {
	testConf := NewNoOpsConfig()
	connector := &SQLConnector{
		config: testConf,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, LoggerKey, logger)
	ctx = context.WithValue(ctx, MetricsKey, NoopScope)
	conn, err := connector.Connect(ctx)
	assert.Nil(t, err)
	prepStatement, err := conn.Prepare("select 123")
	assert.Nil(t, err)
	assert.NotNil(t, prepStatement)
	assert.Nil(t, conn.Close())
	transaction, err := conn.Begin()
	assert.Nil(t, transaction)
	assert.Equal(t, "Athena doesn't support transaction statements", err.Error())
}

// noDSNCredentials clears the credential env vars so the connector under
// test falls through to config.LoadDefaultConfig instead of the DSN/env
// static-credentials branch.
func noDSNCredentials(t *testing.T) {
	t.Helper()
	for _, k := range []string{"AWS_ACCESS_KEY_ID", "AWS_ACCESS_KEY",
		"AWS_SECRET_ACCESS_KEY", "AWS_SECRET_KEY", "AWS_SESSION_TOKEN"} {
		t.Setenv(k, "")
	}
}

// TestSQLConnector_Connect_DefaultChain is the regression guard for the
// v1-era AWS_SDK_LOAD_CONFIG gate: with no profile and no static
// credentials the connector must still resolve through
// config.LoadDefaultConfig, which always yields a non-nil credentials
// provider (IMDS / container creds / SSO / shared config), never a
// credential-less aws.Config.
func TestSQLConnector_Connect_DefaultChain(t *testing.T) {
	testConf := NewNoOpsConfig()
	_ = testConf.SetRegion("ap-southeast-1")
	noDSNCredentials(t)
	connector := &SQLConnector{config: testConf}

	awsCfg, err := connector.resolveAWSConfig(context.Background())
	assert.Nil(t, err)
	assert.NotNil(t, awsCfg.Credentials)

	conn, err := connector.Connect(context.Background())
	assert.Nil(t, err)
	assert.NotNil(t, conn)
}

func TestSQLConnector_Connect_AWSProfile_Set(t *testing.T) {
	testConf := NewNoOpsConfig()
	_ = testConf.SetRegion("ap-southeast-1")
	testConf.AWSProfile = "hello-profile"
	connector := &SQLConnector{
		config: testConf,
	}
	conn, err := connector.Connect(context.Background())

	// In aws-sdk-go-v2 you cannot load a nonexistent profile
	assert.NotNil(t, err)
	assert.Nil(t, conn)

	// The cached resolution failure is replayed, not retried into a
	// half-built connector.
	conn, err2 := connector.Connect(context.Background())
	assert.Equal(t, err, err2)
	assert.Nil(t, conn)
}

// TestSQLConnector_Connect_SharesClient pins the pooling fix: every
// connection a connector hands out must reuse one *athena.Client, not
// build its own credential chain and HTTP transport.
func TestSQLConnector_Connect_SharesClient(t *testing.T) {
	testConf := NewNoOpsConfig()
	_ = testConf.SetRegion("ap-southeast-1")
	connector := &SQLConnector{config: testConf}

	c1, err := connector.Connect(context.Background())
	assert.Nil(t, err)
	c2, err := connector.Connect(context.Background())
	assert.Nil(t, err)
	assert.Same(t, c1.(*Connection).athenaClient, c2.(*Connection).athenaClient)
}

func TestSQLConnector_Connect_NewSession_Credentials(t *testing.T) {
	testConf := NewNoOpsConfig()
	_ = testConf.SetRegion("ap-southeast-1")
	_ = testConf.SetAccessID("testid")
	_ = testConf.SetSecretAccessKey("testkey")
	connector := &SQLConnector{
		config: testConf,
	}

	conn, err := connector.Connect(context.Background())

	assert.Nil(t, err)
	assert.NotNil(t, conn)
}

func TestSQLConnector_Driver(t *testing.T) {
	testConf := NewNoOpsConfig()
	connector := &SQLConnector{
		config: testConf,
	}
	assert.NotNil(t, connector.Driver())
}
