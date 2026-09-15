// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
)

// newClientRequestToken returns a random UUIDv4 suitable for use as an
// Athena ClientRequestToken (32–128 ASCII chars). Used to make
// StartQueryExecution idempotent against SDK-level retries so a transient
// network blip does not produce a duplicate (and double-charged) query.
func newClientRequestToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read does not fail on supported platforms; fall back
		// to a time-based token rather than returning an error from a code
		// path the caller cannot meaningfully recover from.
		return fmt.Sprintf("athenadriver-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// stringFromContext returns a non-empty string value stored under key on
// ctx, or "" if no such value is set.
func stringFromContext(ctx context.Context, key TContextKey) string {
	if v, ok := ctx.Value(key).(string); ok && v != "" {
		return v
	}
	return ""
}

// WithResultEncryption opts a single query into a specific S3 encryption
// option for its result bytes. Equivalent to setting ResultEncryptionKey on
// the ctx with a fully-built *EncryptionConfiguration.
func WithResultEncryption(ctx context.Context, option athenatypes.EncryptionOption, kmsKey string) context.Context {
	enc := &athenatypes.EncryptionConfiguration{EncryptionOption: option}
	if kmsKey != "" {
		enc.KmsKey = aws.String(kmsKey)
	}
	return context.WithValue(ctx, ResultEncryptionKey, enc)
}

// resultEncryptionFromContext returns the per-query *EncryptionConfiguration
// override from ctx, or nil if none is set.
func resultEncryptionFromContext(ctx context.Context) *athenatypes.EncryptionConfiguration {
	if v, ok := ctx.Value(ResultEncryptionKey).(*athenatypes.EncryptionConfiguration); ok {
		return v
	}
	return nil
}

// WithResultReuse opts a single query into Athena's result-reuse cache for
// up to maxAge. Pass the returned context to db.QueryContext /
// db.ExecContext. maxAge is clamped to the Athena-supported range
// [1 minute, 10080 minutes (7 days)]; passing 0 or a negative value is a
// no-op.
func WithResultReuse(ctx context.Context, maxAge time.Duration) context.Context {
	if maxAge <= 0 {
		return ctx
	}
	return context.WithValue(ctx, ResultReuseMaxAgeKey, maxAge)
}

// resultReuseFromContext returns the ResultReuseConfiguration the caller
// asked for via WithResultReuse / ResultReuseMaxAgeKey, or nil if no reuse
// was requested.
func resultReuseFromContext(ctx context.Context) *athenatypes.ResultReuseConfiguration {
	v, ok := ctx.Value(ResultReuseMaxAgeKey).(time.Duration)
	if !ok || v <= 0 {
		return nil
	}
	minutes := max(int32(v/time.Minute), 1)
	// 10080 (7 days) is Athena's documented maximum for
	// ResultReuseByAgeConfiguration.MaxAgeInMinutes; 60 is merely its default.
	if minutes > 10080 {
		minutes = 10080
	}
	return &athenatypes.ResultReuseConfiguration{
		ResultReuseByAgeConfiguration: &athenatypes.ResultReuseByAgeConfiguration{
			Enabled:         true,
			MaxAgeInMinutes: &minutes,
		},
	}
}
