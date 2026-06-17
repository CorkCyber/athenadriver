// SPDX-License-Identifier: MIT

package athenadriver

import (
	"github.com/stretchr/testify/assert"
	"net/url"
	"testing"
	"time"
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
	testConf.SetUser("henry.wu@uber.com")
	testConf.SetDB("default") // default

	err = testConf.SetWorkGroup(wg)
	assert.Nil(t, err)
	assert.Equal(t, "henry.wu@uber.com", testConf.GetUser())
	assert.Equal(t, "s3://fake-query-results-arbitrary-bucket/", testConf.GetOutputBucket())
	expected := "s3://henry.wu%40uber.com:@fake-query-results-arbitrary-bucket?WGRemoteCreation=true&db=default&missingAsEmptyString=true&region=us-east-1&tag=%7CUber+User%60henry.wu%40uber.com%7CUber+Asset%60abc.efg&workgroupName=henry_wu"
	actual := testConf.Stringify()
	assert.Equal(t, actual, expected)
	w := testConf.GetWorkgroup()
	assert.Equal(t, len(w.Tags.Get()), len(wgTags.Get()))

	x, err := NewConfig(expected)
	assert.Equal(t, x.GetOutputBucket(), s3bucket)
	assert.Nil(t, err)
}

func TestGetOutputBucket(t *testing.T) {
	var s3bucket string = "s3://fake-query-results-arbitrary-bucket/local/"
	testConf := NewNoOpsConfig()
	err := testConf.SetOutputBucket(s3bucket)
	conf, _ := NewConfig(testConf.Stringify())
	assert.Nil(t, err)
	assert.Equal(t, "s3://fake-query-results-arbitrary-bucket/local/", testConf.GetOutputBucket())
	assert.Equal(t, "s3://fake-query-results-arbitrary-bucket/local/", conf.GetOutputBucket())
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
	testConf.SetUser("henry.wu@uber.com")
	testConf.SetDB("default") // default
	err = testConf.SetWorkGroup(wg)
	assert.Nil(t, err)
	err = testConf.SetSecretAccessKey("thisisaKey")
	assert.Nil(t, err)
	err = testConf.SetAccessID("thisisanID")
	assert.Nil(t, err)
	testConf.SetSessionToken("thisisaToken")
	assert.Equal(t, "henry.wu@uber.com", testConf.GetUser())
	assert.Equal(t, "s3://fake-query-results-arbitrary-bucket/", testConf.GetOutputBucket())
	expectedRawString := "s3://henry.wu%40uber.com:@fake-query-results-arbitrary-bucket?WGRemoteCreation=true&accessID=thisisanID&db=default&missingAsEmptyString=true&region=us-east-1&secretAccessKey=thisisaKey&sessionToken=thisisaToken&tag=&workgroupName=henry_wu"
	expectedSafeString := "s3://henry.wu%40uber.com:@fake-query-results-arbitrary-bucket?WGRemoteCreation=true&accessID=*&db=default&missingAsEmptyString=true&region=us-east-1&secretAccessKey=*&sessionToken=*&tag=&workgroupName=henry_wu"
	actualRaw := testConf.Stringify()
	actualSafe := testConf.SafeStringify()
	assert.Equal(t, expectedRawString, actualRaw)
	assert.Equal(t, expectedSafeString, actualSafe)

	x, err := NewConfig(expectedRawString)
	assert.Equal(t, x.GetOutputBucket(), s3bucket)
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
	testConf.SetMetrics(true)
	assert.True(t, testConf.IsMetricsEnabled())
	testConf.SetMetrics(false)
	assert.False(t, testConf.IsMetricsEnabled())
}

func TestConfig_SetLogging(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetLogging(true)
	assert.True(t, testConf.IsLoggingEnabled())
	testConf.SetLogging(false)
	assert.False(t, testConf.IsLoggingEnabled())
}

func TestConfig_IsMissingAsEmptyString(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetMissingAsEmptyString(true)
	assert.True(t, testConf.IsMissingAsEmptyString())
	testConf.SetMissingAsEmptyString(false)
	assert.False(t, testConf.IsMissingAsEmptyString())
}

func TestConfig_IsMissingAsDefault(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetMissingAsDefault(true)
	assert.True(t, testConf.IsMissingAsDefault())
	testConf.SetMissingAsDefault(false)
	assert.False(t, testConf.IsMissingAsDefault())
}

func TestConfig_IsMissingAsNil(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetMissingAsNil(true)
	assert.True(t, testConf.IsMissingAsNil())
	testConf.SetMissingAsNil(false)
	assert.False(t, testConf.IsMissingAsNil())
}

func TestConfig_IsWGRemoteCreationAllowed(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetWGRemoteCreationAllowed(true)
	assert.True(t, testConf.IsWGRemoteCreationAllowed())
	testConf.SetWGRemoteCreationAllowed(false)
	assert.False(t, testConf.IsWGRemoteCreationAllowed())
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

func TestConfig_GetWorkgroup(t *testing.T) {
	wg := NewWG("henry_wu", nil, nil)
	testConf := NewNoOpsConfig()
	err := testConf.SetWorkGroup(wg)
	assert.Nil(t, err)
	w := testConf.GetWorkgroup()
	assert.Empty(t, w.Tags.tags)
}

func TestConfig_SetReadOnly(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetReadOnly(false)
	assert.False(t, testConf.IsReadOnly())
}

func TestConfig_GetDB(t *testing.T) {
	testConf := NewNoOpsConfig()
	assert.Equal(t, DefaultDBName, testConf.GetDB())
	testConf.SetDB("")
	assert.Equal(t, DefaultDBName, testConf.GetDB())
}

func TestConfig_GetRegion(t *testing.T) {
	testConf := NewNoOpsConfig()
	assert.Equal(t, DefaultRegion, testConf.GetRegion())
	testConf = &Config{
		dsn:    *new(url.URL),
		values: url.Values{},
	}
	assert.Equal(t, GetFromEnvVal(regionEnvKeys), testConf.GetRegion())
}

func TestConfig_GetAccessID(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetAccessID("abc")
	assert.Equal(t, "abc", testConf.GetAccessID())
	testConf = &Config{
		dsn:    *new(url.URL),
		values: url.Values{},
	}
	assert.Equal(t, GetFromEnvVal(credAccessEnvKey), testConf.GetAccessID())
}

func TestConfig_GetSecretAccessKey(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetSecretAccessKey("abc")
	assert.Equal(t, "abc", testConf.GetSecretAccessKey())
	testConf = &Config{
		dsn:    *new(url.URL),
		values: url.Values{},
	}
	assert.Equal(t, GetFromEnvVal(credSecretEnvKey), testConf.GetSecretAccessKey())
}

func TestConfig_GetSessionToken(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetSessionToken("abc")
	assert.Equal(t, "abc", testConf.GetSessionToken())
	testConf = &Config{
		dsn:    *new(url.URL),
		values: url.Values{},
	}
	assert.Equal(t, GetFromEnvVal(credSessionEnvKey), testConf.GetSessionToken())
}

func TestConfig_WGConfig(t *testing.T) {
	conf := NewWGConfig(10*DefaultBytesScannedCutoffPerQuery, true, true, false, nil)
	wg := NewWG("workgroup1", conf, nil)
	assert.Equal(t, *wg.Config.BytesScannedCutoffPerQuery, int64(DefaultBytesScannedCutoffPerQuery*10))
}

func TestConfig_SetMoneyWise(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetMoneyWise(false)
	assert.False(t, testConf.IsMoneyWise())
	testConf.SetMoneyWise(true)
	assert.True(t, testConf.IsMoneyWise())
}

func TestConfig_SetAWSProfile(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetAWSProfile("development")
	assert.Equal(t, "development", testConf.GetAWSProfile())
}

func TestConfig_SetServiceLimitOverride(t *testing.T) {
	var s3bucket string = "s3://fake-query-results-arbitrary-bucket/"

	testConf := NewNoOpsConfig()
	_ = testConf.SetOutputBucket(s3bucket)
	serviceLimitOverride := NewServiceLimitOverride()
	ddlQueryTimeout := 1000 * 60 // 1000 minutes
	_ = serviceLimitOverride.SetDDLQueryTimeout(ddlQueryTimeout)
	testConf.SetServiceLimitOverride(*serviceLimitOverride)
	testServiceLimitOverride := testConf.GetServiceLimitOverride()
	assert.Equal(t, ddlQueryTimeout, testServiceLimitOverride.GetDDLQueryTimeout())

	expected := "s3://fake-query-results-arbitrary-bucket?DDLQueryTimeout=60000&DMLQueryTimeout=0&WGRemoteCreation=true&db=default&missingAsEmptyString=true&region=us-east-1"
	assert.Equal(t, expected, testConf.Stringify())

	dmlQueryTimeout := 60 * 60 // 60 minutes
	_ = serviceLimitOverride.SetDMLQueryTimeout(dmlQueryTimeout)
	testConf.SetServiceLimitOverride(*serviceLimitOverride)
	testServiceLimitOverride = testConf.GetServiceLimitOverride()
	assert.Equal(t, ddlQueryTimeout, testServiceLimitOverride.GetDDLQueryTimeout())
	assert.Equal(t, dmlQueryTimeout, testServiceLimitOverride.GetDMLQueryTimeout())

	expected = "s3://fake-query-results-arbitrary-bucket?DDLQueryTimeout=60000&DMLQueryTimeout=3600&WGRemoteCreation=true&db=default&missingAsEmptyString=true&region=us-east-1"
	assert.Equal(t, expected, testConf.Stringify())
}

func TestConfig_ResultPollIntervalOverride(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.SetResultPollIntervalSeconds(1)
	interval := testConf.GetResultPollIntervalSeconds()
	assert.Equal(t, time.Duration(1)*time.Second, interval)
}

func TestConfig_ResultPollIntervalDefault(t *testing.T) {
	testConf := NewNoOpsConfig()
	interval := testConf.GetResultPollIntervalSeconds()
	assert.Equal(t, time.Second*time.Duration(PoolInterval), interval)
}
