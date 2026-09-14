// SPDX-License-Identifier: MIT

package athenadriver

import (
	"database/sql"
	"database/sql/driver"
	"encoding/csv"
	"fmt"
	"os"
	"regexp"
	"slices"
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

// RowsToCSV is to convert rows of sql.Rows to CSV format. Rows are
// streamed to the underlying csv.Writer as they are scanned so peak
// memory stays O(row_width) instead of O(rows).
func RowsToCSV(rows *sql.Rows) string {
	if rows == nil {
		return ""
	}
	columns, _ := rows.Columns()
	var buf strings.Builder
	csvWriter := csv.NewWriter(&buf)
	rawResult := make([]sql.RawBytes, len(columns))
	row := make([]any, len(columns))
	for i := range rawResult {
		row[i] = &rawResult[i]
	}
	s := make([]string, len(columns))
	for rows.Next() {
		// malformed rows are not surfaced
		_ = rows.Scan(row...)
		for i, cell := range rawResult {
			s[i] = string(cell)
		}
		_ = csvWriter.Write(s)
	}
	csvWriter.Flush()
	return buf.String()
}

// ColsRowsToCSV is a convenient function to convert columns and rows of sql.Rows to CSV format.
func ColsRowsToCSV(rows *sql.Rows) string {
	s := ColsToCSV(rows)
	r := RowsToCSV(rows)
	return s + r
}

// workgroupName returns the Config's workgroup name, or "" when no
// workgroup is attached. Convenient for log attributes.
func workgroupName(c *Config) string {
	if c.WorkGroup == nil {
		return ""
	}
	return c.WorkGroup.Name
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

// executionParamsSupported reports whether Athena's StartQueryExecution
// accepts `?` placeholders via ExecutionParameters for this statement.
// Athena supports execution parameters only for SELECT, INSERT INTO,
// CTAS (CREATE TABLE ... AS SELECT), UNLOAD and WITH statements;
// parameterized DDL (ALTER, DROP, MSCK, plain CREATE, ...) is rejected
// server-side and must be interpolated client-side before submission.
func executionParamsSupported(query string) bool {
	fields := strings.Fields(strings.ToLower(query))
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "select", "insert", "unload", "with":
		return true
	case "create":
		// Only the CTAS form takes execution parameters; a bare AS
		// token (reserved word, so never a column/table name) ahead of
		// the SELECT body distinguishes it from plain CREATE TABLE DDL.
		return slices.Contains(fields[1:], "as")
	}
	return false
}

func newColumnInfo(colName string, colType any) athenatypes.ColumnInfo {
	catalogName := "hive"
	schemaName := ""
	tableName := ""
	ci := athenatypes.ColumnInfo{
		CaseSensitive: false,
		CatalogName:   &catalogName,
		Label:         &colName,
		Name:          &colName,
		Nullable:      athenatypes.ColumnNullableUnknown,
		Precision:     19,
		Scale:         0,
		SchemaName:    &schemaName,
		TableName:     &tableName,
	}
	// nil colType means "type unreported" (test fixtures only).
	if ct, ok := colType.(string); ok {
		ci.Nullable = athenatypes.ColumnNullableNullable
		ci.Type = &ct
	}
	return ci
}

func newRow(colLen int, rData []string) athenatypes.Row {
	ptrs := make([]*string, colLen)
	for i := range colLen {
		ptrs[i] = &rData[i]
	}
	return genRow(ptrs)
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

// escapeQuotes escapes v for use inside an Athena/Trino single-quoted string
// literal. Doubling an embedded single quote is the ONLY correct transform:
// Trino does not interpret backslash escapes inside string literals, so the
// MySQL-style \n / \0 / \\ / \" / \r / \Z escapes this used to apply corrupted
// the value instead of protecting it (a literal backslash or newline round
// tripped wrong). Every other byte — NUL included — is carried through as is;
// the value never leaves a quoted context.
// https://docs.aws.amazon.com/athena/latest/ug/select.html
func escapeQuotes(v string) string {
	return strings.ReplaceAll(v, "'", "''")
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
		if v := serviceLimitOverride.DDLQueryTimeout; v > 0 {
			ddlQueryTimeout = v
		}
		if v := serviceLimitOverride.DMLQueryTimeout; v > 0 {
			dmlQueryTimeout = v
		}
	}
	timeout := ddlQueryTimeout
	if queryType == "DML" || queryType == "UTILITY" {
		timeout = dmlQueryTimeout
	}
	return time.Since(startOfStartQueryExecution) > time.Duration(timeout)*time.Second
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
	// MoneyWise deliberately prints to stdout (a CLI-facing feature since
	// v1), not through the slog pipeline — the default driver logger may
	// be discarded while the user still wants cost output. 10 fractional
	// digits covers a single byte scanned in the priciest region;
	// anything past ~15 significant digits is float64 noise anyway.
	fmt.Printf("query cost: %.10f USD, scanned data: %d B, qid: %s\n",
		estimateScanCost(region, bytes), bytes, qid)
}

var qIDPattern = regexp.MustCompile(`^[0-9a-f-]{36}$`)

// IsQID is to check if a query string is a Query ID
// the hexadecimal Athena query ID like a44f8e61-4cbb-429a-b7ab-bea2c4a5caed
// https://aws.amazon.com/premiumsupport/knowledge-center/access-download-athena-query-results/
func IsQID(q string) bool {
	return qIDPattern.MatchString(q)
}

// Raw is a query argument that reaches Athena verbatim: it is NOT quoted or
// escaped by the driver. Use it — and only it — when the argument has to be a
// SQL expression rather than a value, such as a typecast literal:
//
//	db.Query("SELECT * FROM t WHERE created > ?",
//		athenadriver.Raw("TIMESTAMP "+athenadriver.FormatString("2024-07-01 00:00:00")))
//
// Plain string / []byte arguments are quoted and escaped automatically, so
// anything derived from untrusted input must NOT be wrapped in Raw.
type Raw string

// FormatString quotes and escapes a string as an Athena string literal. Plain
// string arguments are formatted this way automatically; FormatString is for
// building a Raw expression that embeds a literal (see Raw).
func FormatString(v string) string {
	return "'" + escapeQuotes(v) + "'"
}

// FormatBytes quotes and escapes a byte slice as an Athena string literal.
// Plain []byte arguments are formatted this way automatically.
func FormatBytes(v []byte) []byte {
	return []byte(FormatString(string(v)))
}
