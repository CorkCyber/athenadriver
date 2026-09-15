// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"testing"
	"time"

	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
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

// TestSQLConnector_Connect_DefaultChain: no profile, no static credentials
// -> must still resolve via config.LoadDefaultConfig (IMDS/container/SSO/
// shared config), never a credential-less aws.Config.
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

// Credentials that come only from the environment must go through
// config.LoadDefaultConfig's own env resolution (refreshable, one link in
// the full chain), not get snapshotted into a static provider.
func TestSQLConnector_EnvOnlyCredentials_UseDefaultChain(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "env-id")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")

	testConf := NewNoOpsConfig()
	_ = testConf.SetRegion("ap-southeast-1")
	connector := &SQLConnector{config: testConf}

	awsCfg, err := connector.resolveAWSConfig(context.Background())
	assert.Nil(t, err)
	creds, err := awsCfg.Credentials.Retrieve(context.Background())
	assert.Nil(t, err)
	assert.Equal(t, "env-id", creds.AccessKeyID)
	assert.NotEqual(t, credentials.StaticCredentialsName, creds.Source,
		"env-var credentials must not be short-circuited into a static provider")
}

// Every credential path must be built by LoadDefaultConfig, so env-driven
// SDK settings apply regardless of which one is taken.
func TestSQLConnector_StaticCredentials_HonorEnvSDKSettings(t *testing.T) {
	t.Setenv("AWS_ENDPOINT_URL", "https://athena.example.internal")
	t.Setenv("AWS_MAX_ATTEMPTS", "7")

	testConf := NewNoOpsConfig()
	_ = testConf.SetRegion("ap-southeast-1")
	_ = testConf.SetAccessID("dsn-id")
	_ = testConf.SetSecretAccessKey("dsn-secret")
	connector := &SQLConnector{config: testConf}

	awsCfg, err := connector.resolveAWSConfig(context.Background())
	assert.Nil(t, err)
	assert.Equal(t, "https://athena.example.internal", aws.ToString(awsCfg.BaseEndpoint))
	assert.Equal(t, 7, awsCfg.RetryMaxAttempts)

	creds, err := awsCfg.Credentials.Retrieve(context.Background())
	assert.Nil(t, err)
	assert.Equal(t, "dsn-id", creds.AccessKeyID)
}

// DummyAccessID/DummySecretAccessKey are documented sentinels meaning "no
// DSN credentials": they must be a no-op that lets the default chain
// (env/IMDS/IRSA/SSO/profile) resolve, not a static provider that disables it.
func TestSQLConnector_DummyCredentials_UseDefaultChain(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "env-id")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")

	testConf := NewNoOpsConfig()
	_ = testConf.SetRegion("ap-southeast-1")
	_ = testConf.SetAccessID(DummyAccessID)
	_ = testConf.SetSecretAccessKey(DummySecretAccessKey)
	connector := &SQLConnector{config: testConf}

	awsCfg, err := connector.resolveAWSConfig(context.Background())
	assert.Nil(t, err)
	creds, err := awsCfg.Credentials.Retrieve(context.Background())
	assert.Nil(t, err)
	assert.Equal(t, "env-id", creds.AccessKeyID,
		"the dummy sentinel must not shadow the default credential chain")
	assert.NotEqual(t, credentials.StaticCredentialsName, creds.Source)
}

// The web-identity branch must build its STS client from the
// LoadDefaultConfig result, not a bare aws.Config{Region:...}, or every
// env-driven SDK setting is dropped for the AssumeRoleWithWebIdentity call.
func TestSQLConnector_WebIdentity_HonorEnvSDKSettings(t *testing.T) {
	noDSNCredentials(t)
	t.Setenv("AWS_MAX_ATTEMPTS", "7")

	cfg := NewNoOpsConfig()
	_ = cfg.SetRegion("ap-southeast-1")
	cfg.WebIdentityRoleARN = "arn:aws:iam::123456789012:role/athena-reader"
	cfg.WebIdentityTokenFile = writeTokenFile(t)

	awsCfg, err := NewConnector(cfg).resolveAWSConfig(context.Background())
	assert.Nil(t, err)
	// The returned config IS the one the STS client was built from, so a
	// non-zero RetryMaxAttempts proves the branch no longer hand-rolls
	// aws.Config{Region:...} (which would leave this at 0).
	assert.Equal(t, 7, awsCfg.RetryMaxAttempts)
	assert.Equal(t, 7, sts.NewFromConfig(awsCfg).Options().RetryMaxAttempts)

	cache, ok := awsCfg.Credentials.(*aws.CredentialsCache)
	assert.True(t, ok)
	assert.True(t, cache.IsCredentialsProvider(&stscreds.WebIdentityRoleProvider{}))
}

func TestSQLConnector_Driver(t *testing.T) {
	testConf := NewNoOpsConfig()
	connector := &SQLConnector{
		config: testConf,
	}
	assert.NotNil(t, connector.Driver())
}

// writeTokenFile drops a throwaway web-identity token on disk. The provider
// is lazy, so the contents only have to exist, never be valid.
func writeTokenFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte("not-a-real-jwt"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestSQLConnector_WebIdentity is the IRSA/EKS branch: all three DSN keys
// present must produce an AssumeRoleWithWebIdentity provider. No network
// call happens; the provider only talks to STS on first Retrieve.
func TestSQLConnector_WebIdentity(t *testing.T) {
	noDSNCredentials(t)
	token := writeTokenFile(t)
	dsn := "s3://fake-bucket/?region=ap-southeast-1" +
		"&webIdentityRoleARN=arn:aws:iam::123456789012:role/athena-reader" +
		"&webIdentityTokenFile=" + token +
		"&webIdentityRoleSessionName=athenadriver-test"
	cfg, err := NewConfig(dsn)
	assert.Nil(t, err)
	assert.Equal(t, "arn:aws:iam::123456789012:role/athena-reader", cfg.WebIdentityRoleARN)
	assert.Equal(t, token, cfg.WebIdentityTokenFile)
	assert.Equal(t, "athenadriver-test", cfg.WebIdentityRoleSessionName)

	awsCfg, err := NewConnector(cfg).resolveAWSConfig(context.Background())
	assert.Nil(t, err)
	assert.Equal(t, "ap-southeast-1", awsCfg.Region)

	cache, ok := awsCfg.Credentials.(*aws.CredentialsCache)
	assert.True(t, ok, "web-identity credentials must be cached, not re-assumed per call")
	assert.True(t, cache.IsCredentialsProvider(&stscreds.WebIdentityRoleProvider{}),
		"the web identity branch was not taken")
}

// A half-configured web identity (role ARN but no token file, or the
// reverse) must fall through to the standard SDK chain rather than build a
// provider that can never work.
func TestSQLConnector_WebIdentity_PartialConfigIgnored(t *testing.T) {
	cases := []struct{ name, arn, tokenFile string }{
		{"ARN only", "arn:aws:iam::123456789012:role/athena-reader", ""},
		{"token file only", "", "/does/not/matter"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			noDSNCredentials(t)
			cfg := NewNoOpsConfig()
			_ = cfg.SetRegion("ap-southeast-1")
			cfg.WebIdentityRoleARN = tc.arn
			cfg.WebIdentityTokenFile = tc.tokenFile

			awsCfg, err := NewConnector(cfg).resolveAWSConfig(context.Background())
			assert.Nil(t, err)
			if cache, ok := awsCfg.Credentials.(*aws.CredentialsCache); ok {
				assert.False(t, cache.IsCredentialsProvider(&stscreds.WebIdentityRoleProvider{}),
					"a half-configured web identity must not be used")
			}
		})
	}
}

// An explicit AWSProfile wins over web identity: the profile branch comes
// first, so LoadDefaultConfig (not stscreds) resolves the credentials.
func TestSQLConnector_WebIdentity_ProfileWins(t *testing.T) {
	noDSNCredentials(t)
	cfg := NewNoOpsConfig()
	_ = cfg.SetRegion("ap-southeast-1")
	cfg.AWSProfile = "no-such-profile"
	cfg.WebIdentityRoleARN = "arn:aws:iam::123456789012:role/athena-reader"
	cfg.WebIdentityTokenFile = writeTokenFile(t)

	_, err := NewConnector(cfg).resolveAWSConfig(context.Background())
	assert.NotNil(t, err, "the nonexistent shared profile must still be what fails")
}

func TestAthenaServerAddress(t *testing.T) {
	baseEndpoint := "https://athena.localstack:4566"
	cases := []struct {
		name string
		cfg  aws.Config
		want string
	}{
		{"standard region", aws.Config{Region: "us-west-2"}, "athena.us-west-2.amazonaws.com"},
		{"china region", aws.Config{Region: "cn-north-1"}, "athena.cn-north-1.amazonaws.com.cn"},
		{"empty region", aws.Config{}, ""},
		{"custom endpoint overrides region", aws.Config{Region: "us-west-2", BaseEndpoint: &baseEndpoint}, "athena.localstack:4566"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, athenaServerAddress(tc.cfg))
		})
	}
}

func TestSQLConnector_ServerAddress_EmptyBeforeConnect(t *testing.T) {
	assert.Equal(t, "", NewConnector(NewNoOpsConfig()).serverAddress())
}
