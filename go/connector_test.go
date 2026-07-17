// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"os"
	"testing"
	"time"

	"io"
	"log/slog"

	"github.com/stretchr/testify/assert"
	"github.com/uber-go/tally/v4"
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
	ctx = context.WithValue(ctx, MetricsKey, tally.NoopScope)
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

func TestSQLConnector_Connect_NewSessionFail(t *testing.T) {
	testConf := NewNoOpsConfig()
	_ = testConf.SetRegion("ap-southeast-1")
	os.Setenv("AWS_SDK_LOAD_CONFIG", "1")
	os.Setenv("AWS_STS_REGIONAL_ENDPOINTS", "123")
	connector := &SQLConnector{
		config: testConf,
	}
	conn, err := connector.Connect(context.Background())
	tx, err := conn.Begin()

	os.Unsetenv("AWS_SDK_LOAD_CONFIG")
	os.Unsetenv("AWS_STS_REGIONAL_ENDPOINTS")
	assert.NotNil(t, err)
	assert.Nil(t, tx)
}

func TestSQLConnector_Connect_NewSession_AWS_SDK_LOAD_CONFIG_true(t *testing.T) {
	testConf := NewNoOpsConfig()
	_ = testConf.SetRegion("ap-southeast-1")
	os.Setenv("AWS_SDK_LOAD_CONFIG", "true")
	connector := &SQLConnector{
		config: testConf,
	}
	conn, err := connector.Connect(context.Background())

	os.Unsetenv("AWS_SDK_LOAD_CONFIG")
	assert.Nil(t, err)
	assert.NotNil(t, conn)
}

func TestSQLConnector_Connect_NewSession_AWS_SDK_LOAD_CONFIG_true_AWSProfile_Set(t *testing.T) {
	testConf := NewNoOpsConfig()
	_ = testConf.SetRegion("ap-southeast-1")
	testConf.AWSProfile = "hello-profile"
	os.Setenv("AWS_SDK_LOAD_CONFIG", "true")
	connector := &SQLConnector{
		config: testConf,
	}
	conn, err := connector.Connect(context.Background())

	os.Unsetenv("AWS_SDK_LOAD_CONFIG")
	// In aws-sdk-go-v2 you cannot load a nonexistent profile
	assert.NotNil(t, err)
	assert.Nil(t, conn)
}

func TestSQLConnector_Connect_NewSession_AWS_SDK_LOAD_CONFIG_false(t *testing.T) {
	testConf := NewNoOpsConfig()
	_ = testConf.SetRegion("ap-southeast-1")
	os.Setenv("AWS_SDK_LOAD_CONFIG", "0")
	connector := &SQLConnector{
		config: testConf,
	}
	conn, err := connector.Connect(context.Background())

	os.Unsetenv("AWS_SDK_LOAD_CONFIG")
	assert.Nil(t, err)
	assert.NotNil(t, conn)
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
