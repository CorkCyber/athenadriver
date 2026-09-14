// SPDX-License-Identifier: MIT

package athenadriver

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Config.String is the fmt.Stringer entry point; it must match
// SafeStringify (masked credentials) across every meaningful Config shape.
func TestConfig_String_MatchesSafeStringify(t *testing.T) {
	t.Run("noops", func(t *testing.T) {
		cfg := NewNoOpsConfig()
		assert.Equal(t, cfg.SafeStringify(), cfg.String())
	})
	t.Run("credentials", func(t *testing.T) {
		cfg, err := NewDefaultConfig("s3://out/bucket/", "us-east-1", "id", "sec")
		assert.NoError(t, err)
		assert.Equal(t, cfg.SafeStringify(), cfg.String())
	})
	t.Run("with_path", func(t *testing.T) {
		cfg, err := NewDefaultConfig("s3://out/bucket/nested/prefix/", "us-east-1", "id", "sec")
		assert.NoError(t, err)
		assert.Equal(t, cfg.SafeStringify(), cfg.String())
	})
	t.Run("with_workgroup", func(t *testing.T) {
		cfg, err := NewDefaultConfig("s3://out/", "us-west-2", "id", "sec")
		assert.NoError(t, err)
		wg := NewWG("primary", nil, nil)
		_ = cfg.SetWorkGroup(wg)
		assert.Equal(t, cfg.SafeStringify(), cfg.String())
	})
	t.Run("wg_remote_creation_false", func(t *testing.T) {
		cfg, err := NewDefaultConfig("s3://out/", "us-west-2", "id", "sec")
		assert.NoError(t, err)
		cfg.WGRemoteCreation = false
		assert.Equal(t, cfg.SafeStringify(), cfg.String())
	})
	t.Run("aws_profile", func(t *testing.T) {
		cfg, err := NewDefaultConfig("s3://out/", "us-west-2", "id", "sec")
		assert.NoError(t, err)
		cfg.AWSProfile = "dev-sso"
		assert.Equal(t, cfg.SafeStringify(), cfg.String())
	})
	t.Run("catalog_override", func(t *testing.T) {
		cfg, err := NewDefaultConfig("s3://out/", "us-west-2", "id", "sec")
		assert.NoError(t, err)
		cfg.Catalog = "AwsGlueCatalog"
		assert.Equal(t, cfg.SafeStringify(), cfg.String())
	})
}

// String() is what %v reaches for, so it must never carry live secrets;
// Stringify() remains the explicit full serializer.
func TestConfig_String_DoesNotLeakCredentials(t *testing.T) {
	cfg, err := NewDefaultConfig("s3://out/bucket/", "us-east-1", "AKIAEXAMPLEID", "s3cr3t-value")
	assert.NoError(t, err)
	cfg.SessionToken = "sess10n-t0ken"

	s := fmt.Sprintf("%v", cfg)
	assert.NotContains(t, s, "s3cr3t-value")
	assert.NotContains(t, s, "sess10n-t0ken")
	assert.NotContains(t, s, "AKIAEXAMPLEID")
	assert.Contains(t, s, "secretAccessKey=*")
	assert.Contains(t, s, "sessionToken=*")

	raw := cfg.Stringify()
	assert.Contains(t, raw, "s3cr3t-value")
	assert.Contains(t, raw, "sess10n-t0ken")
}

// The typed Config rewrite made DSN integer keys strict: an invalid value
// must fail loudly at NewConfig time instead of silently defaulting.
func TestNewConfig_InvalidIntegerDSNKeys(t *testing.T) {
	base := "s3://out/bucket/?db=x&region=us-east-1&accessID=id&secretAccessKey=sec"
	cases := map[string]string{
		"resultPollIntervalSeconds":    "&resultPollIntervalSeconds=not-a-number",
		"resultPollMaxIntervalSeconds": "&resultPollMaxIntervalSeconds=abc",
		"resultPollBackoffMultiplier":  "&resultPollBackoffMultiplier=xyz",
		"DDLQueryTimeout":              "&DDLQueryTimeout=nope",
		"DMLQueryTimeout":              "&DMLQueryTimeout=nope",
	}
	for name, suffix := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewConfig(base + suffix)
			assert.Error(t, err, "invalid integer for %s should error at NewConfig time", name)
		})
	}
}
