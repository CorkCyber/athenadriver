// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql/driver"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/stretchr/testify/assert"
)

// variadicToSlice, https://blog.learngoprogramming.com/golang-variadic-funcs-how-to-patterns-369408f19085
// https://stackoverflow.com/questions/23723955/how-can-i-pass-a-slice-as-a-variadic-input
// If f is variadic with final parameter type ...T, then within the function
// the argument is equivalent to a parameter of type []T. At each call of f,
// the argument passed to the final parameter is a new slice of type []T whose
// successive elements are the actual arguments,
// which all must be assignable to the type T.
func variadicToSlice(dest ...driver.Value) []driver.Value {
	return dest
}

func TestOnePageSuccess(t *testing.T) {
	testConf := NewNoOpsConfig()
	tests := []struct {
		desc                string
		queryID             string
		expectedResultsSize int
		expectedError       error
	}{
		{
			desc:                "show query, header, 5 row, no error",
			queryID:             "show",
			expectedResultsSize: 5,
			expectedError:       nil,
		},
	}
	for _, test := range tests {
		r, _ := NewRows(context.Background(), newMockAthenaClient(),
			test.queryID, testConf, NewObservability(testConf, nil, nil))

		var testArray, firstName, lastName string
		var active bool
		var uid int
		var registerDate, registerTS time.Time
		cnt := 0
		var err error = nil
		for {
			err = r.Next(variadicToSlice(&testArray, &active, &firstName, &lastName,
				&uid, &registerDate, &registerTS))
			if err != nil {
				if err != io.EOF {
					assert.Equal(t, test.expectedError, err)
				}
				break
			}
			cnt++
		}
		assert.Equal(t, test.expectedResultsSize, cnt)
		if err != io.EOF {
			assert.Equal(t, test.expectedError, err)
		}
		r.Close()
	}
}

func TestNextFailure(t *testing.T) {
	testConf := NewNoOpsConfig()
	tests := []struct {
		desc                string
		queryID             string
		expectedResultsSize int
		expectedError       error
	}{
		{
			desc:                "failed during calling next",
			queryID:             "RowsNextFailed",
			expectedResultsSize: 4,
			expectedError:       ErrTestMockGeneric,
		},
	}
	for _, test := range tests {
		r, _ := NewRows(context.Background(), newMockAthenaClient(),
			test.queryID,
			testConf, NewObservability(testConf, nil, nil))

		var testArray, firstName, lastName string
		var active bool
		var uid int
		var registerDate, registerTS time.Time
		cnt := 0
		var err error = nil
		for {
			err = r.Next(variadicToSlice(&testArray, &active, &firstName, &lastName,
				&uid, &registerDate, &registerTS))
			if err != nil {
				if err != io.EOF {
					assert.Equal(t, test.expectedError, err)
				}
				break
			}
			cnt++
		}
		assert.Equal(t, test.expectedResultsSize, cnt)
		if err != io.EOF {
			assert.Equal(t, test.expectedError, err)
		}
	}
}

func TestMultiplePages(t *testing.T) {
	testConf := NewNoOpsConfig()
	tests := []struct {
		desc                string
		queryID             string
		expectedResultsSize int
		expectedError       error
	}{
		{
			desc:                "select query, header, multiple pages",
			queryID:             "SELECT_OK",
			expectedResultsSize: 35,
			expectedError:       nil,
		},
	}
	var r *Rows
	for _, test := range tests {
		r, _ = NewRows(context.Background(), newMockAthenaClient(),
			test.queryID,
			testConf, NewObservability(testConf, nil, nil))

		var testArray, firstName, lastName string
		var active bool
		var uid int
		var registerDate, registerTS time.Time
		cnt := 0
		var err error = nil
		for {
			err = r.Next(variadicToSlice(&testArray, &active, &firstName, &lastName,
				&uid, &registerDate, &registerTS))
			if err != nil {
				if err != io.EOF {
					assert.Equal(t, test.expectedError, err)
				}
				break
			}
			cnt++
		}
		assert.Equal(t, test.expectedResultsSize, cnt)
		if err != io.EOF {
			assert.Equal(t, test.expectedError, err)
		}
	}
	var dest []driver.Value = make([]driver.Value, 8)
	assert.Equal(t, io.EOF, r.Next(dest))
}

func TestRows_Columns(t *testing.T) {
	testConf := NewNoOpsConfig()
	cs := createTestColumns()
	tests := []struct {
		desc                string
		queryID             string
		expectedResultsSize int
		expectedError       error
	}{
		{
			desc:                "select query, header, multiple pages",
			queryID:             "SELECT_OK",
			expectedResultsSize: 35,
			expectedError:       nil,
		},
	}
	for _, test := range tests {
		r, _ := NewRows(context.Background(), newMockAthenaClient(),
			test.queryID,
			testConf, NewObservability(testConf, nil, nil))
		assert.Equal(t, len(r.Columns()), len(cs))
	}
}

func TestRows_ColumnTypeDatabaseTypeName(t *testing.T) {
	testConf := NewNoOpsConfig()
	cs := createTestColumns()
	tests := []struct {
		desc                string
		queryID             string
		expectedResultsSize int
		expectedError       error
	}{
		{
			desc:                "select query, header, multiple pages",
			queryID:             "SELECT_OK",
			expectedResultsSize: 35,
			expectedError:       nil,
		},
	}
	for _, test := range tests {
		r, _ := NewRows(context.Background(), newMockAthenaClient(),
			test.queryID,
			testConf, NewObservability(testConf, nil, nil))
		for i, v := range cs {
			assert.Equal(t, r.ColumnTypeDatabaseTypeName(i), *v.Type)

		}

	}
}

func TestRows_GetDefaultValueForColumnType(t *testing.T) {
	testConf := NewNoOpsConfig()
	tests := []struct {
		desc                string
		queryID             string
		expectedResultsSize int
		expectedError       error
	}{
		{
			desc:                "select query, header, multiple pages",
			queryID:             "SELECT_OK",
			expectedResultsSize: 35,
			expectedError:       nil,
		},
	}
	for _, test := range tests {
		r, _ := NewRows(context.Background(), newMockAthenaClient(),
			test.queryID,
			testConf, NewObservability(testConf, nil, nil))
		for _, v := range []string{"tinyint", "smallint", "integer", "bigint"} {
			assert.Equal(t, 0, r.getDefaultValueForColumnType(v))
		}
		for _, v := range []string{"json", "char", "varchar", "varbinary", "row", "string", "binary",
			"struct", "interval year to month", "interval day to second", "decimal",
			"ipaddress", "array", "map", "unknown"} {
			assert.Equal(t, "", r.getDefaultValueForColumnType(v))
		}
		for _, v := range []string{"float", "double", "real"} {
			assert.Equal(t, 0.0, r.getDefaultValueForColumnType(v))
		}
		for _, v := range []string{"date", "time", "time with time zone", "timestamp", "timestamp with time zone"} {
			assert.Equal(t, r.getDefaultValueForColumnType(v), time.Time{})
		}
		assert.Equal(t, false, r.getDefaultValueForColumnType("boolean"))
		assert.Equal(t, "", r.getDefaultValueForColumnType("XXX"))
	}
}

func TestRows_AthenaTypeToGoType(t *testing.T) {
	testConf := NewNoOpsConfig()
	r, _ := NewRows(context.Background(), newMockAthenaClient(),
		"SELECT_OK", testConf, NewObservability(testConf, nil, nil))
	c := newColumnInfo("a", "tinyint")
	// tinyint
	rv := "1"
	g, e := r.athenaTypeToGoType(c, &rv, testConf)
	assert.Nil(t, e)
	assert.Equal(t, int8(1), g)

	rv = "x"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.NotNil(t, e)
	assert.Nil(t, g)

	// smallint
	c = newColumnInfo("a", "smallint")
	rv = "1"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.Nil(t, e)
	assert.Equal(t, int16(1), g)

	rv = "x"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.NotNil(t, e)
	assert.Nil(t, g)

	// int
	c = newColumnInfo("a", "integer")
	rv = "1"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.Nil(t, e)
	assert.Equal(t, int32(1), g)

	rv = "x"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.NotNil(t, e)
	assert.Nil(t, g)

	// bigint
	c = newColumnInfo("a", "bigint")
	rv = "1"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.Nil(t, e)
	assert.Equal(t, int64(1), g)

	rv = "x"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.NotNil(t, e)
	assert.Nil(t, g)

	// float
	c = newColumnInfo("a", "float")
	rv = "1.0"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.Nil(t, e)
	assert.Equal(t, float32(1.0), g)

	rv = "x"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.NotNil(t, e)
	assert.Nil(t, g)

	// real
	c = newColumnInfo("a", "real")
	rv = "1.0"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.Nil(t, e)
	assert.Equal(t, float32(1.0), g)

	rv = "x"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.NotNil(t, e)
	assert.Nil(t, g)

	// double
	c = newColumnInfo("a", "double")
	rv = "1.0"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.Nil(t, e)
	assert.Equal(t, float64(1.0), g)

	rv = "x"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.NotNil(t, e)
	assert.Nil(t, g)

	// string-like
	for _, s := range []string{"json", "char", "varchar", "varbinary", "row",
		"string", "binary",
		"struct", "interval year to month", "interval day to second", "decimal",
		"ipaddress", "array", "map", "unknown"} {
		c = newColumnInfo("a", s)
		rv = "012"
		g, e = r.athenaTypeToGoType(c, &rv, testConf)
		assert.Nil(t, e)
		assert.Equal(t, "012", g)
	}

	// boolean
	for _, s := range []string{"boolean"} {
		c = newColumnInfo("a", s)
		rv = "true"
		g, e = r.athenaTypeToGoType(c, &rv, testConf)
		assert.Nil(t, e)
		assert.Equal(t, true, g)

		rv = "false"
		g, e = r.athenaTypeToGoType(c, &rv, testConf)
		assert.Nil(t, e)
		assert.Equal(t, false, g)

		rv = "x"
		g, e = r.athenaTypeToGoType(c, &rv, testConf)
		assert.NotNil(t, e)
		assert.Nil(t, g)
	}

	// date and time
	_ = time.Now()
	for _, s := range []string{"date", "time", "time with time zone",
		"timestamp", "timestamp with time zone"} {
		c = newColumnInfo("a", s)
		rv = "2020-01-20"
		g, e = r.athenaTypeToGoType(c, &rv, testConf)
		assert.Nil(t, e)
		assert.Equal(t, reflect.TypeFor[time.Time](), reflect.TypeOf(g))

		rv = "x"
		g, e = r.athenaTypeToGoType(c, &rv, testConf)
		assert.NotNil(t, e)
		assert.Nil(t, g)
	}

	c = newColumnInfo("a", "some_weird_type")
	rv = "123"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.NotNil(t, e)
	assert.Nil(t, g)

	// missing data - rawValue is nil
	c = newColumnInfo("a", "integer")
	g, e = r.athenaTypeToGoType(c, nil, testConf)
	assert.Nil(t, e)
	assert.Equal(t, "", g)

	testConf.MissingAsEmptyString = false
	testConf.MissingAsDefault = true
	testConf.MissingAsNil = false
	g, e = r.athenaTypeToGoType(c, nil, testConf)
	assert.Nil(t, e)
	assert.Equal(t, 0, g)

	testConf.MissingAsEmptyString = false
	testConf.MissingAsDefault = false
	testConf.MissingAsNil = true
	g, e = r.athenaTypeToGoType(c, nil, testConf)
	assert.Nil(t, e)
	assert.Nil(t, g)

	testConf.MissingAsEmptyString = false
	testConf.MissingAsDefault = false
	testConf.MissingAsNil = false
	g, e = r.athenaTypeToGoType(c, nil, testConf)
	assert.NotNil(t, e)
	assert.Nil(t, g)

	// masked column
	testConf.SetMaskedColumnValue("a", "xxx")
	g, e = r.athenaTypeToGoType(c, nil, testConf)
	assert.Nil(t, e)
	assert.Equal(t, "xxx", g)
}

func TestRows_ColumnTypeDatabaseTypeName2(t *testing.T) {
	testConf := NewNoOpsConfig()
	r, _ := NewRows(context.Background(), newMockAthenaClient(),
		"SELECT_OK", testConf, NewObservability(testConf, nil, nil))
	c := newColumnInfo("a", nil)
	getQueryResultsOutput := &athena.GetQueryResultsOutput{
		ResultSet: &athenatypes.ResultSet{
			ResultSetMetadata: &athenatypes.ResultSetMetadata{
				ColumnInfo: []athenatypes.ColumnInfo{
					c,
				},
			},
		},
	}
	r.ResultOutput = getQueryResultsOutput
	assert.Equal(t, "", r.ColumnTypeDatabaseTypeName(0))
}

func TestRows_NewRows(t *testing.T) {
	testConf := NewNoOpsConfig()
	r, e := NewRows(context.Background(), newMockAthenaClient(),
		"1coloumn0row",
		testConf, NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	assert.NotNil(t, r)

	r, e = NewRows(context.Background(), newMockAthenaClient(),
		"1coloumn0row_valid",
		testConf, NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	assert.Equal(t, *r.ResultOutput.ResultSet.Rows[0].Data[0].VarCharValue,
		"1024")

	r, e = NewRows(context.Background(), newMockAthenaClient(),
		"column_more_than_row_fields",
		testConf, NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	assert.NotNil(t, r)

	r, e = NewRows(context.Background(), newMockAthenaClient(),
		"row_fields_more_than_column",
		testConf, NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	assert.NotNil(t, r)

	r, e = NewRows(context.Background(), newMockAthenaClient(),
		"GetQueryResultsWithContext_return_error",
		testConf, NewObservability(testConf, nil, nil))
	assert.NotNil(t, e)
	assert.Nil(t, r)

	// rawValue is nil
	r, e = NewRows(context.Background(), newMockAthenaClient(),
		"missing_data_resp",
		testConf, NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	assert.NotNil(t, r)
	var dest []driver.Value = make([]driver.Value, 8)
	e = r.Next(dest)
	assert.Equal(t, nil, e)

	// raise error for missing value
	testConf.MissingAsEmptyString = false
	testConf.MissingAsDefault = false
	r, e = NewRows(context.Background(), newMockAthenaClient(),
		"missing_data_resp",
		testConf, NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	assert.NotNil(t, r)
	e = r.Next(dest)
	assert.Equal(t, "missing data at column c1", e.Error())

	r, e = NewRows(context.Background(), newMockAthenaClient(),
		"missing_data_resp2",
		testConf, NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	assert.NotNil(t, r)
	e = r.Next(dest)
	assert.NotEqual(t, e, io.EOF)

	// error when row.Next()
	r, e = NewRows(context.Background(), newMockAthenaClient(),
		"SELECT_GetQueryResults_ERR",
		testConf, NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	assert.NotNil(t, r)
	for {
		e = r.Next(dest)
		if e != nil {
			assert.Equal(t, ErrTestMockGeneric, e)
			break
		}
	}

	// missing row in page
	r, e = NewRows(context.Background(), newMockAthenaClient(),
		"SELECT_EMPTY_ROW_IN_PAGE",
		testConf, NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	assert.NotNil(t, r)
	for {
		e = r.Next(dest)
		if e != nil {
			assert.Equal(t, io.EOF, e)
			break
		}
	}

	// close in the loop
	r, e = NewRows(context.Background(), newMockAthenaClient(),
		"SELECT_GetQueryResults_ERR",
		testConf, NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	assert.NotNil(t, r)
	cnt := 0
	for {
		e = r.Next(dest)
		if e != nil {
			assert.Equal(t, io.EOF, e)
			break
		}
		if cnt == 7 {
			r.Close()
		}
		cnt++
	}

}
