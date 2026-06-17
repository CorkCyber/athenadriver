// SPDX-License-Identifier: MIT

package athenadriver

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDriver(t *testing.T) {
	dsn := "s3://henry.wu%40uber.com:@fake-query-results-arbitrary-bucket?db=default&" +
		"region=us-east-1&workgroup_config=%7B%0A++BytesScannedCutoffPerQuery%3A+1073741824%2C%0A++Enfo" +
		"rceWorkGroupConfiguration%3A+true%2C%0A++PublishCloudWatchMetricsEnabled%3A+true%2C%0A++Reques" +
		"terPaysEnabled%3A+false%0A%7D&workgroupName=henry_wu"
	pDB, err := sql.Open(DriverName, dsn)
	assert.Nil(t, err)
	assert.NotNil(t, pDB)

	pDB, err = sql.Open(DriverName+"x", "")
	assert.NotNil(t, err)
	assert.Nil(t, pDB)
}

func TestSQLDriver_Open(t *testing.T) {
	s := SQLDriver{}
	testConf := NewNoOpsConfig()
	c, e := s.Open(testConf.Stringify())
	assert.Nil(t, e)
	assert.NotNil(t, c)

	c, e = s.Open("")
	assert.Nil(t, c)
	assert.NotNil(t, e)
}

func TestSQLDriver_OpenConnector(t *testing.T) {
	s := SQLDriver{}
	testConf := NewNoOpsConfig()
	c, e := s.OpenConnector(testConf.Stringify())
	assert.Nil(t, e)
	assert.NotNil(t, c)
}

func TestSQLDriver_Validate(t *testing.T) {
	s := SQLDriver{}
	assert.Nil(t, s.Validate(NewNoOpsConfig().Stringify()))
	assert.NotNil(t, s.Validate(""))
}
