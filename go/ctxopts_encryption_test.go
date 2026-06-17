// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"testing"

	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	"github.com/stretchr/testify/assert"
)

func TestWithResultEncryption_SSE_S3(t *testing.T) {
	ctx := WithResultEncryption(context.Background(), athenatypes.EncryptionOptionSseS3, "")
	enc := resultEncryptionFromContext(ctx)
	assert.NotNil(t, enc)
	assert.Equal(t, athenatypes.EncryptionOptionSseS3, enc.EncryptionOption)
	assert.Nil(t, enc.KmsKey, "SSE-S3 should not carry a KMS key")
}

func TestWithResultEncryption_SSE_KMS(t *testing.T) {
	const key = "arn:aws:kms:us-east-1:111122223333:key/abc"
	ctx := WithResultEncryption(context.Background(), athenatypes.EncryptionOptionSseKms, key)
	enc := resultEncryptionFromContext(ctx)
	assert.NotNil(t, enc)
	assert.Equal(t, athenatypes.EncryptionOptionSseKms, enc.EncryptionOption)
	assert.NotNil(t, enc.KmsKey)
	assert.Equal(t, key, *enc.KmsKey)
}

func TestResultEncryptionFromContext_MissingKey(t *testing.T) {
	assert.Nil(t, resultEncryptionFromContext(context.Background()))
}
