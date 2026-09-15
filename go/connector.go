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

	// clientOnce caches the resolved aws.Config and Athena client across
	// every pooled connection this connector hands out. athena.Client and
	// aws.CredentialsCache are both safe for concurrent use, so one client
	// per connector means one credential refresh timer and one
	// http.Transport (keep-alive/TLS session reuse) for the whole pool
	// instead of N of each.
	clientOnce retryOnce[*athena.Client]

	// wgOnce records that the configured workgroup has been confirmed to
	// exist and be enabled (or was remote-created). The workgroup is fixed
	// at construction, so only the first query pays for GetWorkGroup — this
	// single-flights the check so a cold-start burst of N pooled
	// connections costs one GetWorkGroup instead of N against Athena's low
	// API quota.
	wgOnce retryOnce[struct{}]
}

// retryOnce lazily computes and shares a value the first time it's needed,
// across every caller — but only caches success. A transient failure (a
// network blip, IMDS not ready yet) is left for the next caller to retry
// instead of poisoning the cache for the connector's whole lifetime.
// sync.OnceValue doesn't fit: it caches a failed attempt just as
// permanently as a successful one.
type retryOnce[T any] struct {
	mu    sync.Mutex
	value atomic.Pointer[T]
}

// do returns the cached value if one exists; otherwise it calls fn under a
// lock (re-checking the cache first, in case another caller just filled it
// while this one was waiting) and caches the result only on success.
func (o *retryOnce[T]) do(fn func() (T, error)) (T, error) {
	if v := o.value.Load(); v != nil {
		return *v, nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if v := o.value.Load(); v != nil {
		return *v, nil
	}
	v, err := fn()
	if err != nil {
		var zero T
		return zero, err
	}
	o.value.Store(&v)
	return v, nil
}

// done reports whether a value has been successfully cached. Test-only
// introspection; production code should call do instead.
func (o *retryOnce[T]) done() bool {
	return o.value.Load() != nil
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
	// Every path goes through LoadDefaultConfig so env-driven SDK settings
	// (AWS_ENDPOINT_URL_ATHENA, AWS_MAX_ATTEMPTS, FIPS, CA bundle, ...) are
	// honored no matter which credential mechanism applies. The branches
	// below only add a credentials provider to the option list.
	var opts []func(*config.LoadOptions) error
	profile := c.config.AWSProfile
	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	if region := c.config.RegionOrEnv(); region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	webIdentity := false
	switch {
	case profile != "":
		// Shared profile wins; LoadDefaultConfig resolves its credentials.
	case c.config.WebIdentityRoleARN != "" && c.config.WebIdentityTokenFile != "":
		// Handled after the base config is loaded: the STS client must be
		// built from that same config, not a bare aws.Config{Region:...}.
		webIdentity = true
	case c.config.AccessID != "" && c.config.AccessID != DummyAccessID:
		// Only DSN-supplied credentials short-circuit the chain. Credentials
		// that merely come from the environment fall through so
		// LoadDefaultConfig resolves (and refreshes) them itself, as does the
		// documented DummyAccessID sentinel.
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(
				c.config.AccessID,
				c.config.SecretAccessKeyOrEnv(),
				c.config.SessionTokenOrEnv(),
			),
		))
	}
	// LoadDefaultConfig always reads ~/.aws/config and ~/.aws/credentials and
	// resolves env vars, SSO, container creds / IRSA and IMDS — there is no
	// v1-style AWS_SDK_LOAD_CONFIG gate in v2.
	base, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil || !webIdentity {
		return base, err
	}
	provider := stscreds.NewWebIdentityRoleProvider(
		sts.NewFromConfig(base), c.config.WebIdentityRoleARN,
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

// sharedClient resolves the aws.Config and builds the Athena client once,
// then hands the same *athena.Client to every connection. athena.Client is
// goroutine-safe and designed to be constructed once and shared.
func (c *SQLConnector) sharedClient(ctx context.Context) (*athena.Client, error) {
	return c.clientOnce.do(func() (*athena.Client, error) {
		awsCfg, err := c.resolveAWSConfig(ctx)
		if err != nil {
			return nil, err
		}
		return athena.NewFromConfig(awsCfg), nil
	})
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
//     Config.AccessID itself is set to something other than DummyAccessID
//     and AWSProfile is unset — environment-only credentials and the
//     DummyAccessID sentinel are left to the SDK chain).
//  4. config.LoadDefaultConfig — the standard aws-sdk-go-v2 chain:
//     ~/.aws/config and ~/.aws/credentials (honoring Config.AWSProfile
//     when set), env vars, SSO, container creds / IRSA, and IMDS.
//
// Cases 2 and 3 are layered onto the LoadDefaultConfig result (case 2's
// STS client is itself built from it), so env-driven SDK settings
// (endpoint URL, retry mode, FIPS, CA bundle) apply identically on every
// path.
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
