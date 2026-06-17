// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWithResultReuse_ZeroIsNoOp(t *testing.T) {
	ctx := WithResultReuse(context.Background(), 0)
	assert.Nil(t, resultReuseFromContext(ctx))

	ctx = WithResultReuse(context.Background(), -5*time.Minute)
	assert.Nil(t, resultReuseFromContext(ctx))
}

func TestWithResultReuse_ClampToMax(t *testing.T) {
	ctx := WithResultReuse(context.Background(), 24*time.Hour)
	cfg := resultReuseFromContext(ctx)
	assert.NotNil(t, cfg)
	assert.NotNil(t, cfg.ResultReuseByAgeConfiguration)
	assert.True(t, cfg.ResultReuseByAgeConfiguration.Enabled)
	assert.Equal(t, int32(60), *cfg.ResultReuseByAgeConfiguration.MaxAgeInMinutes)
}

func TestWithResultReuse_ClampToMin(t *testing.T) {
	ctx := WithResultReuse(context.Background(), 10*time.Second)
	cfg := resultReuseFromContext(ctx)
	assert.NotNil(t, cfg)
	assert.Equal(t, int32(1), *cfg.ResultReuseByAgeConfiguration.MaxAgeInMinutes)
}

func TestWithResultReuse_PassThrough(t *testing.T) {
	ctx := WithResultReuse(context.Background(), 15*time.Minute)
	cfg := resultReuseFromContext(ctx)
	assert.Equal(t, int32(15), *cfg.ResultReuseByAgeConfiguration.MaxAgeInMinutes)
}

func TestResultReuseFromContext_MissingKey(t *testing.T) {
	assert.Nil(t, resultReuseFromContext(context.Background()))
}
