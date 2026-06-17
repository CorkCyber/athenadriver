// SPDX-License-Identifier: MIT

package athenadriver

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
	"encoding/csv"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
)

// ColsToCSV is a convenient function to convert columns of sql.Rows to CSV format.
func ColsToCSV(rows *sql.Rows) string {
	if rows == nil {
		return ""
	}
	columns, _ := rows.Columns()
	var s strings.Builder
	for i, v := range columns {
		s.WriteString(v)
		if i != len(columns)-1 {
			s.WriteString(",")
		} else {
			s.WriteString("\n")
		}
	}
	return s.String()
}

// RowsToCSV is to convert rows of sql.Rows to CSV format.
func RowsToCSV(rows *sql.Rows) string {
	if rows == nil {
		return ""
	}
	columns, _ := rows.Columns()
	var buf bytes.Buffer
	csvWriter := csv.NewWriter(&buf)
	records := make([][]string, 0)
	for rows.Next() {
		rawResult := make([][]byte, len(columns))
		row := make([]any, len(columns))
		for i := range rawResult {
			row[i] = &rawResult[i] // pointers to each string in the interface slice
		}
		// We don't consider malformed rows
		_ = rows.Scan(row...)
		s := make([]string, len(columns))
		for i, cell := range rawResult {
			s[i] = string(cell)
		}
		records = append(records, s)
	}
	csvWriter.WriteAll(records)
	return buf.String()
}

// ColsRowsToCSV is a convenient function to convert columns and rows of sql.Rows to CSV format.
func ColsRowsToCSV(rows *sql.Rows) string {
	s := ColsToCSV(rows)
	r := RowsToCSV(rows)
	return s + r
}

// readOnlyPrefixes lists the lowercase query prefixes the driver treats as
// read-only when read-only mode is enabled. Athena query IDs (UUIDs) are
// also accepted as read-only via IsQID.
var readOnlyPrefixes = []string{"select", "using", "with", "desc", "show"}

func isReadOnlyStatement(query string) bool {
	nQuery := strings.TrimSpace(strings.ToLower(query))
	for _, p := range readOnlyPrefixes {
		if strings.HasPrefix(nQuery, p) {
			return true
		}
	}
	return IsQID(query)
}

func newColumnInfo(colName string, colType any) athenatypes.ColumnInfo {
	catalogName := "hive"
	schemaName := ""
	tableName := ""
	if colType == nil {
		return athenatypes.ColumnInfo{
			CaseSensitive: false,
			CatalogName:   &catalogName,
			Label:         &colName,
			Name:          &colName,
			Nullable:      athenatypes.ColumnNullableUnknown,
			Precision:     19,
			Scale:         0,
			SchemaName:    &schemaName,
			TableName:     &tableName,
			Type:          nil,
		}
	}
	ct := colType.(string)
	return athenatypes.ColumnInfo{
		CaseSensitive: false,
		CatalogName:   &catalogName,
		Label:         &colName,
		Name:          &colName,
		Nullable:      athenatypes.ColumnNullableNullable,
		Precision:     19,
		Scale:         0,
		SchemaName:    &schemaName,
		TableName:     &tableName,
		Type:          &ct,
	}
}

func newRow(colLen int, rData []string) athenatypes.Row {
	var nData = make([]athenatypes.Datum, colLen)
	for i := range colLen {
		nData[i] = athenatypes.Datum{VarCharValue: &rData[i]}
	}
	return athenatypes.Row{
		Data: nData,
	}
}

func genRow(rowData []*string) athenatypes.Row {
	row := athenatypes.Row{
		Data: make([]athenatypes.Datum, len(rowData)),
	}
	for i := range rowData {
		row.Data[i] = athenatypes.Datum{VarCharValue: rowData[i]}
	}
	return row
}

func newHeaderlessResultPage(columnNames []string, columnTypes []string, rowsData [][]*string) *athena.GetQueryResultsOutput {
	columns := make([]athenatypes.ColumnInfo, len(columnNames))
	for i := range columnNames {
		columns[i] = newColumnInfo(columnNames[i], columnTypes[i])
	}
	rowLen := len(rowsData)
	rows := make([]athenatypes.Row, rowLen)
	for i := range rowLen {
		rows[i] = genRow(rowsData[i])
	}
	return &athena.GetQueryResultsOutput{
		NextToken: nil,
		ResultSet: &athenatypes.ResultSet{
			ResultSetMetadata: &athenatypes.ResultSetMetadata{
				ColumnInfo: columns,
			},
			Rows: rows,
		},
	}
}

// escapeBytesBackslash escapes []byte with backslashes (\)
// This escapes the contents of a string (provided as []byte) by adding backslashes before special
// characters, and turning others into specific escape sequences, such as
// turning newlines into \n and null bytes into \0.
//
// \xNN notation to define a string constant holding some peculiar byte values.
// (Of course, bytes range from hexadecimal values 00 through FF, inclusive.)
func escapeBytesBackslash(buf, v []byte) []byte {
	pos := len(buf)
	buf = reserveBuffer(buf, len(v)*2)

	for _, c := range v {
		switch c {
		case '\x00':
			buf[pos] = '\\'
			buf[pos+1] = '0'
			pos += 2
		case '\n':
			buf[pos] = '\\'
			buf[pos+1] = 'n'
			pos += 2
		case '\r':
			buf[pos] = '\\'
			buf[pos+1] = 'r'
			pos += 2
		case '\x1a':
			buf[pos] = '\\'
			buf[pos+1] = 'Z'
			pos += 2
		case '\'':
			// Single quotes can be escaped by adding another single quote.
			// https://docs.aws.amazon.com/athena/latest/ug/select.html
			// https://docs.aws.amazon.com/athena/latest/ug/data-types.html#data-types-considerations
			buf[pos] = '\''
			buf[pos+1] = '\''
			pos += 2
		case '"':
			buf[pos] = '\\'
			buf[pos+1] = '"'
			pos += 2
		case '\\':
			buf[pos] = '\\'
			buf[pos+1] = '\\'
			pos += 2
		default:
			buf[pos] = c
			pos++
		}
	}

	return buf[:pos]
}

// escapeStringBackslash is similar to escapeBytesBackslash but for string.
func escapeStringBackslash(buf []byte, v string) []byte {
	return escapeBytesBackslash(buf, []byte(v))
}

// reserveBuffer checks cap(buf) and expand buffer to len(buf) + appendSize.
// If cap(buf) is not enough, reallocate new buffer.
func reserveBuffer(buf []byte, appendSize int) []byte {
	newSize := len(buf) + appendSize
	if cap(buf) < newSize {
		// Grow buffer exponentially
		newBuf := make([]byte, len(buf)*2+appendSize)
		copy(newBuf, buf)
		buf = newBuf
	}
	return buf[:newSize]
}

func namedValueToValue(named []driver.NamedValue) []driver.Value {
	args := make([]driver.Value, len(named))
	for n, param := range named {
		args[n] = param.Value
	}
	return args
}

func valueToNamedValue(args []driver.Value) []driver.NamedValue {
	nameValues := make([]driver.NamedValue, len(args))
	for i := range args {
		nameValues[i].Value = args[i]
		nameValues[i].Ordinal = i + 1
	}
	return nameValues
}

func isQueryTimeOut(startOfStartQueryExecution time.Time, queryType athenatypes.StatementType, serviceLimitOverride *ServiceLimitOverride) bool {
	ddlQueryTimeout := DDLQueryTimeout
	dmlQueryTimeout := DMLQueryTimeout
	if serviceLimitOverride != nil {
		if serviceLimitOverride.GetDDLQueryTimeout() > 0 {
			ddlQueryTimeout = serviceLimitOverride.GetDDLQueryTimeout()
		}
		if serviceLimitOverride.GetDMLQueryTimeout() > 0 {
			dmlQueryTimeout = serviceLimitOverride.GetDMLQueryTimeout()
		}
	}
	switch queryType {
	case "DDL":
		return time.Since(startOfStartQueryExecution) >
			time.Duration(ddlQueryTimeout)*time.Second
	case "DML":
		return time.Since(startOfStartQueryExecution) >
			time.Duration(dmlQueryTimeout)*time.Second
	case "UTILITY":
		return time.Since(startOfStartQueryExecution) >
			time.Duration(dmlQueryTimeout)*time.Second
	case "TIMEOUT_NOW":
		return true
	default:
		return time.Since(startOfStartQueryExecution) >
			time.Duration(ddlQueryTimeout)*time.Second
	}
}

// isQueryValid is to check the validity of Query, now only string length check.
// https://docs.aws.amazon.com/athena/latest/ug/service-limits.html
func isQueryValid(query string) bool {
	return len(query) < MAXQueryStringLength && len(query) > 4
}

// GetFromEnvVal is to get environmental variable value by keys.
// The return value is from whichever key is set according to the order in the slice.
func GetFromEnvVal(keys []string) string {
	for _, k := range keys {
		if v := os.Getenv(k); len(v) != 0 {
			return v
		}
	}
	return ""
}

// printCost prints the estimated USD cost of the query represented by the
// given GetQueryExecution output, using region-specific Athena pricing.
// region is the AWS region the query ran in; an empty / unknown region
// falls back to the default $5/TB.
func printCost(region string, o *athena.GetQueryExecutionOutput) {
	qid := "NA"
	var bytes int64
	if o != nil && o.QueryExecution != nil {
		if o.QueryExecution.QueryExecutionId != nil {
			qid = *o.QueryExecution.QueryExecutionId
		}
		if o.QueryExecution.Statistics != nil && o.QueryExecution.Statistics.DataScannedInBytes != nil {
			bytes = *o.QueryExecution.Statistics.DataScannedInBytes
		}
	}
	fmt.Printf("query cost: %.20f USD, scanned data: %d B, qid: %s\n",
		estimateScanCost(region, bytes), bytes, qid)
}

var qIDPattern = regexp.MustCompile(`^[0-9a-f-]{36}$`)

// IsQID is to check if a query string is a Query ID
// the hexadecimal Athena query ID like a44f8e61-4cbb-429a-b7ab-bea2c4a5caed
// https://aws.amazon.com/premiumsupport/knowledge-center/access-download-athena-query-results/
func IsQID(q string) bool {
	return qIDPattern.MatchString(q)
}

// FormatString formats a string type query argument for Athena by escaping special characters and surrounding the
// string with single quotes. Using FormatString allows for selective formatting of the query argument, if
// typecasting or function calls are part of the query argument.
//
// Example usage:
// query := "SELECT * FROM my_table WHERE description = ? AND created > ?"
//
//	args := []any{
//		 aws.String(athenadriver.FormatString("The bunny's eating a carrot")),
//		 aws.String(fmt.Sprintf("TIMESTAMP %s", athenadriver.FormatString("2024-07-01 00:00:00")))
//	}
func FormatString(v string) string {
	return fmt.Sprintf("'%s'", escapeBytesBackslash([]byte{}, []byte(v)))
}

// FormatBytes formats a byte slice query argument for Athena by escaping special characters and surrounding it with
// single quotes.
func FormatBytes(v []byte) []byte {
	buf := append([]byte{}, "_binary'"...)
	buf = escapeBytesBackslash(buf, v)
	buf = append(buf, '\'')
	return buf
}
