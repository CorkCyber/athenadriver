// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql/driver"
	"os"
	"strconv"
	"time"

	"log/slog"

	"github.com/uber-go/tally/v4"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// SQLConnector is the connector for AWS Athena Driver.
type SQLConnector struct {
	config *Config
	// awsConfig is an optional caller-supplied aws.Config used verbatim
	// instead of the DSN-driven credential resolution. Lets callers wire
	// up IMDS/IRSA/SSO/OIDC/assume-role chains, custom retryers, or any
	// other aws-sdk-go-v2 configuration the driver does not surface
	// individually. nil means "fall back to DSN-driven resolution".
	awsConfig *aws.Config
}

// NewConnector returns a SQLConnector for the given DSN-parsed Config.
// The Config is copied at construction: mutating cfg afterwards does not
// affect (or race with) connections the connector produces. Callers that
// need to drive credential resolution themselves should chain
// WithAWSConfig.
func NewConnector(cfg *Config) *SQLConnector {
	return &SQLConnector{config: cfg.clone()}
}

// WithAWSConfig injects a caller-built aws.Config. When set, Connect uses
// it verbatim instead of synthesizing one from DSN keys. Returns the same
// connector for chaining.
func (c *SQLConnector) WithAWSConfig(awsCfg aws.Config) *SQLConnector {
	c.awsConfig = &awsCfg
	return c
}

// resolveAWSConfig returns the aws.Config the Athena client should use.
// See Connect for the precedence rules.
func (c *SQLConnector) resolveAWSConfig(ctx context.Context) (aws.Config, error) {
	if c.awsConfig != nil {
		return *c.awsConfig, nil
	}
	profile := c.config.AWSProfile
	loadSharedConfig, _ := strconv.ParseBool(os.Getenv("AWS_SDK_LOAD_CONFIG"))
	if profile != "" || loadSharedConfig {
		opts := []func(*config.LoadOptions) error{}
		if profile != "" {
			opts = append(opts, config.WithSharedConfigProfile(profile))
		}
		if region := c.config.RegionOrEnv(); region != "" {
			opts = append(opts, config.WithRegion(region))
		}
		return config.LoadDefaultConfig(ctx, opts...)
	}
	if c.config.WebIdentityRoleARN != "" && c.config.WebIdentityTokenFile != "" {
		base := aws.Config{Region: c.config.RegionOrEnv()}
		stsClient := sts.NewFromConfig(base)
		provider := stscreds.NewWebIdentityRoleProvider(
			stsClient, c.config.WebIdentityRoleARN,
			stscreds.IdentityTokenFile(c.config.WebIdentityTokenFile),
			func(o *stscreds.WebIdentityRoleOptions) {
				if c.config.WebIdentityRoleSessionName != "" {
					o.RoleSessionName = c.config.WebIdentityRoleSessionName
				}
			},
		)
		base.Credentials = aws.NewCredentialsCache(provider)
		return base, nil
	}
	if c.config.AccessIDOrEnv() != "" {
		return aws.Config{
			Region: c.config.RegionOrEnv(),
			Credentials: credentials.NewStaticCredentialsProvider(
				c.config.AccessIDOrEnv(),
				c.config.SecretAccessKeyOrEnv(),
				c.config.SessionTokenOrEnv(),
			),
		}, nil
	}
	return aws.Config{Region: c.config.RegionOrEnv()}, nil
}

// NoopsSQLConnector is to create a noops SQLConnector.
func NoopsSQLConnector() *SQLConnector {
	return &SQLConnector{config: NewNoOpsConfig()}
}

// AthenaClient is an interface to facilitate testing
type AthenaClient interface {
	CreateWorkGroup(context.Context, *athena.CreateWorkGroupInput, ...func(*athena.Options)) (*athena.CreateWorkGroupOutput, error)
	GetQueryExecution(context.Context, *athena.GetQueryExecutionInput, ...func(*athena.Options)) (*athena.GetQueryExecutionOutput, error)
	GetQueryResults(context.Context, *athena.GetQueryResultsInput, ...func(*athena.Options)) (*athena.GetQueryResultsOutput, error)
	GetWorkGroup(context.Context, *athena.GetWorkGroupInput, ...func(*athena.Options)) (*athena.GetWorkGroupOutput, error)
	StartQueryExecution(context.Context, *athena.StartQueryExecutionInput, ...func(options *athena.Options)) (*athena.StartQueryExecutionOutput, error)
	StopQueryExecution(context.Context, *athena.StopQueryExecutionInput, ...func(*athena.Options)) (*athena.StopQueryExecutionOutput, error)
}

// Driver implements driver.Connector. SQLDriver is stateless, so this
// returns a fresh instance rather than a back-reference to whatever
// actually produced c (a connector built directly via NewConnector was
// never produced by any SQLDriver at all).
func (c *SQLConnector) Driver() driver.Driver {
	return &SQLDriver{}
}

// Connect creates the underlying Athena client. Credential resolution
// follows this precedence:
//
//  1. WithAWSConfig — caller-supplied aws.Config used verbatim.
//  2. Config.AWSProfile, or the AWS_SDK_LOAD_CONFIG env var, triggers
//     config.LoadDefaultConfig with the configured shared-config profile.
//     This is the path for ~/.aws/config-driven setups, IRSA / EKS pod
//     identity, SSO, OIDC, and assume-role chains.
//  3. DSN-supplied static access key / secret / session token.
//  4. Region-only aws.Config — relies on the default credential chain
//     (env vars, IMDS, container creds, etc.).
func (c *SQLConnector) Connect(ctx context.Context) (driver.Conn, error) {
	now := time.Now()
	// tracer is per-connection so concurrent Connect() calls do not race
	// mutating a shared field on the connector.
	tracer := NewObservability(c.config, nil, nil)
	if metrics, ok := ctx.Value(MetricsKey).(tally.Scope); ok {
		tracer.SetScope(metrics)
	}
	if logger, ok := ctx.Value(LoggerKey).(*slog.Logger); ok {
		tracer.SetLogger(logger)
	}

	awsCfg, err := c.resolveAWSConfig(ctx)
	if err != nil {
		tracer.Scope().Counter(DriverName + ".failure.sqlconnector.newsession").Inc(1)
		return nil, err
	}
	athenaClient := athena.NewFromConfig(awsCfg)
	timeConnect := time.Since(now)
	conn := &Connection{
		athenaClient: athenaClient,
		connector:    c,
		tracer:       tracer,
	}
	tracer.Scope().Timer(DriverName + ".connector.connect").Record(timeConnect)
	return conn, nil
}
