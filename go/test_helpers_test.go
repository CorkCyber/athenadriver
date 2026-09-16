// SPDX-License-Identifier: MIT

// This file contains random-data generators and result-page builders used
// only by the driver's _test.go files (rows_test, connection_test,
// mockathenaclient_test, utils_test). It lives in the test scope so the
// production binary does not carry the helpers or their math/rand
// dependency footprint.

package athenadriver

import (
	"database/sql"
	"math"
	"math/rand"
	"strconv"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
)

// mockRowsToSQLRows wraps a sqlmock.Rows in a real *sql.Rows so tests can
// exercise the CSV / pretty-print helpers without touching Athena. Lives
// in the test scope so go-sqlmock is not a production dependency.
func mockRowsToSQLRows(mockRows *sqlmock.Rows) *sql.Rows {
	db, mock, _ := sqlmock.New()
	mock.ExpectQuery("SELECT_OK").WillReturnRows(mockRows)
	rows, _ := db.Query("SELECT_OK")
	return rows
}

func randString(l int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	s := make([]byte, l)
	for i := range l {
		s[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(s)
}

func randomInt64(min int64, max int64) int64 {
	return min + rand.Int63n(max-min)
}

func randInt8() *string {
	s := strconv.Itoa(int(randomInt64(math.MinInt8, math.MaxInt8)))
	return &s
}

func randInt16() *string {
	s := strconv.Itoa(int(randomInt64(math.MinInt16, math.MaxInt16)))
	return &s
}

func randInt() *string {
	s := strconv.Itoa(int(randomInt64(math.MinInt32, math.MaxInt32)))
	return &s
}

func randUInt64() *string {
	s := strconv.FormatUint(rand.Uint64(), 10)
	return &s
}

func randFloat32() *string {
	s := strconv.FormatFloat(rand.Float64(), 'f', 6, 32)
	return &s
}

func randFloat64() *string {
	s := strconv.FormatFloat(rand.Float64(), 'f', 6, 64)
	return &s
}

func randStr() *string {
	s := randString(rand.Intn(10))
	return &s
}

func randBool() *string {
	if rand.Intn(10)%2 == 0 {
		s := "true"
		return &s
	}
	s := "false"
	return &s
}

func randDate() *string {
	min := time.Date(1970, 1, 0, 0, 0, 0, 0, time.UTC).Unix()
	max := time.Date(2070, 1, 0, 0, 0, 0, 0, time.UTC).Unix()
	delta := max - min
	sec := rand.Int63n(delta) + min
	s := time.Unix(sec, 0).Format(DateUniXFormat)
	return &s
}

func randTimeStamp() *string {
	min := time.Date(1970, 1, 0, 0, 0, 0, 0, time.UTC).Unix()
	max := time.Date(2070, 1, 0, 0, 0, 0, 0, time.UTC).Unix()
	delta := max - min
	sec := rand.Int63n(delta) + min
	s := time.Unix(sec, 0).Format(TimestampUniXFormat)
	return &s
}

func genHeaderRow(columns []athenatypes.ColumnInfo) athenatypes.Row {
	colLen := len(columns)
	rData := make([]string, colLen)
	for i := range colLen {
		rData[i] = *columns[i].Name
	}
	return newRow(colLen, rData)
}

// randRow generates a row with random data aligned with type information in
// athenatypes.ColumnInfo.
func randRow(columns []athenatypes.ColumnInfo) athenatypes.Row {
	colLen := len(columns)
	row := athenatypes.Row{Data: make([]athenatypes.Datum, colLen)}
	for j := range colLen {
		if columns[j].Type == nil {
			s := "a\tb"
			row.Data[j] = athenatypes.Datum{VarCharValue: &s}
			continue
		}
		row.Data[j] = athenatypes.Datum{VarCharValue: randDatumFor(*columns[j].Type)}
	}
	return row
}

// randDatumFor returns a random VarCharValue for the given Athena column
// type. Falls back to randStr for any type not specially handled (matches
// the previous switch's default branch).
func randDatumFor(athenaType string) *string {
	switch athenaType {
	case "tinyint":
		return randInt8()
	case "smallint":
		return randInt16()
	case "integer":
		return randInt()
	case "bigint":
		return randUInt64()
	case "float", "real":
		return randFloat32()
	case "double":
		return randFloat64()
	case "boolean":
		return randBool()
	case "date":
		return randDate()
	case "time", "time with time zone", "timestamp", "timestamp with time zone":
		return randTimeStamp()
	}
	return randStr()
}

func missingDataRow(columns []athenatypes.ColumnInfo) athenatypes.Row {
	row := athenatypes.Row{Data: make([]athenatypes.Datum, len(columns))}
	// All cells get a nil VarCharValue to simulate a missing-data response.
	// The original implementation switched on Type, but every branch
	// produced the same Datum, so the switch has been collapsed.
	return row
}

// columnTypes must be Athena column type names (see athenaTypes).
func newHeaderResultPage(columnNames []*string, columnTypes []string, rowsData [][]*string) *athena.GetQueryResultsOutput {
	columns := make([]athenatypes.ColumnInfo, len(columnNames))
	for i := range columnNames {
		columns[i] = newColumnInfo(*columnNames[i], columnTypes[i])
	}
	rowLen := len(rowsData)
	rows := make([]athenatypes.Row, rowLen+1)
	rows[0] = genHeaderRow(columns)
	for i := 1; i < rowLen+1; i++ {
		rows[i] = genRow(rowsData[i-1])
	}
	return &athena.GetQueryResultsOutput{
		NextToken: nil,
		ResultSet: &athenatypes.ResultSet{
			ResultSetMetadata: &athenatypes.ResultSetMetadata{ColumnInfo: columns},
			Rows:              rows,
		},
	}
}

func newRandomHeaderResultPage(columns []athenatypes.ColumnInfo, nextToken *string, rowLen int) *athena.GetQueryResultsOutput {
	rows := make([]athenatypes.Row, rowLen)
	rows[0] = genHeaderRow(columns)
	for i := 1; i < rowLen; i++ {
		rows[i] = randRow(columns)
	}
	return &athena.GetQueryResultsOutput{
		NextToken: nextToken,
		ResultSet: &athenatypes.ResultSet{
			ResultSetMetadata: &athenatypes.ResultSetMetadata{ColumnInfo: columns},
			Rows:              rows,
		},
	}
}

func newRandomHeaderlessResultPage(columns []athenatypes.ColumnInfo, nextToken *string, rowLen int) *athena.GetQueryResultsOutput {
	rows := make([]athenatypes.Row, rowLen)
	for i := range rowLen {
		rows[i] = randRow(columns)
	}
	return &athena.GetQueryResultsOutput{
		NextToken: nextToken,
		ResultSet: &athenatypes.ResultSet{
			ResultSetMetadata: &athenatypes.ResultSetMetadata{ColumnInfo: columns},
			Rows:              rows,
		},
	}
}
