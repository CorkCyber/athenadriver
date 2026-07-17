// SPDX-License-Identifier: MIT

package athenadriver

import (
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
)

// Config is the athenadriver Config, a typed view of the DSN passed to
// sql.Open. All fields are public; assign them directly or use the
// validating Set* methods (which return errors on bad input).
//
// Marshal a Config to a DSN with Stringify(); parse one with NewConfig.
// Round-trip is lossless for every field encoded into the DSN.
//
// Construct an empty Config with NewNoOpsConfig (sane defaults, no
// credentials), or with NewDefaultConfig if you want to set bucket /
// region / credentials up front with validation.
type Config struct {
	// Output bucket. Parsed from / serialized as `s3://Host/Path`. Set
	// via SetOutputBucket so the s3:// prefix and split are validated.
	bucketHost string
	bucketPath string

	User string

	DB     string
	Region string

	AccessID        string
	SecretAccessKey string
	SessionToken    string
	AWSProfile      string

	// Catalog defaults to AwsDataCatalog when empty.
	Catalog string

	// Workgroup name / tags / config. Use SetWorkGroup so nil-tags and
	// nil-config are filled with sensible defaults at assignment time.
	WorkGroup        *Workgroup
	WGRemoteCreation bool

	ResultEncryption    *athenatypes.EncryptionConfiguration
	ExpectedBucketOwner string

	// Polling. Zero values mean "use the package defaults" (PoolInterval
	// for initial interval, 1.0 multiplier disables backoff).
	ResultPollInterval          time.Duration
	ResultPollBackoffMultiplier float64
	ResultPollMaxInterval       time.Duration

	MissingAsEmptyString bool
	MissingAsDefault     bool
	MissingAsNil         bool

	MoneyWise      bool
	ReadOnly       bool
	LoggingEnabled bool
	MetricsEnabled bool

	WebIdentityRoleARN         string
	WebIdentityTokenFile       string
	WebIdentityRoleSessionName string

	// ServiceLimit overrides Athena's per-statement query timeouts. nil
	// means "use the DDLQueryTimeout / DMLQueryTimeout package
	// constants".
	ServiceLimit *ServiceLimitOverride

	// MaskedColumns maps a column name to the substitute value the
	// driver should hand database/sql when the column is read.
	MaskedColumns map[string]string
}

var (
	reSecretAccessKey = regexp.MustCompile(`secretAccessKey=[^&]+`)
	reAccessID        = regexp.MustCompile(`accessID=[^&]+`)
	reSessionToken    = regexp.MustCompile(`sessionToken=[^&]+`)
)

var (
	credAccessEnvKey = []string{
		"AWS_ACCESS_KEY_ID",
		"AWS_ACCESS_KEY",
	}
	credSecretEnvKey = []string{
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SECRET_KEY",
	}
	credSessionEnvKey = []string{
		"AWS_SESSION_TOKEN",
	}
	regionEnvKeys = []string{
		"AWS_REGION",
		"AWS_DEFAULT_REGION", // Only read if AWS_SDK_LOAD_CONFIG is also set
	}
)

// NewDefaultConfig builds a Config with the supplied bucket / region /
// credentials and the recommended polling default.
func NewDefaultConfig(outputBucket, region, accessID, secretAccessKey string) (*Config, error) {
	c := NewNoOpsConfig()
	if err := c.SetOutputBucket(outputBucket); err != nil {
		return nil, err
	}
	if err := c.SetRegion(region); err != nil {
		return nil, err
	}
	if err := c.SetAccessID(accessID); err != nil {
		return nil, err
	}
	if err := c.SetSecretAccessKey(secretAccessKey); err != nil {
		return nil, err
	}
	c.ResultPollInterval = time.Duration(PoolInterval) * time.Second
	return c, nil
}

// NewNoOpsConfig builds a Config with no credentials and the defaults
// that match athenadriver's pre-v2 behavior: default DB, default
// region, missing-as-empty-string on, WG remote creation allowed,
// logging and metrics on, AwsDataCatalog catalog.
func NewNoOpsConfig() *Config {
	return &Config{
		DB:                   DefaultDBName,
		Region:               DefaultRegion,
		MissingAsEmptyString: true,
		WGRemoteCreation:     true,
		LoggingEnabled:       true,
		MetricsEnabled:       true,
	}
}

// NewConfig parses a DSN string into a Config. Returns
// ErrConfigInvalidConfig if the scheme is not s3 or the region is
// missing.
func NewConfig(dsn string) (*Config, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "s3" {
		return nil, ErrConfigInvalidConfig
	}
	// Reject "s3:/foo" and similar single-slash forms: url.Parse routes the
	// path into u.Path with Host="", but Stringify can't faithfully emit
	// them (Go's URL formatter would promote the first path segment into
	// the host slot on re-parse). A path of "/" alone is fine — it's the
	// canonical trailing slash on a NoOps DSN with no bucket path.
	if u.Host == "" && strings.TrimLeft(u.Path, "/") != "" {
		return nil, ErrConfigInvalidConfig
	}
	c := &Config{
		bucketHost:     u.Host,
		bucketPath:     strings.TrimLeft(u.Path, "/"),
		LoggingEnabled: true,
	}
	if u.User != nil {
		c.User = u.User.Username()
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, err
	}
	if err := c.fromQuery(q); err != nil {
		return nil, err
	}
	if c.Region == "" {
		return nil, ErrConfigInvalidConfig
	}
	// Round-trip guard: url.Parse is lenient and accepts DSNs whose bucket
	// component (whitespace, `!`, `%`) URL-escapes into forms url.Parse
	// then rejects when re-parsing Stringify's output. Reject those up
	// front so a Config that survives NewConfig can always round-trip.
	if _, err := url.Parse(c.Stringify()); err != nil {
		return nil, ErrConfigInvalidConfig
	}
	return c, nil
}

// fromQuery decodes url.Values produced by Stringify back into typed
// fields. Unknown keys are silently ignored so callers can carry
// custom state through the DSN without confusing the driver.
func (c *Config) fromQuery(q url.Values) error {
	c.DB = orDefault(q.Get("db"), DefaultDBName)
	c.Region = q.Get("region")
	c.AccessID = q.Get("accessID")
	c.SecretAccessKey = q.Get("secretAccessKey")
	c.SessionToken = q.Get("sessionToken")
	c.AWSProfile = q.Get("AWSProfile")
	c.Catalog = q.Get("catalog")
	c.ExpectedBucketOwner = q.Get("expectedBucketOwner")
	c.WebIdentityRoleARN = q.Get("webIdentityRoleARN")
	c.WebIdentityTokenFile = q.Get("webIdentityTokenFile")
	c.WebIdentityRoleSessionName = q.Get("webIdentityRoleSessionName")

	if wgName := q.Get("workgroupName"); wgName != "" {
		c.WorkGroup = NewWG(wgName, nil, parseTags(q.Get("tag")))
	}

	if enc := q.Get("resultEncryptionOption"); enc != "" {
		ec := &athenatypes.EncryptionConfiguration{
			EncryptionOption: athenatypes.EncryptionOption(enc),
		}
		if k := q.Get("resultEncryptionKmsKey"); k != "" {
			ec.KmsKey = aws.String(k)
		}
		c.ResultEncryption = ec
	}

	if v := q.Get("resultPollIntervalSeconds"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return err
		}
		c.ResultPollInterval = time.Duration(n) * time.Second
	}
	if v := q.Get("resultPollBackoffMultiplier"); v != "" {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return err
		}
		c.ResultPollBackoffMultiplier = n
	}
	if v := q.Get("resultPollMaxIntervalSeconds"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return err
		}
		c.ResultPollMaxInterval = time.Duration(n) * time.Second
	}

	// missingAsEmptyString, WGRemoteCreation, LoggingEnabled and
	// MetricsEnabled default to true when the key is absent, matching
	// NewNoOpsConfig; the remaining booleans default to false.
	c.MissingAsEmptyString = q.Get("missingAsEmptyString") != "false"
	c.MissingAsDefault = q.Get("missingAsDefault") == "true"
	c.MissingAsNil = q.Get("missingAsNil") == "true"
	c.MoneyWise = q.Get("MoneyWise") == "true"
	c.ReadOnly = q.Get("ReadOnly") == "true"
	c.WGRemoteCreation = q.Get("WGRemoteCreation") != "false"
	c.LoggingEnabled = q.Get("LoggingEnabled") != "false"
	c.MetricsEnabled = q.Get("MetricsEnabled") != "false"

	if v := q.Get("DDLQueryTimeout"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return err
		}
		if n <= 0 || n > maxQueryTimeoutSeconds {
			return fmt.Errorf("DDLQueryTimeout must be between 1 and %d seconds, got %d", maxQueryTimeoutSeconds, n)
		}
		if c.ServiceLimit == nil {
			c.ServiceLimit = &ServiceLimitOverride{}
		}
		c.ServiceLimit.DDLQueryTimeout = n
	}
	if v := q.Get("DMLQueryTimeout"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return err
		}
		if n <= 0 || n > maxQueryTimeoutSeconds {
			return fmt.Errorf("DMLQueryTimeout must be between 1 and %d seconds, got %d", maxQueryTimeoutSeconds, n)
		}
		if c.ServiceLimit == nil {
			c.ServiceLimit = &ServiceLimitOverride{}
		}
		c.ServiceLimit.DMLQueryTimeout = n
	}

	for k, v := range q {
		if strings.HasPrefix(k, "masked_") && len(v) > 0 {
			if c.MaskedColumns == nil {
				c.MaskedColumns = map[string]string{}
			}
			c.MaskedColumns[strings.TrimPrefix(k, "masked_")] = v[0]
		}
	}
	return nil
}

func orDefault(v, dflt string) string {
	if v != "" {
		return v
	}
	return dflt
}

// parseTags decodes a tag DSN value of the form `|key1`val1|key2`val2`
// (leading pipe, backtick separator) into a *WGTags.
func parseTags(s string) *WGTags {
	t := NewWGTags()
	if s == "" {
		return t
	}
	for _, kv := range strings.Split(strings.TrimPrefix(s, "|"), "|") {
		parts := strings.SplitN(kv, "`", 2)
		if len(parts) == 2 {
			t.AddTag(parts[0], parts[1])
		}
	}
	return t
}

// String returns the DSN. Equivalent to Stringify; provided for
// fmt.Stringer compatibility.
func (c *Config) String() string {
	return c.Stringify()
}

// Stringify serializes the Config back to its DSN form. The output is
// deterministic (query keys sorted alphabetically by url.Values.Encode)
// and round-trips through NewConfig.
func (c *Config) Stringify() string {
	u := url.URL{
		Scheme: "s3",
		Host:   c.bucketHost,
	}
	if c.User != "" {
		u.User = url.UserPassword(c.User, "")
	}
	if c.bucketPath != "" {
		u.Path = c.bucketPath
	}
	u.RawQuery = c.toQuery().Encode()
	return u.String()
}

// SafeStringify is Stringify with credentials masked as `*`.
func (c *Config) SafeStringify() string {
	s := c.Stringify()
	s = reSecretAccessKey.ReplaceAllString(s, "secretAccessKey=*")
	s = reAccessID.ReplaceAllString(s, "accessID=*")
	s = reSessionToken.ReplaceAllString(s, "sessionToken=*")
	return s
}

// toQuery synthesizes the DSN query portion. To preserve the pre-v2
// DSN format byte-for-byte we only emit keys whose presence carries
// meaning beyond the type's zero value.
func (c *Config) toQuery() url.Values {
	q := url.Values{}
	setIfNonEmpty := func(k, v string) {
		if v != "" {
			q.Set(k, v)
		}
	}
	setIfNonEmpty("db", c.DB)
	setIfNonEmpty("region", c.Region)
	setIfNonEmpty("accessID", c.AccessID)
	setIfNonEmpty("secretAccessKey", c.SecretAccessKey)
	setIfNonEmpty("sessionToken", c.SessionToken)
	setIfNonEmpty("AWSProfile", c.AWSProfile)
	setIfNonEmpty("catalog", c.Catalog)
	setIfNonEmpty("expectedBucketOwner", c.ExpectedBucketOwner)
	setIfNonEmpty("webIdentityRoleARN", c.WebIdentityRoleARN)
	setIfNonEmpty("webIdentityTokenFile", c.WebIdentityTokenFile)
	setIfNonEmpty("webIdentityRoleSessionName", c.WebIdentityRoleSessionName)

	if c.WorkGroup != nil {
		q.Set("workgroupName", c.WorkGroup.Name)
		if tags := c.WorkGroup.Tags.Get(); len(tags) > 0 {
			var tb strings.Builder
			for _, t := range tags {
				tb.WriteString("|")
				tb.WriteString(*t.Key)
				tb.WriteString("`")
				tb.WriteString(*t.Value)
			}
			q.Set("tag", tb.String())
		} else if c.WorkGroup.Tags != nil {
			// empty-but-non-nil tags emit an explicit empty `tag=` for
			// parity with the pre-v2 DSN format the tests assert.
			q.Set("tag", "")
		}
	}

	if c.ResultEncryption != nil {
		q.Set("resultEncryptionOption", string(c.ResultEncryption.EncryptionOption))
		if c.ResultEncryption.KmsKey != nil && *c.ResultEncryption.KmsKey != "" {
			q.Set("resultEncryptionKmsKey", *c.ResultEncryption.KmsKey)
		}
	}

	if c.ResultPollInterval > 0 {
		q.Set("resultPollIntervalSeconds", strconv.Itoa(int(c.ResultPollInterval.Seconds())))
	}
	// > 0, not > 1: an explicit 1.0 means "disable backoff" (see
	// PollBackoffMultiplier) and must survive the DSN round-trip rather
	// than silently reverting to the package default.
	if c.ResultPollBackoffMultiplier > 0 {
		q.Set("resultPollBackoffMultiplier", strconv.FormatFloat(c.ResultPollBackoffMultiplier, 'f', -1, 64))
	}
	if c.ResultPollMaxInterval > 0 {
		q.Set("resultPollMaxIntervalSeconds", strconv.Itoa(int(c.ResultPollMaxInterval.Seconds())))
	}

	// missingAsEmptyString defaults to true; emit only when explicitly
	// off so the DSN stays terse for the common case.
	if !c.MissingAsEmptyString {
		q.Set("missingAsEmptyString", "false")
	}
	if c.MissingAsDefault {
		q.Set("missingAsDefault", "true")
	}
	if c.MissingAsNil {
		q.Set("missingAsNil", "true")
	}
	if c.MoneyWise {
		q.Set("MoneyWise", "true")
	}
	if c.ReadOnly {
		q.Set("ReadOnly", "true")
	}
	// WGRemoteCreation defaults to true (see NewNoOpsConfig / fromQuery
	// missing-key handling); emit explicitly so DSN round-trip preserves
	// the field when a caller sets it to false.
	q.Set("WGRemoteCreation", strconv.FormatBool(c.WGRemoteCreation))
	// MetricsEnabled and LoggingEnabled default to true; emit only when
	// explicitly off so the DSN stays terse for the common case.
	if !c.MetricsEnabled {
		q.Set("MetricsEnabled", "false")
	}
	if !c.LoggingEnabled {
		q.Set("LoggingEnabled", "false")
	}

	if c.ServiceLimit != nil {
		q.Set("DDLQueryTimeout", strconv.Itoa(c.ServiceLimit.DDLQueryTimeout))
		q.Set("DMLQueryTimeout", strconv.Itoa(c.ServiceLimit.DMLQueryTimeout))
	}

	for col, val := range c.MaskedColumns {
		q.Set("masked_"+col, val)
	}

	return q
}

// SetOutputBucket sets the S3 bucket holding query result sets. The
// argument must start with `s3://`; the bucket name follows S3 naming
// rules (lowercase, no underscores).
func (c *Config) SetOutputBucket(s string) error {
	if !strings.HasPrefix(s, "s3://") {
		return ErrConfigOutputLocation
	}
	rest := s[len("s3://"):]
	parts := strings.SplitN(rest, "/", 2)
	c.bucketHost = parts[0]
	c.bucketPath = ""
	if len(parts) == 2 {
		c.bucketPath = parts[1]
	}
	return nil
}

// OutputBucket returns the S3 bucket URI in `s3://Host/Path` form.
func (c *Config) OutputBucket() string {
	if c.bucketPath == "" {
		return "s3://" + c.bucketHost + "/"
	}
	return "s3://" + c.bucketHost + "/" + c.bucketPath
}

// SetRegion validates and sets the AWS region.
func (c *Config) SetRegion(s string) error {
	if s == "" {
		return ErrConfigRegion
	}
	c.Region = s
	return nil
}

// SetAccessID validates and sets the AWS access key ID.
func (c *Config) SetAccessID(s string) error {
	if s == "" {
		return ErrConfigAccessIDRequired
	}
	c.AccessID = s
	return nil
}

// SetSecretAccessKey validates and sets the AWS secret access key.
func (c *Config) SetSecretAccessKey(s string) error {
	if s == "" {
		return ErrConfigAccessKeyRequired
	}
	c.SecretAccessKey = s
	return nil
}

// SetWorkGroup attaches a workgroup, filling nil-Config and nil-Tags
// with defaults so the driver always has something usable.
func (c *Config) SetWorkGroup(w *Workgroup) error {
	if w == nil {
		return ErrConfigWGPointer
	}
	if w.Config == nil {
		w.Config = GetDefaultWGConfig()
	}
	if w.Tags == nil {
		w.Tags = NewWGTags()
	}
	c.WorkGroup = w
	return nil
}

// CheckColumnMasked returns the substitute value configured for the
// given column, if any.
func (c *Config) CheckColumnMasked(column string) (string, bool) {
	v, ok := c.MaskedColumns[column]
	return v, ok
}

// SetMaskedColumnValue records a substitute value for `column`.
func (c *Config) SetMaskedColumnValue(column, value string) {
	if c.MaskedColumns == nil {
		c.MaskedColumns = map[string]string{}
	}
	c.MaskedColumns[column] = value
}

// AccessIDOrEnv returns the explicit access ID or the AWS_ACCESS_KEY_ID
// / AWS_ACCESS_KEY environment variable when the field is empty.
func (c *Config) AccessIDOrEnv() string {
	if c.AccessID != "" {
		return c.AccessID
	}
	return GetFromEnvVal(credAccessEnvKey)
}

// SecretAccessKeyOrEnv returns the explicit secret or the
// AWS_SECRET_ACCESS_KEY / AWS_SECRET_KEY environment variable.
func (c *Config) SecretAccessKeyOrEnv() string {
	if c.SecretAccessKey != "" {
		return c.SecretAccessKey
	}
	return GetFromEnvVal(credSecretEnvKey)
}

// SessionTokenOrEnv returns the explicit session token or the
// AWS_SESSION_TOKEN environment variable.
func (c *Config) SessionTokenOrEnv() string {
	if c.SessionToken != "" {
		return c.SessionToken
	}
	return GetFromEnvVal(credSessionEnvKey)
}

// RegionOrEnv returns the explicit region or AWS_REGION /
// AWS_DEFAULT_REGION.
func (c *Config) RegionOrEnv() string {
	if c.Region != "" {
		return c.Region
	}
	return GetFromEnvVal(regionEnvKeys)
}

// CatalogOrDefault returns the configured catalog or DefaultCatalog
// (AwsDataCatalog).
func (c *Config) CatalogOrDefault() string {
	if c.Catalog != "" {
		return c.Catalog
	}
	return DefaultCatalog
}

// PollInterval returns the initial GetQueryExecution poll interval,
// falling back to PoolInterval seconds when ResultPollInterval is
// zero.
func (c *Config) PollInterval() time.Duration {
	if c.ResultPollInterval > 0 {
		return c.ResultPollInterval
	}
	return time.Duration(PoolInterval) * time.Second
}

// PollMaxInterval returns the cap on the per-poll wait, or
// PollMaxInterval seconds when no override is set.
func (c *Config) PollMaxInterval() time.Duration {
	if c.ResultPollMaxInterval > 0 {
		return c.ResultPollMaxInterval
	}
	return time.Duration(PollMaxInterval) * time.Second
}

// PollBackoffMultiplier returns the per-poll multiplier, defaulting to
// PollBackoffMultiplier when unset. Values <= 1 disable backoff.
func (c *Config) PollBackoffMultiplier() float64 {
	if c.ResultPollBackoffMultiplier > 0 {
		return c.ResultPollBackoffMultiplier
	}
	return PollBackoffMultiplier
}

// clone returns a copy deep enough that a Connector can freeze the
// Config at construction time: pooled Connections all share the
// connector's Config, so a caller mutating theirs after sql.OpenDB
// must not race or retroactively change live connections. Workgroup
// Tags / Config sub-pointers stay shared — the driver treats them as
// read-only.
func (c *Config) clone() *Config {
	dup := *c
	dup.MaskedColumns = maps.Clone(c.MaskedColumns)
	if c.ServiceLimit != nil {
		sl := *c.ServiceLimit
		dup.ServiceLimit = &sl
	}
	if c.ResultEncryption != nil {
		enc := *c.ResultEncryption
		if c.ResultEncryption.KmsKey != nil {
			k := *c.ResultEncryption.KmsKey
			enc.KmsKey = &k
		}
		dup.ResultEncryption = &enc
	}
	if c.WorkGroup != nil {
		wg := *c.WorkGroup
		dup.WorkGroup = &wg
	}
	return &dup
}
