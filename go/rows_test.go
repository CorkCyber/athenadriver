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

	"github.com/aws/aws-sdk-go-v2/aws"
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

// athenaTypeToGoType is a test-only shim: it does the same athenaTypes lookup
// buildColMetas does once per Rows, then hands off to convertCell. Keeps the
// per-type conversion cases below readable without a second production code
// path for the same map.
func (r *Rows) athenaTypeToGoType(columnInfo athenatypes.ColumnInfo, rawValue *string, driverConfig *Config) (any, error) {
	var meta *athenaTypeMeta
	if columnInfo.Type != nil {
		if m, ok := athenaTypes[*columnInfo.Type]; ok {
			meta = &m
		}
	}
	return r.convertCell(&columnInfo, meta, rawValue, driverConfig)
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

func TestRows_MissingAsDefaultValue(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.MissingAsDefault = true
	testConf.MissingAsNil = false
	testConf.MissingAsEmptyString = false
	r, _ := NewRows(context.Background(), newMockAthenaClient(),
		"SELECT_OK", testConf, NewObservability(testConf, nil, nil))

	// convertCell returns the resolved meta's default when the cell is missing.
	check := func(typeName string, expected any) {
		t.Helper()
		meta := athenaTypes[typeName]
		ci := newColumnInfo("c", typeName)
		v, err := r.convertCell(&ci, &meta, nil, testConf)
		assert.NoError(t, err)
		assert.Equal(t, expected, v)
	}
	// Typed zero values: must match both the parse func's return type and
	// ColumnTypeScanType, so a missing cell scans like a present one.
	check("tinyint", int8(0))
	check("smallint", int16(0))
	check("integer", int32(0))
	check("bigint", int64(0))
	for _, v := range []string{"json", "char", "varchar", "varbinary", "row", "string", "binary",
		"struct", "interval year to month", "interval day to second", "decimal",
		"ipaddress", "array", "map", "unknown"} {
		check(v, "")
	}
	check("float", float32(0))
	check("real", float32(0))
	check("double", float64(0))
	for _, v := range []string{"date", "time", "time with time zone", "timestamp", "timestamp with time zone"} {
		check(v, time.Time{})
	}
	check("boolean", false)

	// unknown type: no meta resolved, falls back to the empty string.
	ciX := newColumnInfo("c", "XXX")
	v, err := r.convertCell(&ciX, nil, nil, testConf)
	assert.NoError(t, err)
	assert.Equal(t, "", v)
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

	// uuid / geometry (engine v3) decode as strings
	for _, s := range []string{"uuid", "geometry"} {
		c = newColumnInfo("a", s)
		rv = "abc"
		g, e = r.athenaTypeToGoType(c, &rv, testConf)
		assert.Nil(t, e)
		assert.Equal(t, "abc", g)
	}

	// an unrecognized type is passed through as its raw string, not an error
	c = newColumnInfo("a", "some_weird_type")
	rv = "123"
	g, e = r.athenaTypeToGoType(c, &rv, testConf)
	assert.Nil(t, e)
	assert.Equal(t, "123", g)

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
	assert.Equal(t, int32(0), g)

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

// database/sql reuses the same dest slice across Next calls, so a row with
// fewer datums than columns must not leak the previous row's trailing values.
func TestRows_ConvertRowShortRowClearsTail(t *testing.T) {
	testConf := NewNoOpsConfig()
	testConf.MissingAsNil = true
	testConf.MissingAsEmptyString = false
	testConf.MissingAsDefault = false
	r, _ := NewRows(context.Background(), newMockAthenaClient(),
		"SELECT_OK", testConf, NewObservability(testConf, nil, nil))

	cols := []athenatypes.ColumnInfo{newColumnInfo("a", "integer"), newColumnInfo("b", "integer")}
	dest := make([]driver.Value, 3)
	r.colMetas = nil

	full := []athenatypes.Datum{{VarCharValue: aws.String("1")}, {VarCharValue: aws.String("2")}}
	assert.Nil(t, r.convertRow(cols, full, dest, testConf))
	assert.Equal(t, []driver.Value{int32(1), int32(2), nil}, dest)

	// Second row has only one datum: columns 2 and 3 must be reset, not stale.
	short := []athenatypes.Datum{{VarCharValue: aws.String("9")}}
	assert.Nil(t, r.convertRow(cols, short, dest, testConf))
	assert.Equal(t, []driver.Value{int32(9), nil, nil}, dest)
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

// TestRows_NilResultSetNormalizedToEmpty pins fetchOnePage's guard against a
// page with no ResultSet/ResultSetMetadata at all (Athena can return this):
// it must normalize to an empty result set, not panic on nil deref.
func TestRows_NilResultSetNormalizedToEmpty(t *testing.T) {
	testConf := NewNoOpsConfig()
	r, e := NewRows(context.Background(), newMockAthenaClient(),
		"nil_resultset",
		testConf, NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	assert.NotNil(t, r)
	assert.NotNil(t, r.ResultOutput.ResultSet)
	assert.NotNil(t, r.ResultOutput.ResultSet.ResultSetMetadata)
	assert.Empty(t, r.Columns())
	dest := make([]driver.Value, 1)
	assert.Equal(t, io.EOF, r.Next(dest))
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

	// an empty page mid-stream must not truncate: every row of every page
	// is returned (5 after the header + 0 + 5 + 5) before the fixture's
	// forced error on the following page.
	r, e = NewRows(context.Background(), newMockAthenaClient(),
		"SELECT_EMPTY_ROW_IN_PAGE",
		testConf, NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	assert.NotNil(t, r)
	rowCount := 0
	for {
		e = r.Next(dest)
		if e != nil {
			assert.Equal(t, ErrTestMockGeneric, e)
			break
		}
		rowCount++
	}
	assert.Equal(t, 15, rowCount)

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

// A page whose columns outnumber a single-datum first row triggers the
// tab-split path; a later row carrying zero datums must be skipped, not
// indexed into.
func TestRows_RaggedTabSplitPage(t *testing.T) {
	testConf := NewNoOpsConfig()
	m := newMockAthenaClient()
	cols := []athenatypes.ColumnInfo{
		newColumnInfo("c1", "string"),
		newColumnInfo("c2", "string"),
		newColumnInfo("c3", "string"),
	}
	m.queryToResultsGenMap["ragged_tab_split"] = singlePage(buildPage(cols,
		[]athenatypes.Row{
			{Data: []athenatypes.Datum{{VarCharValue: aws.String("a\tb\tc")}}},
			{Data: nil}, // no datums at all: used to panic
			{Data: []athenatypes.Datum{{VarCharValue: aws.String("d\te\tf")}}},
		}, -1))

	r, e := NewRows(context.Background(), m, "ragged_tab_split", testConf,
		NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	dest := make([]driver.Value, 3)
	assert.Nil(t, r.Next(dest))
	assert.Equal(t, []driver.Value{"a", "b", "c"}, dest)
	// the empty row survives (no panic) and every slot falls to the
	// configured missing-value policy rather than leaking row 1's values.
	assert.Nil(t, r.Next(dest))
	assert.Equal(t, []driver.Value{"", "", ""}, dest)
	assert.Nil(t, r.Next(dest))
	assert.Equal(t, []driver.Value{"d", "e", "f"}, dest)
}

// An unrecognized column type must not abort the whole result set: it decodes
// as its raw string while its neighbours decode normally.
func TestRows_UnknownColumnTypeFallsBackToString(t *testing.T) {
	testConf := NewNoOpsConfig()
	m := newMockAthenaClient()
	cols := []athenatypes.ColumnInfo{
		newColumnInfo("n", "integer"),
		newColumnInfo("g", "some_future_type"),
		newColumnInfo("s", "varchar"),
	}
	m.queryToResultsGenMap["unknown_col_type"] = singlePage(buildPage(cols,
		[]athenatypes.Row{genRow([]*string{
			aws.String("7"), aws.String("POINT (1 2)"), aws.String("ok"),
		})}, -1))

	r, e := NewRows(context.Background(), m, "unknown_col_type", testConf,
		NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	dest := make([]driver.Value, 3)
	assert.Nil(t, r.Next(dest))
	assert.Equal(t, []driver.Value{int32(7), "POINT (1 2)", "ok"}, dest)
}

// Athena can return row data with no ColumnInfo at all (MSCK REPAIR TABLE).
// Column names must be synthesized rather than the page being left unusable.
func TestRows_NilColumnInfoSynthesizesColumnNames(t *testing.T) {
	testConf := NewNoOpsConfig()
	m := newMockAthenaClient()
	m.queryToResultsGenMap["nil_columninfo"] = singlePage(buildPage(nil,
		[]athenatypes.Row{genRow([]*string{aws.String("x"), aws.String("y")})}, -1))

	r, e := NewRows(context.Background(), m, "nil_columninfo", testConf,
		NewObservability(testConf, nil, nil))
	assert.Nil(t, e)
	assert.Equal(t, []string{"_col0", "_col1"}, r.Columns())
	dest := make([]driver.Value, 2)
	assert.Nil(t, r.Next(dest))
	assert.Equal(t, []driver.Value{"x", "y"}, dest)
}

// Athena can echo back an unchanged NextToken on an empty page. The paginator
// is constructed with StopOnDuplicateToken so fetchNextPage terminates instead
// of calling GetQueryResults forever.
func TestRows_DuplicateNextTokenTerminates(t *testing.T) {
	testConf := NewNoOpsConfig()
	m := newMockAthenaClient()
	m.queryToResultsGenMap["dup_token"] = func(string) (*athena.GetQueryResultsOutput, error) {
		page := buildPage([]athenatypes.ColumnInfo{newColumnInfo("c1", "string")}, nil, -1)
		page.NextToken = aws.String("same-token-forever")
		return page, nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		r, e := NewRows(context.Background(), m, "dup_token", testConf,
			NewObservability(testConf, nil, nil))
		assert.Nil(t, e)
		assert.Equal(t, io.EOF, r.Next(make([]driver.Value, 1)))
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("fetchNextPage did not terminate on a repeated NextToken")
	}
	assert.LessOrEqual(t, m.callCount("GetQueryResults"), 3)
}

// TestRows_MissingAsDefaultMatchesScanType pins the invariant that a
// MissingAsDefault value scans as the same Go type ColumnTypeScanType
// promises (and that a present value parses to).
func TestRows_MissingAsDefaultMatchesScanType(t *testing.T) {
	conf := NewNoOpsConfig()
	conf.MissingAsDefault = true
	conf.MissingAsNil = false
	conf.MissingAsEmptyString = false

	for _, ty := range []string{"tinyint", "smallint", "integer", "bigint",
		"float", "real", "double", "boolean", "timestamp", "varchar"} {
		cols := []athenatypes.ColumnInfo{colInfo("c", ty)}
		r := newRowsWithMetadata("q", nil, cols)
		r.config = conf
		r.tracer = NewObservability(conf, nil, nil)

		dest := make([]driver.Value, 1)
		// no datums -> missing cell -> default value path
		assert.NoError(t, r.convertRow(cols, nil, dest, conf), ty)
		assert.Equal(t, r.ColumnTypeScanType(0), reflect.TypeOf(dest[0]), ty)
	}
}
