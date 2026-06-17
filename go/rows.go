// SPDX-License-Identifier: MIT

package athenadriver

import (
	"context"
	"database/sql/driver"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"time"

	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/athena"
	athenatypes "github.com/aws/aws-sdk-go-v2/service/athena/types"
)

// Pre-computed reflect.Type tokens for ColumnTypeScanType. Built once at init,
// no runtime reflection.
var (
	scanTypeInt8    = reflect.TypeFor[int8]()
	scanTypeInt16   = reflect.TypeFor[int16]()
	scanTypeInt32   = reflect.TypeFor[int32]()
	scanTypeInt64   = reflect.TypeFor[int64]()
	scanTypeFloat32 = reflect.TypeFor[float32]()
	scanTypeFloat64 = reflect.TypeFor[float64]()
	scanTypeBool    = reflect.TypeFor[bool]()
	scanTypeString  = reflect.TypeFor[string]()
	scanTypeTime    = reflect.TypeFor[time.Time]()
	scanTypeUnknown = reflect.TypeFor[any]()
)

// athenaTypeMeta describes how the driver surfaces a single Athena column
// type to database/sql callers: the reflect.Type returned from
// RowsColumnTypeScanType, and the zero value handed back when
// MissingAsDefault is enabled and a row has no value for the column.
//
// The parse step (string -> Go value) lives in athenaTypeToGoType because
// each Athena type needs its own width/format handling.
type athenaTypeMeta struct {
	scanType   reflect.Type
	defaultVal any
}

// athenaTypes is the single source of truth for Athena column types the
// driver recognizes. ColumnTypeScanType and getDefaultValueForColumnType
// both index into this map; athenaTypeToGoType's parse switch covers the
// same key set. Types listed here as scanTypeString are returned to
// database/sql as Go strings (Athena's text-shaped types and anything the
// driver does not specially decode).
var athenaTypes = map[string]athenaTypeMeta{
	"tinyint":  {scanTypeInt8, 0},
	"smallint": {scanTypeInt16, 0},
	"integer":  {scanTypeInt32, 0},
	"bigint":   {scanTypeInt64, 0},

	"float":  {scanTypeFloat32, 0.0},
	"real":   {scanTypeFloat32, 0.0},
	"double": {scanTypeFloat64, 0.0},

	"boolean": {scanTypeBool, false},

	"date":                     {scanTypeTime, time.Time{}},
	"time":                     {scanTypeTime, time.Time{}},
	"time with time zone":      {scanTypeTime, time.Time{}},
	"timestamp":                {scanTypeTime, time.Time{}},
	"timestamp with time zone": {scanTypeTime, time.Time{}},

	"json":                   {scanTypeString, ""},
	"char":                   {scanTypeString, ""},
	"varchar":                {scanTypeString, ""},
	"varbinary":              {scanTypeString, ""},
	"row":                    {scanTypeString, ""},
	"string":                 {scanTypeString, ""},
	"binary":                 {scanTypeString, ""},
	"struct":                 {scanTypeString, ""},
	"interval year to month": {scanTypeString, ""},
	"interval day to second": {scanTypeString, ""},
	"decimal":                {scanTypeString, ""},
	"ipaddress":              {scanTypeString, ""},
	"array":                  {scanTypeString, ""},
	"map":                    {scanTypeString, ""},
	"unknown":                {scanTypeString, ""},
}

// Rows defines rows in AWS Athena ResultSet.
type Rows struct {
	athena          AthenaClient
	ctx             context.Context
	queryID         string
	reachedLastPage bool
	ResultOutput    *athena.GetQueryResultsOutput
	config          *Config
	tracer          *DriverTracer
	pageCount       int64
	paginator       *athena.GetQueryResultsPaginator
	// queryExecution is the terminal-state QueryExecution returned by
	// GetQueryExecution at the end of the polling loop. Used by the
	// StatementType / SubstatementType accessors and any caller that needs
	// scan-bytes or engine-version metadata. May be nil for Rows returned
	// from cached-query (QID-direct) paths or non-ops helpers.
	queryExecution *athenatypes.QueryExecution
}

// QueryID returns the Athena query execution ID for the rows. Useful for
// logging or follow-up StopQueryExecution / GetQueryExecution calls.
func (r *Rows) QueryID() string { return r.queryID }

// StatementType returns the top-level statement type Athena assigned to the
// query (DML, DDL, UTILITY, ...). Returns "" if the underlying query
// execution metadata is not available (e.g. Rows came from a cached-result
// path).
func (r *Rows) StatementType() string {
	if r.queryExecution == nil {
		return ""
	}
	return string(r.queryExecution.StatementType)
}

// SubstatementType returns Athena's finer-grained classification of the
// statement (SELECT, INSERT, CREATE_TABLE_AS_SELECT, ...). Returns "" if
// the underlying query execution metadata is not available or if Athena did
// not populate the field for this statement.
func (r *Rows) SubstatementType() string {
	if r.queryExecution == nil || r.queryExecution.SubstatementType == nil {
		return ""
	}
	return *r.queryExecution.SubstatementType
}

// NewNonOpsRows is to create a new Rows.
func NewNonOpsRows(ctx context.Context, client AthenaClient, queryID string, driverConfig *Config,
	obs *DriverTracer) (*Rows, error) {
	r := Rows{
		athena:    client,
		ctx:       ctx,
		queryID:   queryID,
		config:    driverConfig,
		tracer:    obs,
		pageCount: -1,
	}
	return &r, nil
}

// NewRows is to create a new Rows.
func NewRows(ctx context.Context, client AthenaClient, queryID string, driverConfig *Config,
	obs *DriverTracer) (*Rows, error) {
	r := Rows{
		athena:    client,
		ctx:       ctx,
		queryID:   queryID,
		config:    driverConfig,
		tracer:    obs,
		pageCount: -1,
		paginator: athena.NewGetQueryResultsPaginator(client, &athena.GetQueryResultsInput{
			QueryExecutionId: aws.String(queryID),
		}),
	}
	if err := r.fetchNextPage(); err != nil {
		return nil, err
	}
	return &r, nil
}

// Columns return Columns metadata.
func (r *Rows) Columns() []string {
	var columns []string
	for _, colInfo := range r.ResultOutput.ResultSet.ResultSetMetadata.ColumnInfo {
		columns = append(columns, *colInfo.Name)
	}
	return columns
}

// ColumnTypeDatabaseTypeName will be called by sql framework.
func (r *Rows) ColumnTypeDatabaseTypeName(index int) string {
	colInfo := r.ResultOutput.ResultSet.ResultSetMetadata.ColumnInfo[index]
	if colInfo.Type == nil {
		// Treat a missing type the same way the sibling ColumnType* methods
		// treat unknown shapes — return a zero value rather than logging an
		// error, since the sql framework calls these on every column and a
		// nil here is benign at the call site.
		return ""
	}
	return *colInfo.Type
}

// ColumnTypeScanType implements driver.RowsColumnTypeScanType. Returns the Go
// type that values from athenaTypeToGoType land in for the given column index.
// database/sql uses this when callers do sql.ColumnType.ScanType().
func (r *Rows) ColumnTypeScanType(index int) reflect.Type {
	colInfo := r.ResultOutput.ResultSet.ResultSetMetadata.ColumnInfo[index]
	if colInfo.Type == nil {
		return scanTypeUnknown
	}
	if meta, ok := athenaTypes[*colInfo.Type]; ok {
		return meta.scanType
	}
	return scanTypeUnknown
}

// ColumnTypeNullable implements driver.RowsColumnTypeNullable. Athena's
// ColumnInfo.Nullable currently always reports UNKNOWN per the SDK docs, so
// this returns ok=false in that case. Wired up so it starts reporting real
// values if/when Athena begins populating the field.
func (r *Rows) ColumnTypeNullable(index int) (nullable, ok bool) {
	colInfo := r.ResultOutput.ResultSet.ResultSetMetadata.ColumnInfo[index]
	switch colInfo.Nullable {
	case athenatypes.ColumnNullableNotNull:
		return false, true
	case athenatypes.ColumnNullableNullable:
		return true, true
	default:
		return false, false
	}
}

// ColumnTypePrecisionScale implements driver.RowsColumnTypePrecisionScale.
// Meaningful for `decimal` columns; returns ok=false otherwise.
func (r *Rows) ColumnTypePrecisionScale(index int) (precision, scale int64, ok bool) {
	colInfo := r.ResultOutput.ResultSet.ResultSetMetadata.ColumnInfo[index]
	if colInfo.Type == nil || *colInfo.Type != "decimal" {
		return 0, 0, false
	}
	return int64(colInfo.Precision), int64(colInfo.Scale), true
}

// Next is to get next result set page.
func (r *Rows) Next(dest []driver.Value) error {
	if r.reachedLastPage {
		return io.EOF
	}
	if len(r.ResultOutput.ResultSet.Rows) == 0 {
		if r.paginator == nil || !r.paginator.HasMorePages() {
			// no paginator (e.g. NewNonOpsRows path) or no more pages — done.
			r.reachedLastPage = true
			return io.EOF
		}

		if err := r.fetchNextPage(); err != nil {
			return err
		}
		if r.reachedLastPage {
			return io.EOF
		}
	}

	// Shift to next row
	cur := r.ResultOutput.ResultSet.Rows[0]
	columns := r.ResultOutput.ResultSet.ResultSetMetadata.ColumnInfo
	if err := r.convertRow(columns, cur.Data, dest, r.config); err != nil {
		return err
	}
	r.ResultOutput.ResultSet.Rows = r.ResultOutput.ResultSet.Rows[1:]
	return nil
}

// fetchNextPage retrieves the next result page via the v2 SDK paginator. The
// paginator carries the NextToken state across calls, so callers don't pass
// a token in.
func (r *Rows) fetchNextPage() error {
	if r.paginator == nil || !r.paginator.HasMorePages() {
		r.reachedLastPage = true
		return nil
	}
	out, err := r.paginator.NextPage(r.ctx)
	if err != nil {
		r.tracer.Scope().Counter(DriverName + ".failure.fetchnextpage.getqueryresults").Inc(1)
		r.tracer.Log(ErrorLevel, "GetQueryResults failed", slog.String("error", err.Error()))
		r.reachedLastPage = true
		return err
	}
	r.ResultOutput = out

	r.pageCount++
	// First row of the first page contains header if the query is not DDL.
	// These are also available in *athenaAPI.Row.ResultSetMetadata.
	// Sometimes Athena go API will return row data without corresponding ColumnInfo. To circumvent this situation,
	// we choose to name the column as `column` + 0-index-based number
	// One example is:
	//   input:
	//      MSCK REPAIR TABLE sampledb.elb_logs
	//   output:
	//     _col0
	//     Partitions not in metastore:    elb_logs:2015/01/01     elb_logs:2015/01/02     elb_logs:2015/01/03
	//       elb_logs:2015/01/04     elb_logs:2015/01/05     elb_logs:2015/01/06     elb_logs:2015/01/07
	if r.ResultOutput != nil &&
		r.ResultOutput.ResultSet.ResultSetMetadata != nil &&
		r.ResultOutput.ResultSet.ResultSetMetadata.ColumnInfo != nil {
		rowLen := len(r.ResultOutput.ResultSet.Rows)
		colLen := len(r.ResultOutput.ResultSet.ResultSetMetadata.ColumnInfo)
		if rowLen > 0 {
			rowColLen := len(r.ResultOutput.ResultSet.Rows[0].Data)
			if colLen < rowColLen {
				for i := range rowColLen - colLen {
					colName := "_col" + strconv.Itoa(i+colLen)
					colType := "string"
					colInfo := newColumnInfo(colName, colType)
					r.ResultOutput.ResultSet.ResultSetMetadata.ColumnInfo = append(r.ResultOutput.ResultSet.ResultSetMetadata.ColumnInfo,
						colInfo)
				}
			} else if colLen > rowColLen && rowColLen == 1 {
				for k := range rowLen {
					items := strings.Split(*r.ResultOutput.ResultSet.Rows[k].Data[0].VarCharValue, "\t")
					if len(items) == colLen {
						for i, v := range items {
							items[i] = strings.TrimSpace(v)
						}
						r.ResultOutput.ResultSet.Rows[k] = newRow(colLen, items)
					}
				}
			}
		} else if rowLen == 0 && colLen == 1 && r.ResultOutput.UpdateCount != nil {
			if *r.ResultOutput.UpdateCount > 0 {
				if *r.ResultOutput.ResultSet.ResultSetMetadata.ColumnInfo[0].Name == "rows" {
					// For DML's INSERT INTO, DDL's CTAS
					updateCount := strconv.FormatInt(*r.ResultOutput.UpdateCount, 10)
					rData := athenatypes.Datum{VarCharValue: &updateCount}
					aRow := athenatypes.Row{Data: []athenatypes.Datum{rData}}
					r.ResultOutput.ResultSet.Rows = append(r.ResultOutput.ResultSet.Rows, aRow)
				}
			}
		}
	}
	var rowOffset = 0
	if r.pageCount == 0 {
		rs := r.ResultOutput.ResultSet
		ci := r.ResultOutput.ResultSet.ResultSetMetadata.ColumnInfo
		i := 0
		if len(ci) > 0 && len(rs.Rows) > 0 && len(rs.Rows[0].Data) > 0 && len(rs.Rows[0].Data) == len(ci) {
			for ; i < len(ci); i++ {
				if rs.Rows[0].Data[i].VarCharValue == nil {
					break
				}
				if *ci[i].Name != *rs.Rows[0].Data[i].VarCharValue {
					break
				}
			}
			if i == len(ci) {
				rowOffset = 1
			}
		}
	}

	// if there is no new row, we should not continue, and this also filters out cases that Rows is nil
	if len(r.ResultOutput.ResultSet.Rows) <= rowOffset {
		r.reachedLastPage = true
		return nil
	}

	r.ResultOutput.ResultSet.Rows = r.ResultOutput.ResultSet.Rows[rowOffset:]
	return nil
}

// Close is to close Rows after reading all data.
func (r *Rows) Close() error {
	if r.paginator != nil && r.paginator.HasMorePages() {
		r.tracer.Log(WarnLevel, "rows close prematurely, queryID: "+r.queryID)
		r.ResultOutput = nil
	}
	r.paginator = nil
	r.reachedLastPage = true
	return nil
}

// convertRow is to convert data from Athena type to Golang SQL type and put them into an array of driver.Value.
func (r *Rows) convertRow(columns []athenatypes.ColumnInfo, rdata []athenatypes.Datum, ret []driver.Value,
	driverConfig *Config) error {
	for i, val := range rdata {
		value, err := r.athenaTypeToGoType(columns[i], val.VarCharValue, driverConfig)
		if err != nil {
			r.tracer.Log(ErrorLevel, "convertrow failed", slog.String("error", err.Error()))
			r.tracer.Scope().Counter(DriverName + ".failure.convertrow").Inc(1)
			return err
		}
		ret[i] = value
	}
	return nil
}

// athenaTypeToGoType converts Athena type to Golang SQL type.
// https://docs.aws.amazon.com/en_pv/athena/latest/ug/data-types.html
// https://docs.aws.amazon.com/athena/latest/ug/geospatial-input-data-formats-supported-geometry-types.html#geometry-data-types
// varbinary is undocumented above, but appears in geo query like:
//
//	SELECT ST_POINT(-74.006801, 40.705220).
//
// json is also undocumented above, but appears here https://docs.aws.amazon.com/athena/latest/ug/querying-JSON.html
// The full list is here: https://prestodb.io/docs/0.172/language/types.html
// Include ipaddress for forward compatibility.
func (r *Rows) athenaTypeToGoType(columnInfo athenatypes.ColumnInfo, rawValue *string, driverConfig *Config) (any, error) {
	if maskedValue, masked := driverConfig.CheckColumnMasked(*columnInfo.Name); masked { // "comma ok" idiom
		return maskedValue, nil
	}
	if rawValue == nil {
		r.tracer.Scope().Counter(DriverName + ".missingvalue").Inc(1)
		r.tracer.Log(ErrorLevel, "missing data",
			slog.String("columnInfo.Name", *columnInfo.Name),
			slog.String("queryID", r.queryID),
			slog.String("workgroup", driverConfig.GetWorkgroup().Name))
		if driverConfig.IsMissingAsNil() {
			return nil, nil
		} else if driverConfig.IsMissingAsEmptyString() {
			return "", nil
		} else if driverConfig.IsMissingAsDefault() {
			return r.getDefaultValueForColumnType(*columnInfo.Type), nil
		}
		r.tracer.Scope().Counter(DriverName + ".failure.convertvalue.config").Inc(1)
		r.tracer.Log(ErrorLevel, "missing data", slog.String("columnInfo.Name", *columnInfo.Name))
		return nil, fmt.Errorf("missing data at column %s", *columnInfo.Name)
	}
	val := *rawValue
	// https://stackoverflow.com/questions/30299649/parse-string-to-specific-type-of-int-int8-int16-int32-int64
	// https://prestodb.io/docs/current/language/types.html#integer
	var err error
	var i int64
	var f float64
	switch *columnInfo.Type {
	case "tinyint":
		// strconv.ParseInt() behavior is to return (int64(0), err)
		// which is not as good as just return (nil, err)
		if i, err = strconv.ParseInt(val, 10, 8); err != nil {
			return nil, err
		}
		return int8(i), nil
	case "smallint":
		if i, err = strconv.ParseInt(val, 10, 16); err != nil {
			return nil, err
		}
		return int16(i), nil
	case "integer":
		if i, err = strconv.ParseInt(val, 10, 32); err != nil {
			return nil, err
		}
		return int32(i), nil
	case "bigint":
		if i, err = strconv.ParseInt(val, 10, 64); err != nil {
			return nil, err
		}
		return i, nil
	case "float", "real":
		if f, err = strconv.ParseFloat(val, 32); err != nil {
			return nil, err
		}
		return float32(f), nil
	case "double":
		if f, err = strconv.ParseFloat(val, 64); err != nil {
			return nil, err
		}
		return f, nil
	// for binary, we assume all chars are 0 or 1; for json,
	// we assume the json syntax is correct. Leave to caller to verify it.
	case "json", "char", "varchar", "varbinary", "row", "string", "binary",
		"struct", "interval year to month", "interval day to second", "decimal",
		"ipaddress", "array", "map", "unknown":
		return val, nil
	case "boolean":
		if val == "true" {
			return true, nil
		} else if val == "false" {
			return false, nil
		}
		r.tracer.Scope().Counter(DriverName + ".failure.convertvalue.boolean").Inc(1)
		r.tracer.Log(ErrorLevel, "boolean data error", slog.String("val", val))
		return nil, fmt.Errorf("unknown value `%s` for boolean", val)
	case "date", "time", "time with time zone", "timestamp", "timestamp with time zone":
		vv, err := scanTime(val)
		if !vv.Valid {
			r.tracer.Scope().Counter(DriverName + ".failure.convertvalue." +
				"time").Inc(1)
			r.tracer.Log(ErrorLevel, "time data error",
				slog.String("val", val),
				slog.String("type", *columnInfo.Type))
			return nil, err
		}
		return vv.Time, err
	default:
		r.tracer.Scope().Counter(DriverName + ".failure.convertvalue.type").Inc(1)
		r.tracer.Log(ErrorLevel, "column data type error", slog.String("columnInfo.Type", *columnInfo.Type))
		return nil, fmt.Errorf("unknown type `%s` with value %s", *columnInfo.Type, val)
	}
}

// getDefaultValueForColumnType is used internally by athenaTypeToGoType to
// get default value for a column type when the row has no value for it and
// MissingAsDefault is enabled. Unknown types log + meter and fall back to
// the empty string.
func (r *Rows) getDefaultValueForColumnType(athenaType string) any {
	if meta, ok := athenaTypes[athenaType]; ok {
		return meta.defaultVal
	}
	r.tracer.Scope().Counter(DriverName + ".failure.defaultvalueforcolumntype.type").Inc(1)
	r.tracer.Log(ErrorLevel, "column data type error", slog.String("columnInfo.Type", athenaType))
	return ""
}
