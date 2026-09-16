// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql/driver"
	"sync"
	"sync/atomic"
	"time"

	"log/slog"

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

	// clientMu/client cache the resolved aws.Config and Athena client
	// across every pooled connection this connector hands out. athena.Client
	// and aws.CredentialsCache are both safe for concurrent use, so one
	// client per connector means one credential refresh timer and one
	// http.Transport (keep-alive/TLS session reuse) for the whole pool
	// instead of N of each. Only success is cached — a transient failure
	// (network blip, IMDS not ready yet) on the first Connect must not
	// poison every later Connect for the connector's lifetime.
	clientMu sync.Mutex
	client   atomic.Pointer[athena.Client]

	// wgVerified records that the configured workgroup has been confirmed
	// to exist and be enabled (or was remote-created). The workgroup is
	// fixed at construction, so only the first query pays for GetWorkGroup.
	// Only success is cached — a transient failure must not poison the
	// connector for its whole lifetime.
	wgVerified atomic.Bool
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
	if profile == "" {
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
	}
	// Default: the full aws-sdk-go-v2 chain. LoadDefaultConfig always reads
	// ~/.aws/config and ~/.aws/credentials and resolves env vars, SSO,
	// container creds / IRSA and IMDS — there is no v1-style
	// AWS_SDK_LOAD_CONFIG gate in v2.
	var opts []func(*config.LoadOptions) error
	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	if region := c.config.RegionOrEnv(); region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	return config.LoadDefaultConfig(ctx, opts...)
}

// sharedClient resolves the aws.Config and builds the Athena client once,
// then hands the same *athena.Client to every connection. athena.Client is
// goroutine-safe and designed to be constructed once and shared. A failed
// resolution is retried on the next call rather than cached, since a
// transient credential/network failure must not permanently break every
// future connection on this connector.
func (c *SQLConnector) sharedClient(ctx context.Context) (*athena.Client, error) {
	if client := c.client.Load(); client != nil {
		return client, nil
	}
	c.clientMu.Lock()
	defer c.clientMu.Unlock()
	if client := c.client.Load(); client != nil {
		return client, nil
	}
	awsCfg, err := c.resolveAWSConfig(ctx)
	if err != nil {
		return nil, err
	}
	client := athena.NewFromConfig(awsCfg)
	c.client.Store(client)
	return client, nil
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

// Connect returns a connection backed by the connector's shared Athena
// client. The client (and the aws.Config behind it) is built on the first
// Connect and reused by every later one, so a connection pool shares one
// credential cache and one HTTP transport.
//
// Credential resolution follows this precedence:
//
//  1. WithAWSConfig — caller-supplied aws.Config used verbatim.
//  2. Config.WebIdentityRoleARN + WebIdentityTokenFile — explicit
//     AssumeRoleWithWebIdentity (only when AWSProfile is unset).
//  3. DSN-supplied static access key / secret / session token (only when
//     AWSProfile is unset).
//  4. config.LoadDefaultConfig — the standard aws-sdk-go-v2 chain:
//     ~/.aws/config and ~/.aws/credentials (honoring Config.AWSProfile
//     when set), env vars, SSO, container creds / IRSA, and IMDS.
func (c *SQLConnector) Connect(ctx context.Context) (driver.Conn, error) {
	now := time.Now()
	// tracer is per-connection so concurrent Connect() calls do not race
	// mutating a shared field on the connector.
	tracer := NewObservability(c.config, nil, nil)
	if metrics, ok := ctx.Value(MetricsKey).(Scope); ok {
		tracer.SetScope(metrics)
	}
	if logger, ok := ctx.Value(LoggerKey).(*slog.Logger); ok {
		tracer.SetLogger(logger)
	}

	athenaClient, err := c.sharedClient(ctx)
	if err != nil {
		tracer.Scope().Counter(DriverName + ".failure.sqlconnector.newsession").Inc(1)
		return nil, err
	}
	timeConnect := time.Since(now)
	conn := &Connection{
		athenaClient: athenaClient,
		connector:    c,
		tracer:       tracer,
	}
	tracer.Scope().Timer(DriverName + ".connector.connect").Record(timeConnect)
	return conn, nil
}
