// SPDX-License-Identifier: MIT

package athenadriver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Config.String is the fmt.Stringer entry point; it must match Stringify
// across every meaningful Config shape.
func TestConfig_String_MatchesStringify(t *testing.T) {
	t.Run("noops", func(t *testing.T) {
		cfg := NewNoOpsConfig()
		assert.Equal(t, cfg.Stringify(), cfg.String())
	})
	t.Run("credentials", func(t *testing.T) {
		cfg, err := NewDefaultConfig("s3://out/bucket/", "us-east-1", "id", "sec")
		assert.NoError(t, err)
		assert.Equal(t, cfg.Stringify(), cfg.String())
	})
	t.Run("with_path", func(t *testing.T) {
		cfg, err := NewDefaultConfig("s3://out/bucket/nested/prefix/", "us-east-1", "id", "sec")
		assert.NoError(t, err)
		assert.Equal(t, cfg.Stringify(), cfg.String())
	})
	t.Run("with_workgroup", func(t *testing.T) {
		cfg, err := NewDefaultConfig("s3://out/", "us-west-2", "id", "sec")
		assert.NoError(t, err)
		wg := NewWG("primary", nil, nil)
		_ = cfg.SetWorkGroup(wg)
		assert.Equal(t, cfg.Stringify(), cfg.String())
	})
	t.Run("wg_remote_creation_false", func(t *testing.T) {
		cfg, err := NewDefaultConfig("s3://out/", "us-west-2", "id", "sec")
		assert.NoError(t, err)
		cfg.WGRemoteCreation = false
		assert.Equal(t, cfg.Stringify(), cfg.String())
	})
	t.Run("aws_profile", func(t *testing.T) {
		cfg, err := NewDefaultConfig("s3://out/", "us-west-2", "id", "sec")
		assert.NoError(t, err)
		cfg.AWSProfile = "dev-sso"
		assert.Equal(t, cfg.Stringify(), cfg.String())
	})
	t.Run("catalog_override", func(t *testing.T) {
		cfg, err := NewDefaultConfig("s3://out/", "us-west-2", "id", "sec")
		assert.NoError(t, err)
		cfg.Catalog = "AwsGlueCatalog"
		assert.Equal(t, cfg.Stringify(), cfg.String())
	})
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
