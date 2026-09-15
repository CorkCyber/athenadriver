// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql/driver"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	"github.com/stretchr/testify/assert"
)

// Athena accepts ExecutionParameters for CTAS but not for CREATE VIEW ... AS
// SELECT, which the old bare-`as` heuristic wrongly matched.
func TestExecutionParamsSupported_CreateViewVsCTAS(t *testing.T) {
	assert.True(t, executionParamsSupported("CREATE TABLE t AS SELECT * FROM t2 WHERE x = ?"))
	assert.False(t, executionParamsSupported("CREATE VIEW v AS SELECT * FROM t WHERE x = ?"))
	assert.False(t, executionParamsSupported("CREATE OR REPLACE VIEW v AS SELECT ?"))
	assert.False(t, executionParamsSupported("create"))

	c := newTestConnWithWG(func(cfg *Config, _ *mockAthenaClient) {
		cfg.WorkGroup = nil
	})
	nm := c.athenaClient.(*mockAthenaClient)

	// CVAS: interpolated client-side, submitted parameterless.
	//nolint:errcheck // mock has no canned result for these queries
	_, _ = c.QueryContext(context.Background(), "CREATE VIEW v AS SELECT * FROM t WHERE x = ?",
		[]driver.NamedValue{{Value: "a"}})
	assert.Equal(t, "CREATE VIEW v AS SELECT * FROM t WHERE x = 'a'", *nm.lastStartInput.QueryString)
	assert.Nil(t, nm.lastStartInput.ExecutionParameters)

	// CTAS: still routed through ExecutionParameters.
	_, _ = c.QueryContext(context.Background(), "CREATE TABLE t AS SELECT * FROM t2 WHERE x = ?",
		[]driver.NamedValue{{Value: "a"}})
	assert.Equal(t, "CREATE TABLE t AS SELECT * FROM t2 WHERE x = ?", *nm.lastStartInput.QueryString)
	assert.Equal(t, []string{"'a'"}, nm.lastStartInput.ExecutionParameters)
}

func TestColsToCSV(t *testing.T) {
	sqlRows := sqlmock.NewRows([]string{"one", "two", "three"})
	rows := mockRowsToSQLRows(sqlRows)
	expected := ColsToCSV(rows)
	assert.Equal(t, "one,two,three\n", expected)
	assert.Equal(t, "", ColsToCSV(nil))
	assert.Equal(t, "one\n", ColsToCSV(mockRowsToSQLRows(sqlmock.NewRows([]string{"one"}))))
	assert.Equal(t, "", ColsToCSV(mockRowsToSQLRows(sqlmock.NewRows(nil))))
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

	for _, ty := range []string{"tinyint", "smallint", "integer", "bigint",
		"float", "real", "double", "json", "char", "varchar", "varbinary",
		"row", "string", "binary", "struct", "interval year to month",
		"interval day to second", "decimal", "ipaddress", "array", "map",
		"unknown", "boolean", "date", "time", "time with time zone",
		"timestamp with time zone", "timestamp", "weird_type"} {
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

	testConf := &ServiceLimitOverride{DMLQueryTimeout: 65 * 60}
	assert.False(t, isQueryTimeOut(OneHourAgo, athenatypes.StatementTypeDml, testConf))

	testConf.DDLQueryTimeout = 30 * 60
	assert.True(t, isQueryTimeOut(OneHourAgo, athenatypes.StatementTypeDdl, testConf))
	assert.True(t, isQueryTimeOut(OneHourAgo, "UNKNOWN", testConf))
}

// TestEscapeQuotes pins the Athena/Trino escaping rules: ONLY a single quote
// is escaped (by doubling). Trino does not interpret backslash escapes inside
// string literals, so every byte the old MySQL-style escaper rewrote must now
// round-trip verbatim.
func TestEscapeQuotes(t *testing.T) {
	// Single quotes can be escaped by adding another single quote.
	// https://docs.aws.amazon.com/athena/latest/ug/select.html#select-escaping
	assert.Equal(t, `''`, escapeQuotes(`'`))
	assert.Equal(t, `a''''b`, escapeQuotes(`a''b`))

	// Everything the MySQL-style escaper used to mangle is passed through.
	assert.Equal(t, "\x00", escapeQuotes("\x00"))
	assert.Equal(t, "\n", escapeQuotes("\n"))
	assert.Equal(t, "\r", escapeQuotes("\r"))
	assert.Equal(t, "\x1a", escapeQuotes("\x1a"))
	assert.Equal(t, `"`, escapeQuotes(`"`))
	assert.Equal(t, `\`, escapeQuotes(`\`))
	assert.Equal(t, `C:\path\n`, escapeQuotes(`C:\path\n`))
	assert.Equal(t, `x`, escapeQuotes(`x`))
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

// TestUtils_IsQID pins IsQID to Athena's documented QueryExecutionId
// contract (1-128 non-whitespace chars, https://docs.aws.amazon.com/athena/latest/APIReference/API_StartQueryExecution.html),
// not an assumed UUID shape: anything with whitespace or over 128 chars is
// rejected as "definitely not a QueryExecutionId", but a short or
// oddly-shaped token is accepted since AWS's contract allows it.
func TestUtils_IsQID(t *testing.T) {
	assert.True(t, IsQID("a44f8e61-4cbb-429a-b7ab-bea2c4a5caed"))
	assert.True(t, IsQID("a44f8e61-4cbb-429a-b7ab-bea2c4a5caeD"))
	assert.True(t, IsQID("a44f8e61"))

	// Real SQL almost always contains whitespace; that's what actually
	// distinguishes it from a QueryExecutionId, not a UUID assumption.
	assert.False(t, IsQID(`select "a44f8e61-4cbb-429a-b7ab-bea2c4a5caed"`))
	assert.False(t, IsQID("select 1"))

	// Length bound from the contract: 128 chars is the max.
	assert.True(t, IsQID(strings.Repeat("a", 128)))
	assert.False(t, IsQID(strings.Repeat("a", 129)))
	assert.False(t, IsQID(""))
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
			name:     "Quotes doubled, other bytes verbatim",
			input:    "Athena's query's param\n",
			expected: "'Athena''s query''s param\n'",
		},
		{
			name:     "Injection payload becomes an inert literal",
			input:    "1' OR '1'='1' --",
			expected: "'1'' OR ''1''=''1'' --'",
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
			expected: []byte("''"),
		},
		{
			name:     "No special characters",
			input:    []byte("This is a description string with no special characters"),
			expected: []byte("'This is a description string with no special characters'"),
		},
		{
			// The old `_binary'...'` prefix was MySQL syntax; Trino/Athena
			// rejects it outright.
			name:     "Quotes doubled, other bytes verbatim",
			input:    []byte("Athena's query's param\n"),
			expected: []byte("'Athena''s query''s param\n'"),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, FormatBytes(tc.input))
		})
	}
}

// TestSQLOperationName_CardinalityLeaks pins the exact leak vectors round-2
// tracing review found: sqlcommenter-style leading query comments (a real,
// common ORM/OTel-integration practice) and a stray leading paren must
// return "" for db.operation.name, not the raw token.
func TestSQLOperationName_CardinalityLeaks(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"plain select", "select 1", "SELECT"},
		{"sqlcommenter leading comment", "/*traceparent='00-abc-01'*/ select 1", ""},
		{"stray leading paren", "(select 1) union (select 2)", ""},
		{"bare query id", "a44f8e61-4cbb-429a-b7ab-bea2c4a5caed", ""},
		{"garbage token", "!!!not sql", ""},
		{"unrecognized verb", "vacuum-ish nonsense", ""},
		{"empty", "", ""},
		{"whitespace only", "   \t\n  ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, sqlOperationName(tc.query))
		})
	}
}

// FuzzSQLOperationName is a chaos/completeness check: whatever garbage a
// caller's query builder produces, sqlOperationName must stay within the
// low-cardinality contract db.operation.name requires: either "" or one of
// the fixed allowlisted verbs, never an arbitrary pass-through token.
func FuzzSQLOperationName(f *testing.F) {
	seeds := []string{
		"select 1",
		"/*traceparent=00-x-01*/ select 1",
		"(select 1)",
		"a44f8e61-4cbb-429a-b7ab-bea2c4a5caed",
		"",
		"   ",
		"SELECT'; DROP TABLE x; --",
		"select\x00null\x00byte",
		"日本語 select",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, query string) {
		got := sqlOperationName(query)
		if got == "" {
			return
		}
		if !athenaStatementVerbs[got] {
			t.Fatalf("sqlOperationName(%q) = %q, not in the allowlist", query, got)
		}
	})
}

// FuzzDBSpanNameAndOperation chaos-tests the span-name/operation contract
// directly: no fuzzed query, under any pseudo-command, may put a UUID (or
// any non-allowlisted token) into the span name or db.operation.name.
func FuzzDBSpanNameAndOperation(f *testing.F) {
	const uuid = "a44f8e61-4cbb-429a-b7ab-bea2c4a5caed"
	seeds := []string{
		"select 1",
		uuid,
		"/*trace*/ select 1",
		"pc:get_query_id " + uuid,
		"",
	}
	for _, pc := range []string{"", PCGetQID, PCGetQIDStatus, PCStopQID} {
		for _, s := range seeds {
			f.Add(pc, s, "mydb")
		}
	}
	f.Fuzz(func(t *testing.T, pseudoCommand, query, database string) {
		_, op := dbSpanNameAndOperation(pseudoCommand, query, database)
		if op != "" && op != "GET_QUERY_ID" && op != "GET_QUERY_ID_STATUS" && op != "STOP_QUERY_ID" && !athenaStatementVerbs[op] {
			t.Fatalf("dbSpanNameAndOperation(%q, %q, %q) op = %q, not a recognized low-cardinality value",
				pseudoCommand, query, database, op)
		}
		// op must never equal the raw query when the query is QID-shaped.
		// spanName is not checked the same way: it legitimately embeds the
		// (separately fuzzed) database name, which can coincidentally
		// collide with a degenerate query on repeated-character inputs.
		if IsQID(query) && strings.EqualFold(op, query) {
			t.Fatalf("dbSpanNameAndOperation(%q, %q, %q) leaked the query ID into op=%q",
				pseudoCommand, query, database, op)
		}
	})
}
