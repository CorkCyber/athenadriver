// SPDX-License-Identifier: MIT

package athenadriver

import (
	"database/sql/driver"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	"math"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/stretchr/testify/assert"
)

func TestColsToCSV(t *testing.T) {
	sqlRows := sqlmock.NewRows([]string{"one", "two", "three"})
	rows := mockRowsToSQLRows(sqlRows)
	expected := ColsToCSV(rows)
	assert.Equal(t, "one,two,three\n", expected)
	assert.Equal(t, "", ColsToCSV(nil))
}

func TestRowsToCSV(t *testing.T) {
	sqlRows := sqlmock.NewRows([]string{"one", "two", "three"})
	sqlRows.AddRow("1", "2", "3")
	rows := mockRowsToSQLRows(sqlRows)
	expected := RowsToCSV(rows)
	assert.Equal(t, "1,2,3\n", expected)

	s := RowsToCSV(nil)
	assert.Equal(t, "", s)
}

func TestColsRowsToCSV(t *testing.T) {
	sqlRows := sqlmock.NewRows([]string{"one", "two", "three"})
	sqlRows.AddRow("1", "2", "3")
	rows := mockRowsToSQLRows(sqlRows)
	expected := ColsRowsToCSV(rows)
	assert.Equal(t, "one,two,three\n1,2,3\n", expected)
}

func TestRandInt8(t *testing.T) {
	s := randInt8()
	i, err := strconv.ParseInt(*s, 10, 8)
	assert.True(t, math.MinInt8 <= i && i <= math.MaxInt8)
	assert.Nil(t, err)
}

func TestRandInt16(t *testing.T) {
	s := randInt16()
	i, err := strconv.ParseInt(*s, 10, 16)
	assert.True(t, math.MinInt16 <= i && i <= math.MaxInt16)
	assert.Nil(t, err)
}

func TestRandInt(t *testing.T) {
	s := randInt()
	i, err := strconv.ParseInt(*s, 10, 32)
	assert.True(t, math.MinInt32 <= i && i <= math.MaxInt32)
	assert.Nil(t, err)
}

func TestRandInt64(t *testing.T) {
	s := randUInt64()
	_, err := strconv.ParseUint(*s, 10, 64)
	assert.Nil(t, err)
}

func TestRandFloat32(t *testing.T) {
	s := randFloat32()
	i, err := strconv.ParseFloat(*s, 32)
	assert.True(t, math.SmallestNonzeroFloat32 <= i && i <= math.MaxFloat32)
	assert.Nil(t, err)
}

func TestRandFloat64(t *testing.T) {
	s := randFloat64()
	i, err := strconv.ParseFloat(*s, 64)
	assert.True(t, math.SmallestNonzeroFloat64 <= i && i <= math.MaxFloat64)
	assert.Nil(t, err)
}

func TestRandRow(t *testing.T) {
	c1 := newColumnInfo("c1", nil)
	r := randRow([]athenatypes.ColumnInfo{c1})
	assert.Equal(t, len(r.Data), 1)
	assert.Equal(t, "a\tb", *r.Data[0].VarCharValue)

	for _, ty := range AthenaColumnTypes {
		c1 := newColumnInfo("c1", ty)
		r := randRow([]athenatypes.ColumnInfo{c1})
		assert.Equal(t, len(r.Data), 1)
	}
}

func TestNamedValueToValue(t *testing.T) {
	dn := driver.NamedValue{
		Name: "abc",
	}
	d := []driver.NamedValue{
		dn,
	}
	v := namedValueToValue(d)
	assert.Equal(t, len(v), 1)
}

type aType struct {
	S string
}

func TestValueToNamedValue(t *testing.T) {
	dn := aType{
		S: "abc",
	}
	d := []driver.Value{
		dn,
	}
	v := valueToNamedValue(d)
	assert.Equal(t, len(v), 1)
	assert.True(t, v[0].Name == "")
	assert.True(t, v[0].Ordinal == 1)
	assert.True(t, v[0].Value.(aType).S == "abc")
}

func TestIsQueryTimeOut(t *testing.T) {
	assert.False(t, isQueryTimeOut(time.Now(), athenatypes.StatementTypeDdl, nil))
	assert.False(t, isQueryTimeOut(time.Now(), athenatypes.StatementTypeDml, nil))
	assert.False(t, isQueryTimeOut(time.Now(), athenatypes.StatementTypeUtility, nil))
	now := time.Now()
	OneHourAgo := now.Add(-3600 * time.Second)
	assert.True(t, isQueryTimeOut(OneHourAgo, athenatypes.StatementTypeDml, nil))
	assert.False(t, isQueryTimeOut(OneHourAgo, athenatypes.StatementTypeDdl, nil))
	assert.False(t, isQueryTimeOut(OneHourAgo, "UNKNOWN", nil))

	testConf := NewServiceLimitOverride()
	testConf.SetDMLQueryTimeout(65 * 60) // 65 minutes
	assert.False(t, isQueryTimeOut(OneHourAgo, athenatypes.StatementTypeDml, testConf))

	testConf.SetDDLQueryTimeout(30 * 60) // 30 minutes
	assert.True(t, isQueryTimeOut(OneHourAgo, athenatypes.StatementTypeDdl, testConf))
	assert.True(t, isQueryTimeOut(OneHourAgo, "UNKNOWN", testConf))
}

func TestEscapeBytesBackslash(t *testing.T) {
	r := escapeBytesBackslash([]byte{}, []byte{'\x00'})
	assert.Equal(t, "\\0", string(r))

	r = escapeBytesBackslash([]byte{}, []byte{'\n'})
	assert.Equal(t, "\\n", string(r))

	r = escapeBytesBackslash([]byte{}, []byte{'\r'})
	assert.Equal(t, "\\r", string(r))

	r = escapeBytesBackslash([]byte{}, []byte{'\x1a'})
	assert.Equal(t, "\\Z", string(r))

	// Single quotes can be escaped by adding another single quote.
	// https://docs.aws.amazon.com/athena/latest/ug/select.html#select-escaping
	// https://docs.aws.amazon.com/athena/latest/ug/data-types.html#data-types-considerations
	r = escapeBytesBackslash([]byte{}, []byte{'\''})
	assert.Equal(t, string(r), `''`)

	r = escapeBytesBackslash([]byte{}, []byte{'"'})
	assert.Equal(t, string(r), `\"`)

	r = escapeBytesBackslash([]byte{}, []byte{'\\'})
	assert.Equal(t, string(r), `\\`)

	r = escapeBytesBackslash([]byte{}, []byte{'x'})
	assert.Equal(t, string(r), `x`)
}

func TestGetFromEnvVal(t *testing.T) {
	os.Setenv("henrywu_test", "1")
	assert.Equal(t, GetFromEnvVal([]string{"henrywu_test"}), "1")
	assert.Equal(t, GetFromEnvVal([]string{"wufuheng", "henrywu_test"}), "1")
	os.Unsetenv("henrywu_test")
	assert.Equal(t, GetFromEnvVal([]string{"henrywu_test"}), "")
}

func TestPrintCost(t *testing.T) {
	ping := "SELECTExecContext_OK_QID"
	stat := athenatypes.QueryExecutionStateSucceeded
	o := &athena.GetQueryExecutionOutput{
		QueryExecution: &athenatypes.QueryExecution{
			Query:            &ping,
			QueryExecutionId: &ping,
			Status: &athenatypes.QueryExecutionStatus{
				State: stat,
			},
			Statistics: &athenatypes.QueryExecutionStatistics{
				DataScannedInBytes: nil,
			},
		},
	}
	printCost("us-east-1", nil)
	printCost("us-east-1", &athena.GetQueryExecutionOutput{
		QueryExecution: nil,
	})
	printCost("us-east-1", &athena.GetQueryExecutionOutput{
		QueryExecution: &athenatypes.QueryExecution{
			Query:            &ping,
			QueryExecutionId: &ping,
			Status: &athenatypes.QueryExecutionStatus{
				State: stat,
			},
			Statistics: nil,
		},
	})
	printCost("us-east-1", o)
	cost := int64(123)
	o.QueryExecution.Statistics.DataScannedInBytes = &cost
	printCost("us-east-1", o)
	cost = int64(12345678123456)
	o.QueryExecution.Statistics.DataScannedInBytes = &cost
	printCost("us-east-1", o)
	cost = int64(0)
	o.QueryExecution.Statistics.DataScannedInBytes = &cost
	printCost("us-east-1", o)
}

func TestUilts_GetCost(t *testing.T) {
	const region = "us-east-1"
	// Zero / negative scans cost nothing.
	assert.Equal(t, 0.0, estimateScanCost(region, 0))
	assert.Equal(t, 0.0, estimateScanCost(region, -10))
	// Anything under 10MB is billed as 10MB.
	assert.Equal(t, float64(minScanBytes)*usdPerByte(region), estimateScanCost(region, 1))
	assert.Equal(t, float64(minScanBytes)*usdPerByte(region), estimateScanCost(region, minScanBytes-1))
	// Over the floor: linear in bytes.
	assert.Equal(t, float64(10*1024*1024*13)*usdPerByte(region), estimateScanCost(region, 10*1024*1024*13))
	// Unknown region falls back to the default rate.
	assert.Equal(t, estimateScanCost("zz-unknown-99", 1<<40), estimateScanCost("us-east-1", 1<<40))
	// More expensive region scales accordingly.
	assert.Equal(t, 9.0/5.0*estimateScanCost("us-east-1", 1<<40), estimateScanCost("sa-east-1", 1<<40))
}

func TestUtils_IsQID(t *testing.T) {
	assert.False(t, IsQID(`select "a44f8e61-4cbb-429a-b7ab-bea2c4a5caed"`))
	assert.True(t, IsQID("a44f8e61-4cbb-429a-b7ab-bea2c4a5caed"))
	assert.False(t, IsQID("a44f8e61-4cbb-429a-b7ab-bea2c4a5caeD"))
	assert.False(t, IsQID("a44f8e61"))
}

func Test_newHeaderResultPage(t *testing.T) {
	colName := "_col0"
	qid := "123"
	columnNames := []*string{&colName}
	columnTypes := []string{"string"}
	data := make([][]*string, 1)
	data[0] = []*string{&qid}
	page := newHeaderResultPage(columnNames, columnTypes, data)
	assert.NotNil(t, page)
}

func TestFormatString(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Empty string",
			input:    "",
			expected: "''",
		},
		{
			name:     "No special characters",
			input:    "This is a description string with no special characters",
			expected: "'This is a description string with no special characters'",
		},
		{
			name:     "Special characters are escaped",
			input:    "Athena's query's param\n",
			expected: "'Athena''s query''s param\\n'",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, FormatString(tc.input))
		})
	}
}

func TestFormatBytes(t *testing.T) {
	testCases := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "Empty byte slice",
			input:    []byte{},
			expected: []byte("_binary''"),
		},
		{
			name:     "No special characters",
			input:    []byte("This is a description string with no special characters"),
			expected: []byte("_binary'This is a description string with no special characters'"),
		},
		{
			name:     "Special characters are escaped",
			input:    []byte("Athena's query's param\n"),
			expected: []byte("_binary'Athena''s query''s param\\n'"),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, FormatBytes(tc.input))
		})
	}
}
