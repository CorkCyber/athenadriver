// SPDX-License-Identifier: MIT

package athenadriver

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
)

// Config is for AWS Athena Driver Config.
// Be noted this is different from aws.Config.
type Config struct {
	dsn    url.URL    `yaml:"dns"`
	values url.Values `yaml:"values"`
}

var reSecretAccessKey = regexp.MustCompile(`secretAccessKey=[^&]+`)
var reAccessID = regexp.MustCompile(`accessID=[^&]+`)
var reSessionToken = regexp.MustCompile(`sessionToken=[^&]+`)

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

// NewDefaultConfig is to new a Config with some default values.
func NewDefaultConfig(outputBucket string, region string, accessID string,
	secretAccessKey string) (*Config, error) {
	conf := NewNoOpsConfig()
	err := conf.SetOutputBucket(outputBucket)
	if err != nil {
		return nil, err
	}
	err = conf.SetRegion(region)
	if err != nil {
		return nil, err
	}
	err = conf.SetAccessID(accessID)
	if err != nil {
		return nil, err
	}
	err = conf.SetSecretAccessKey(secretAccessKey)
	conf.SetResultPollIntervalSeconds(PoolInterval)
	return conf, err
}

// NewNoOpsConfig is to create a noop version of driver Config WITHOUT credentials.
func NewNoOpsConfig() *Config {
	a := Config{
		dsn: url.URL{},
	}
	a.dsn.Scheme = "s3"
	a.values = make(map[string][]string, 32)
	a.values.Set("db", DefaultDBName)
	a.values.Set("region", DefaultRegion)
	a.SetMissingAsEmptyString(true)
	a.SetWGRemoteCreationAllowed(true)
	return &a
}

// NewConfig is to create Config from a string.
func NewConfig(s string) (*Config, error) {
	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}
	a := Config{
		dsn: *u,
	}

	a.values, err = url.ParseQuery(u.RawQuery)
	if !a.isValid() {
		return nil, ErrConfigInvalidConfig
	}
	return &a, err
}

func (c *Config) isValid() bool {
	return c.dsn.Scheme == "s3" && c.values.Get("region") != ""
}

// String is to return the string form of DSN.
func (c *Config) String() string {
	return c.dsn.String()
}

// Stringify is to return the string form of DSN like JSON.stringify().
// Please refer to: https://www.w3schools.com/js/js_json_stringify.asp
func (c *Config) Stringify() string {
	c.dsn.RawQuery = c.values.Encode()
	return c.String()
}

// SafeStringify is a secure version of Stringify(), with security information masked with *.
func (c *Config) SafeStringify() string {
	rawString := c.Stringify()
	s := reSecretAccessKey.ReplaceAllString(rawString, `secretAccessKey=*`)
	s = reAccessID.ReplaceAllString(s, `accessID=*`)
	s = reSessionToken.ReplaceAllString(s, `sessionToken=*`)
	return s
}

// SetOutputBucket is to set S3 bucket for result set.
// On March 1, 2018, we updated our naming conventions for S3 buckets in the US East (N. Virginia) Region to match
// the naming conventions that we use in all other worldwide AWS Regions.
// Amazon S3 no longer supports creating bucket names that contain uppercase letters or underscores.
// https://docs.aws.amazon.com/AmazonS3/latest/dev/BucketRestrictions.html#bucketnamingrules
func (c *Config) SetOutputBucket(o string) error {
	if !strings.HasPrefix(o, "s3://") {
		return ErrConfigOutputLocation
	}
	o = o[5:]
	ss := strings.SplitN(o, "/", 2)
	if len(ss) == 2 {
		c.dsn.Host = ss[0]
		c.dsn.Path = ss[1]
	} else {
		c.dsn.Host = ss[0]
		c.dsn.Path = ""
	}
	return nil
}

// SetRegion is to set region.
func (c *Config) SetRegion(o string) error {
	if len(o) == 0 {
		return ErrConfigRegion
	}
	c.values.Set("region", o)
	return nil
}

// GetRegion is getter of Region.
func (c *Config) GetRegion() string {
	if val := c.values.Get("region"); val != "" {
		return val
	}
	return GetFromEnvVal(regionEnvKeys)
}

// SetUser is a setter of User.
func (c *Config) SetUser(o string) {
	c.dsn.User = url.UserPassword(o, "")
}

// SetDB is a setter of DB.
func (c *Config) SetDB(o string) {
	c.values.Set("db", o)
}

// GetDB is getter of DB.
func (c *Config) GetDB() string {
	if val := c.values.Get("db"); val != "" {
		return val
	}
	return DefaultDBName
}

// SetResultEncryption sets the default S3 server-side encryption applied to
// query results written to OutputLocation. Pass `option =
// athenatypes.EncryptionOptionSseS3` for SSE-S3 (no kmsKey needed); for
// SSE-KMS or CSE-KMS supply the KMS key ARN or ID. Per-query override is
// available via context with ResultEncryptionKey (or WithResultEncryption).
func (c *Config) SetResultEncryption(option athenatypes.EncryptionOption, kmsKey string) {
	c.values.Set("resultEncryptionOption", string(option))
	if kmsKey != "" {
		c.values.Set("resultEncryptionKmsKey", kmsKey)
	} else {
		c.values.Del("resultEncryptionKmsKey")
	}
}

// GetResultEncryption returns the configured ResultConfiguration encryption,
// or nil if none is set on this Config.
func (c *Config) GetResultEncryption() *athenatypes.EncryptionConfiguration {
	opt := c.values.Get("resultEncryptionOption")
	if opt == "" {
		return nil
	}
	enc := &athenatypes.EncryptionConfiguration{
		EncryptionOption: athenatypes.EncryptionOption(opt),
	}
	if k := c.values.Get("resultEncryptionKmsKey"); k != "" {
		enc.KmsKey = &k
	}
	return enc
}

// SetExpectedBucketOwner sets the AWS account ID expected to own the S3
// bucket referenced by OutputLocation. If the live bucket owner differs at
// query time, Athena fails the request — a cross-account safety net.
func (c *Config) SetExpectedBucketOwner(accountID string) {
	c.values.Set("expectedBucketOwner", accountID)
}

// GetExpectedBucketOwner returns the configured expected S3 bucket owner,
// or "" if none is set.
func (c *Config) GetExpectedBucketOwner() string {
	return c.values.Get("expectedBucketOwner")
}

// SetCatalog sets the default Athena data catalog used by this Config.
// Per-query override is available via context with CatalogKey.
func (c *Config) SetCatalog(o string) {
	c.values.Set("catalog", o)
}

// GetCatalog returns the configured Athena data catalog, falling back to
// DefaultCatalog (AwsDataCatalog) when unset.
func (c *Config) GetCatalog() string {
	if val := c.values.Get("catalog"); val != "" {
		return val
	}
	return DefaultCatalog
}

// SetResultPollIntervalSeconds is a setter of Overriding poll interval.
func (c *Config) SetResultPollIntervalSeconds(n int) {
	c.values.Set("resultPollIntervalSeconds", strconv.Itoa(n))
}

// GetResultPollIntervalSeconds is getter of resultPollIntervalSeconds.
func (c *Config) GetResultPollIntervalSeconds() time.Duration {
	if val := c.values.Get("resultPollIntervalSeconds"); val != "" {
		n, err := strconv.Atoi(val)
		if err != nil {
			return time.Duration(PoolInterval) * time.Second
		}
		return time.Duration(n) * time.Second
	}
	return time.Duration(PoolInterval) * time.Second
}

// SetResultPollBackoff configures exponential backoff between
// GetQueryExecution polls. The poll interval starts at the value set by
// SetResultPollIntervalSeconds and is multiplied by multiplier after each
// poll, capped at maxInterval. A multiplier of 1.0 (the default) disables
// backoff and uses the initial interval forever. maxInterval below the
// initial interval is treated as "no growth".
func (c *Config) SetResultPollBackoff(multiplier float64, maxInterval time.Duration) {
	if multiplier > 1 {
		c.values.Set("resultPollBackoffMultiplier", strconv.FormatFloat(multiplier, 'f', -1, 64))
	} else {
		c.values.Del("resultPollBackoffMultiplier")
	}
	if maxInterval > 0 {
		c.values.Set("resultPollMaxIntervalSeconds", strconv.Itoa(int(maxInterval.Seconds())))
	} else {
		c.values.Del("resultPollMaxIntervalSeconds")
	}
}

// GetResultPollBackoffMultiplier returns the per-poll multiplier; 1.0
// means "no backoff".
func (c *Config) GetResultPollBackoffMultiplier() float64 {
	val := c.values.Get("resultPollBackoffMultiplier")
	if val == "" {
		return 1.0
	}
	if n, err := strconv.ParseFloat(val, 64); err == nil && n > 1 {
		return n
	}
	return 1.0
}

// GetResultPollMaxInterval returns the cap on the per-poll wait, or the
// initial poll interval when no cap is set.
func (c *Config) GetResultPollMaxInterval() time.Duration {
	val := c.values.Get("resultPollMaxIntervalSeconds")
	if val == "" {
		return c.GetResultPollIntervalSeconds()
	}
	if n, err := strconv.Atoi(val); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	return c.GetResultPollIntervalSeconds()
}

// SetWorkGroup is a setter of WorkGroup.
func (c *Config) SetWorkGroup(w *Workgroup) error {
	if w == nil {
		return ErrConfigWGPointer
	}
	c.values.Set("workgroupName", w.Name)
	if w.Tags != nil {
		var tagsString strings.Builder
		tagsString.WriteString(c.values.Get("tag"))
		for _, tag := range w.Tags.Get() {
			tagsString.WriteString("|" + *tag.Key + "`" + *tag.Value)
		}
		c.values.Set("tag", tagsString.String())
	}
	if w.Config == nil {
		w.Config = GetDefaultWGConfig()
	}
	return nil
}

// SetAccessID is a setter of AWS Access ID.
func (c *Config) SetAccessID(o string) error {
	if len(o) == 0 {
		return ErrConfigAccessIDRequired
	}
	c.values.Set("accessID", o)
	return nil
}

// GetAccessID is a getter of AWS Access ID. It will try to get access ID from:
//  1. string stored in c.values
//  2. environmental variable ${AWS_ACCESS_KEY_ID} or ${AWS_ACCESS_KEY}
func (c *Config) GetAccessID() string {
	if val := c.values.Get("accessID"); val != "" {
		return val
	}
	return GetFromEnvVal(credAccessEnvKey)
}

// SetSecretAccessKey is a setter of AWS Access Key.
func (c *Config) SetSecretAccessKey(o string) error {
	if len(o) == 0 {
		return ErrConfigAccessKeyRequired
	}
	c.values.Set("secretAccessKey", o)
	return nil
}

// GetSecretAccessKey is a getter of AWS Access Key.
func (c *Config) GetSecretAccessKey() string {
	if val := c.values.Get("secretAccessKey"); val != "" {
		return val
	}
	return GetFromEnvVal(credSecretEnvKey)
}

// SetSessionToken is a setter of AWS Session Token.
func (c *Config) SetSessionToken(o string) {
	c.values.Set("sessionToken", o)
}

// GetSessionToken is a getter of AWS Session Token.
func (c *Config) GetSessionToken() string {
	if val := c.values.Get("sessionToken"); val != "" {
		return val
	}
	return GetFromEnvVal(credSessionEnvKey)
}

// GetUser is getter of User.
func (c *Config) GetUser() string {
	return c.dsn.User.Username()
}

// GetOutputBucket is getter of OutputBucket.
func (c *Config) GetOutputBucket() string {
	if strings.HasPrefix(c.dsn.Path, "/") {
		return c.dsn.Scheme + "://" + c.dsn.Host + c.dsn.Path
	}
	return c.dsn.Scheme + "://" + c.dsn.Host + "/" + c.dsn.Path
}

// GetWorkgroup is getter of Workgroup.
func (c *Config) GetWorkgroup() Workgroup {
	tagString := c.values.Get("tag")
	if len(tagString) == 0 {
		wg := Workgroup{
			Name:   c.values.Get("workgroupName"),
			Config: GetDefaultWGConfig(),
			Tags:   NewWGTags(),
		}
		return wg
	}
	tags := strings.Split(tagString[1:], "|")
	t := NewWGTags()
	for _, tag := range tags {
		ts := strings.Split(tag, "`")
		t.AddTag(ts[0], ts[1])
	}
	wg := Workgroup{
		Name:   c.values.Get("workgroupName"),
		Config: GetDefaultWGConfig(),
		Tags:   t,
	}
	return wg
}

// IsMissingAsEmptyString return true if missing value is set to be returned as empty string.
func (c *Config) IsMissingAsEmptyString() bool {
	return c.values.Get("missingAsEmptyString") == "true"
}

// IsMissingAsDefault return true if missing value is set to be returned as default data.
func (c *Config) IsMissingAsDefault() bool {
	return c.values.Get("missingAsDefault") == "true"
}

// IsMissingAsNil return true if missing value is set to be returned as nil.
func (c *Config) IsMissingAsNil() bool {
	return c.values.Get("missingAsNil") == "true"
}

// SetMissingAsEmptyString is to set if missing value is returned as empty string.
func (c *Config) SetMissingAsEmptyString(b bool) {
	missingAsEmptyString := "true"
	if !b {
		missingAsEmptyString = "false"
	}
	c.values.Set("missingAsEmptyString", missingAsEmptyString)
}

// SetMissingAsDefault is to set if missing value is returned as default data.
func (c *Config) SetMissingAsDefault(b bool) {
	if b {
		c.values.Set("missingAsDefault", "true")
	} else {
		c.values.Set("missingAsDefault", "false")
	}

}

// SetMissingAsNil is to set if missing value is returned as nil.
func (c *Config) SetMissingAsNil(b bool) {
	if b {
		c.values.Set("missingAsNil", "true")
	} else {
		c.values.Set("missingAsNil", "false")
	}
}

// CheckColumnMasked is to check if a specific column has been masked by some value.
// https://stackoverflow.com/questions/30285169/replace-the-empty-or-null-value-with-specific-value-in-hive-query-result/30289503
func (c *Config) CheckColumnMasked(columnName string) (string, bool) {
	if val, ok := c.values["masked_"+columnName]; ok {
		return val[0], true
	}
	return "", false
}

// SetMaskedColumnValue is to set masked value for some column.
func (c *Config) SetMaskedColumnValue(columnName string, value string) {
	c.values.Set("masked_"+columnName, value)
}

// IsWGRemoteCreationAllowed is to check if we are allowed to create workgroup with API from client.
func (c *Config) IsWGRemoteCreationAllowed() bool {
	return c.values.Get("WGRemoteCreation") == "true"
}

// SetWGRemoteCreationAllowed is to set if we are allowed to create workgroup with API from client.
func (c *Config) SetWGRemoteCreationAllowed(b bool) {
	if b {
		c.values.Set("WGRemoteCreation", "true")
	} else {
		c.values.Set("WGRemoteCreation", "false")
	}
}

// IsLoggingEnabled is to check if driver level logging enabled.
func (c *Config) IsLoggingEnabled() bool {
	return c.values.Get("LoggingEnabled") != "false"
}

// SetLogging is to set if driver level logging enabled.
func (c *Config) SetLogging(b bool) {
	if b {
		c.values.Set("LoggingEnabled", "true")
	} else {
		c.values.Set("LoggingEnabled", "false")
	}
}

// IsMetricsEnabled is to check if driver level metrics enabled.
func (c *Config) IsMetricsEnabled() bool {
	return c.values.Get("MetricsEnabled") == "true"
}

// SetMetrics is to set if driver level logging enabled.
func (c *Config) SetMetrics(b bool) {
	if b {
		c.values.Set("MetricsEnabled", "true")
	} else {
		c.values.Set("MetricsEnabled", "false")
	}
}

// SetReadOnly is to set if only SELECT/SHOW/DESC are allowed
func (c *Config) SetReadOnly(b bool) {
	if b {
		c.values.Set("ReadOnly", "true")
	} else {
		c.values.Set("ReadOnly", "false")
	}
}

// IsReadOnly is to check if only SELECT/SHOW/DESC are allowed
func (c *Config) IsReadOnly() bool {
	return c.values.Get("ReadOnly") == "true"
}

// SetMoneyWise is to set if we are in the moneywise mode
func (c *Config) SetMoneyWise(b bool) {
	if b {
		c.values.Set("MoneyWise", "true")
	} else {
		c.values.Set("MoneyWise", "false")
	}
}

// IsMoneyWise is to check if we are in the moneywise mode
func (c *Config) IsMoneyWise() bool {
	return c.values.Get("MoneyWise") == "true"
}

// SetWebIdentity configures AssumeRoleWithWebIdentity credential
// resolution. tokenFile is the path the SDK will read on each refresh
// (matches AWS_WEB_IDENTITY_TOKEN_FILE); roleSessionName may be empty.
// All three values round-trip through the DSN. When roleARN and
// tokenFile are both set, resolveAWSConfig builds a
// stscreds.NewWebIdentityRoleProvider and skips the other DSN credential
// paths.
func (c *Config) SetWebIdentity(roleARN, tokenFile, roleSessionName string) {
	c.values.Set("webIdentityRoleARN", roleARN)
	c.values.Set("webIdentityTokenFile", tokenFile)
	if roleSessionName != "" {
		c.values.Set("webIdentityRoleSessionName", roleSessionName)
	} else {
		c.values.Del("webIdentityRoleSessionName")
	}
}

// GetWebIdentity returns the configured roleARN, tokenFile,
// roleSessionName triple. Empty strings mean unset.
func (c *Config) GetWebIdentity() (roleARN, tokenFile, roleSessionName string) {
	return c.values.Get("webIdentityRoleARN"),
		c.values.Get("webIdentityTokenFile"),
		c.values.Get("webIdentityRoleSessionName")
}

// SetAWSProfile is to manually set the credential provider
// https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/configuring-sdk.html
func (c *Config) SetAWSProfile(profile string) {
	c.values.Set("AWSProfile", profile)
}

// GetAWSProfile is to get the credential provider name manually set by user
func (c *Config) GetAWSProfile() string {
	return c.values.Get("AWSProfile")
}

// SetServiceLimitOverride is to set values from a ServiceLimitOverride
func (c *Config) SetServiceLimitOverride(serviceLimitOverride ServiceLimitOverride) {
	for k, v := range serviceLimitOverride.GetAsStringMap() {
		c.values.Set(k, v)
	}
}

// GetServiceLimitOverride is to get the ServiceLimitOverride manually set by a user
func (c *Config) GetServiceLimitOverride() *ServiceLimitOverride {
	serviceLimitOverride := NewServiceLimitOverride()
	serviceLimitOverride.SetFromValues(c.values)
	return serviceLimitOverride
}
