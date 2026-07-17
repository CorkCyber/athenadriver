// SPDX-License-Identifier: MIT

package athenadriver

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	"github.com/stretchr/testify/assert"
)

func TestAthenaConfig(t *testing.T) {
	var s3bucket string = "s3://fake-query-results-arbitrary-bucket/"

	wgTags := NewWGTags()
	wgTags.AddTag("Uber User", "henry.wu@uber.com")
	wgTags.AddTag("Uber Asset", "abc.efg")
	wg := NewWG("henry_wu", nil, wgTags)
	testConf := NewNoOpsConfig()
	err := testConf.SetOutputBucket(s3bucket)
	assert.Nil(t, err)
	err = testConf.SetRegion("us-east-1")
	assert.Nil(t, err)
	testConf.User = "henry.wu@uber.com"
	testConf.DB = "default" // default

	err = testConf.SetWorkGroup(wg)
	assert.Nil(t, err)
	assert.Equal(t, "henry.wu@uber.com", testConf.User)
	assert.Equal(t, "s3://fake-query-results-arbitrary-bucket/", testConf.OutputBucket())
	expected := "s3://henry.wu%40uber.com:@fake-query-results-arbitrary-bucket?WGRemoteCreation=true&db=default&region=us-east-1&tag=%7CUber+User%60henry.wu%40uber.com%7CUber+Asset%60abc.efg&workgroupName=henry_wu"
	actual := testConf.Stringify()
	assert.Equal(t, actual, expected)
	w := *testConf.WorkGroup
	assert.Equal(t, len(w.Tags.Get()), len(wgTags.Get()))

	x, err := NewConfig(expected)
	assert.Equal(t, x.OutputBucket(), s3bucket)
	assert.Nil(t, err)
}

func TestGetOutputBucket(t *testing.T) {
	var s3bucket string = "s3://fake-query-results-arbitrary-bucket/local/"
	testConf := NewNoOpsConfig()
	err := testConf.SetOutputBucket(s3bucket)
	conf, _ := NewConfig(testConf.Stringify())
	assert.Nil(t, err)
	assert.Equal(t, "s3://fake-query-results-arbitrary-bucket/local/", testConf.OutputBucket())
	assert.Equal(t, "s3://fake-query-results-arbitrary-bucket/local/", conf.OutputBucket())
}

func TestAthenaConfigWrongS3Bucket(t *testing.T) {
	var s3bucket string = "file:///fake-query-results-arbitrary-bucket/"
	testConf := NewNoOpsConfig()
	err := testConf.SetOutputBucket(s3bucket)
	assert.NotNil(t, err)
}

func TestConfig_SetOutputBucket(t *testing.T) {
	var s3bucket string = "s3://fake-query-results-arbitrary-bucket"
	testConf := NewNoOpsConfig()
	err := testConf.SetOutputBucket(s3bucket)
	assert.Nil(t, err)
}

func TestAthenaConfigWrongRegion(t *testing.T) {
	testConf := NewNoOpsConfig()
	err := testConf.SetRegion("")
	assert.NotNil(t, err)
}

func TestAthenaConfigWrongWG(t *testing.T) {
	testConf := NewNoOpsConfig()
	err := testConf.SetWorkGroup(nil)
	assert.NotNil(t, err)

	wg := NewWG("wg", nil, nil)
	e := testConf.SetWorkGroup(wg)
	assert.Nil(t, e)
}

func TestAthenaConfigSafeString(t *testing.T) {
	var s3bucket string = "s3://fake-query-results-arbitrary-bucket/"

	wg := NewWG("henry_wu", nil, nil)
	testConf := NewNoOpsConfig()
	err := testConf.SetOutputBucket(s3bucket)
	assert.Nil(t, err)
	err = testConf.SetRegion("us-east-1")
	assert.Nil(t, err)
	testConf.User = "henry.wu@uber.com"
	testConf.DB = "default" // default
	err = testConf.SetWorkGroup(wg)
	assert.Nil(t, err)
	err = testConf.SetSecretAccessKey("thisisaKey")
	assert.Nil(t, err)
	err = testConf.SetAccessID("thisisanID")
	assert.Nil(t, err)
	testConf.SessionToken = "thisisaToken"
	assert.Equal(t, "henry.wu@uber.com", testConf.User)
	assert.Equal(t, "s3://fake-query-results-arbitrary-bucket/", testConf.OutputBucket())
	expectedRawString := "s3://henry.wu%40uber.com:@fake-query-results-arbitrary-bucket?WGRemoteCreation=true&accessID=thisisanID&db=default&region=us-east-1&secretAccessKey=thisisaKey&sessionToken=thisisaToken&tag=&workgroupName=henry_wu"
	expectedSafeString := "s3://henry.wu%40uber.com:@fake-query-results-arbitrary-bucket?WGRemoteCreation=true&accessID=*&db=default&region=us-east-1&secretAccessKey=*&sessionToken=*&tag=&workgroupName=henry_wu"
	actualRaw := testConf.Stringify()
	actualSafe := testConf.SafeStringify()
	assert.Equal(t, expectedRawString, actualRaw)
	assert.Equal(t, expectedSafeString, actualSafe)

	x, err := NewConfig(expectedRawString)
	assert.Equal(t, x.OutputBucket(), s3bucket)
	assert.Nil(t, err)
}

func TestConfig_SetMaskedColumnValue(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetMaskedColumnValue("abc", "xxx")
	m, b := testConf.CheckColumnMasked("abc")
	assert.Equal(t, "xxx", m)
	assert.True(t, b)
	m, b = testConf.CheckColumnMasked("ABC")
	assert.NotEqual(t, m, "xxx")
	assert.False(t, b)
}

func TestConfig_SetMetrics(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.MetricsEnabled = true
	assert.True(t, testConf.MetricsEnabled)
	testConf.MetricsEnabled = false
	assert.False(t, testConf.MetricsEnabled)
}

func TestConfig_SetLogging(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.LoggingEnabled = true
	assert.True(t, testConf.LoggingEnabled)
	testConf.LoggingEnabled = false
	assert.False(t, testConf.LoggingEnabled)
}

func TestConfig_IsMissingAsEmptyString(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.MissingAsEmptyString = true
	assert.True(t, testConf.MissingAsEmptyString)
	testConf.MissingAsEmptyString = false
	assert.False(t, testConf.MissingAsEmptyString)
}

func TestConfig_IsMissingAsDefault(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.MissingAsDefault = true
	assert.True(t, testConf.MissingAsDefault)
	testConf.MissingAsDefault = false
	assert.False(t, testConf.MissingAsDefault)
}

func TestConfig_IsMissingAsNil(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.MissingAsNil = true
	assert.True(t, testConf.MissingAsNil)
	testConf.MissingAsNil = false
	assert.False(t, testConf.MissingAsNil)
}

func TestConfig_IsWGRemoteCreationAllowed(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.WGRemoteCreation = true
	assert.True(t, testConf.WGRemoteCreation)
	testConf.WGRemoteCreation = false
	assert.False(t, testConf.WGRemoteCreation)
}

func TestConfig_NewDefaultConfig(t *testing.T) {
	_, err := NewDefaultConfig("", "", "", "")
	assert.NotNil(t, err)
	_, err = NewDefaultConfig("file:///", "", "", "")
	assert.NotNil(t, err)
	_, err = NewDefaultConfig("s3:///abc", "", "", "")
	assert.NotNil(t, err)
	assert.NotNil(t, err)
	_, err = NewDefaultConfig("s3:///abc", "east", "", "")
	assert.NotNil(t, err)
	_, err = NewDefaultConfig("s3:///abc", "east", "as", "")
	assert.NotNil(t, err)
	_, err = NewDefaultConfig("s3:///abc", "east", "as", "ss")
	assert.Nil(t, err)
}

func TestConfig_NewConfig(t *testing.T) {
	x, err := NewConfig("\n")
	assert.NotNil(t, err)
	assert.Nil(t, x)
}

// TestConfig_NewConfig_QueryTimeoutOverflowRejected pins a fix for an
// integer overflow: isQueryTimeOut converts DDLQueryTimeout/DMLQueryTimeout
// (seconds) to a time.Duration via time.Duration(n) * time.Second. A large
// enough n overflows int64 and wraps negative, making time.Since(start) >
// timeout true immediately - every query would appear to have already timed
// out. NewConfig must reject such values instead of silently accepting them.
func TestConfig_NewConfig_QueryTimeoutOverflowRejected(t *testing.T) {
	for _, key := range []string{"DDLQueryTimeout", "DMLQueryTimeout"} {
		for _, bad := range []string{"0", "-1", "99999999999"} {
			dsn := "s3://out/bucket/?db=x&region=us-east-1&" + key + "=" + bad
			_, err := NewConfig(dsn)
			assert.Error(t, err, "%s=%s: want error, got nil", key, bad)
		}
	}

	// A sane value must still work.
	cfg, err := NewConfig("s3://out/bucket/?db=x&region=us-east-1&DDLQueryTimeout=3600&DMLQueryTimeout=60")
	assert.NoError(t, err)
	assert.Equal(t, 3600, cfg.ServiceLimit.DDLQueryTimeout)
	assert.Equal(t, 60, cfg.ServiceLimit.DMLQueryTimeout)
}

func TestConfig_GetWorkgroup(t *testing.T) {
	wg := NewWG("henry_wu", nil, nil)
	testConf := NewNoOpsConfig()
	err := testConf.SetWorkGroup(wg)
	assert.Nil(t, err)
	w := *testConf.WorkGroup
	assert.Empty(t, w.Tags.tags)
}

func TestConfig_SetReadOnly(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.ReadOnly = false
	assert.False(t, testConf.ReadOnly)
}

func TestConfig_GetDB(t *testing.T) {
	testConf := NewNoOpsConfig()
	assert.Equal(t, DefaultDBName, testConf.DB)
}

func TestConfig_GetRegion(t *testing.T) {
	testConf := NewNoOpsConfig()
	assert.Equal(t, DefaultRegion, testConf.RegionOrEnv())
	testConf = &Config{}
	assert.Equal(t, GetFromEnvVal(regionEnvKeys), testConf.RegionOrEnv())
}

func TestConfig_GetAccessID(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetAccessID("abc")
	assert.Equal(t, "abc", testConf.AccessIDOrEnv())
	testConf = &Config{}
	assert.Equal(t, GetFromEnvVal(credAccessEnvKey), testConf.AccessIDOrEnv())
}

func TestConfig_GetSecretAccessKey(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetSecretAccessKey("abc")
	assert.Equal(t, "abc", testConf.SecretAccessKeyOrEnv())
	testConf = &Config{}
	assert.Equal(t, GetFromEnvVal(credSecretEnvKey), testConf.SecretAccessKeyOrEnv())
}

func TestConfig_GetSessionToken(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SessionToken = "abc"
	assert.Equal(t, "abc", testConf.SessionTokenOrEnv())
	testConf = &Config{}
	assert.Equal(t, GetFromEnvVal(credSessionEnvKey), testConf.SessionTokenOrEnv())
}

func TestConfig_WGConfig(t *testing.T) {
	conf := &athenatypes.WorkGroupConfiguration{
		BytesScannedCutoffPerQuery:      aws.Int64(10 * DefaultBytesScannedCutoffPerQuery),
		EnforceWorkGroupConfiguration:   aws.Bool(true),
		PublishCloudWatchMetricsEnabled: aws.Bool(true),
		RequesterPaysEnabled:            aws.Bool(false),
	}
	wg := NewWG("workgroup1", conf, nil)
	assert.Equal(t, *wg.Config.BytesScannedCutoffPerQuery, int64(DefaultBytesScannedCutoffPerQuery*10))
}

func TestConfig_SetMoneyWise(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.MoneyWise = false
	assert.False(t, testConf.MoneyWise)
	testConf.MoneyWise = true
	assert.True(t, testConf.MoneyWise)
}

func TestConfig_SetAWSProfile(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.AWSProfile = "development"
	assert.Equal(t, "development", testConf.AWSProfile)
}

func TestConfig_SetServiceLimitOverride(t *testing.T) {
	var s3bucket string = "s3://fake-query-results-arbitrary-bucket/"

	testConf := NewNoOpsConfig()
	_ = testConf.SetOutputBucket(s3bucket)
	ddlQueryTimeout := 1000 * 60 // 1000 minutes
	testConf.ServiceLimit = &ServiceLimitOverride{DDLQueryTimeout: ddlQueryTimeout}
	assert.Equal(t, ddlQueryTimeout, testConf.ServiceLimit.DDLQueryTimeout)

	expected := "s3://fake-query-results-arbitrary-bucket?DDLQueryTimeout=60000&DMLQueryTimeout=0&WGRemoteCreation=true&db=default&region=us-east-1"
	assert.Equal(t, expected, testConf.Stringify())

	dmlQueryTimeout := 60 * 60 // 60 minutes
	testConf.ServiceLimit.DMLQueryTimeout = dmlQueryTimeout
	assert.Equal(t, ddlQueryTimeout, testConf.ServiceLimit.DDLQueryTimeout)
	assert.Equal(t, dmlQueryTimeout, testConf.ServiceLimit.DMLQueryTimeout)

	expected = "s3://fake-query-results-arbitrary-bucket?DDLQueryTimeout=60000&DMLQueryTimeout=3600&WGRemoteCreation=true&db=default&region=us-east-1"
	assert.Equal(t, expected, testConf.Stringify())
}

func TestConfig_ResultPollIntervalOverride(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.ResultPollInterval = time.Duration(1) * time.Second
	interval := testConf.PollInterval()
	assert.Equal(t, time.Duration(1)*time.Second, interval)
}

func TestConfig_ResultPollIntervalDefault(t *testing.T) {
	testConf := NewNoOpsConfig()
	interval := testConf.PollInterval()
	assert.Equal(t, time.Second*time.Duration(PoolInterval), interval)
}

// TestConfig_WGRemoteCreationRoundtrip guards against a regression where
// toQuery skipped WGRemoteCreation=false, so NewConfig(cfg.Stringify())
// flipped the field back to its default (true).
func TestConfig_WGRemoteCreationRoundtrip(t *testing.T) {
	for _, want := range []bool{true, false} {
		src := NewNoOpsConfig()
		_ = src.SetOutputBucket("s3://bucket/")
		src.WGRemoteCreation = want
		got, err := NewConfig(src.Stringify())
		assert.Nil(t, err)
		assert.Equal(t, want, got.WGRemoteCreation)
	}
}
