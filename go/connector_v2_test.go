// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/stretchr/testify/assert"
)

func TestNewConnector(t *testing.T) {
	cfg := NewNoOpsConfig()
	c := NewConnector(cfg)
	assert.NotNil(t, c)
	assert.Same(t, cfg, c.config)
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
