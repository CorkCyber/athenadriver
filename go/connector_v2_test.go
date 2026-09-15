// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql/driver"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
)

func TestNewConnector(t *testing.T) {
	cfg := NewNoOpsConfig()
	c := NewConnector(cfg)
	assert.NotNil(t, c)
	// Config is frozen at construction: an equal copy, not the caller's
	// pointer, so post-construction mutation can't race live connections.
	assert.NotSame(t, cfg, c.config)
	assert.Equal(t, cfg, c.config)
	cfg.SetMaskedColumnValue("password", "xxx")
	_, masked := c.config.CheckColumnMasked("password")
	assert.False(t, masked)
	assert.Nil(t, c.awsConfig)
}

func TestSQLConnector_WithAWSConfig(t *testing.T) {
	cfg := NewNoOpsConfig()
	c := NewConnector(cfg)
	awsCfg := aws.Config{Region: "eu-west-2"}
	ret := c.WithAWSConfig(awsCfg)
	assert.Same(t, c, ret, "WithAWSConfig should return same connector for chaining")
	assert.NotNil(t, c.awsConfig)
	assert.Equal(t, "eu-west-2", c.awsConfig.Region)
}

func TestSQLConnector_WithAWSConfig_Overrides_DSNPath(t *testing.T) {
	cfg := NewNoOpsConfig()
	c := NewConnector(cfg).WithAWSConfig(aws.Config{Region: "us-west-2"})
	got, err := c.resolveAWSConfig(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "us-west-2", got.Region, "injected aws.Config should be used verbatim")
}

// TestSQLConnector_WithTracer_DeterministicAcrossConnectCallers: Connect
// runs with both the caller's ctx and, from connectionOpener, a valueless
// one. WithTracer must apply identically either way.
func TestSQLConnector_WithTracer_DeterministicAcrossConnectCallers(t *testing.T) {
	tracer := &recordingTracer{}
	connector := NoopsSQLConnector().WithTracer(tracer)

	// Simulates the caller-ctx path.
	withValue, err := connector.Connect(context.WithValue(context.Background(), TracerKey, &recordingTracer{}))
	assert.NoError(t, err)
	// Simulates the background connectionOpener path: no values at all.
	withoutValue, err := connector.Connect(context.Background())
	assert.NoError(t, err)

	for _, conn := range []driver.Conn{withValue, withoutValue} {
		c := conn.(*Connection)
		_, span := c.tracer.StartSpan(context.Background(), "athena.query")
		_, isNoop := span.(noopSpan)
		assert.False(t, isNoop, "connector-bound tracer must fire regardless of which ctx built this connection")
		span.End()
	}
}

func TestSQLConnector_WithTracer_ReturnsSameConnectorForChaining(t *testing.T) {
	c := NewConnector(NewNoOpsConfig())
	ret := c.WithTracer(&recordingTracer{})
	assert.Same(t, c, ret)
}
