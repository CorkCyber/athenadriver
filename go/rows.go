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
// RowsColumnTypeScanType, the zero value handed back when
// MissingAsDefault is enabled and a row has no value for the column, and
// the parse step (string -> Go value).
type athenaTypeMeta struct {
	scanType   reflect.Type
	defaultVal any
	parse      func(string) (any, error)
}

// athenaTypes is the single source of truth for Athena column types the
// driver recognizes. ColumnTypeScanType and the per-column decoder cache
// (colMetas) both index into this map. Types listed here as scanTypeString are returned to
// database/sql as Go strings (Athena's text-shaped types and anything the
// driver does not specially decode).
func parseIntBits(bits int) func(string) (any, error) {
	return func(s string) (any, error) {
		n, err := strconv.ParseInt(s, 10, bits)
		if err != nil {
			return nil, err
		}
		switch bits {
		case 8:
			return int8(n), nil
		case 16:
			return int16(n), nil
		case 32:
			return int32(n), nil
		}
		return n, nil
	}
}

func parseFloatBits(bits int) func(string) (any, error) {
	return func(s string) (any, error) {
		f, err := strconv.ParseFloat(s, bits)
		if err != nil {
			return nil, err
		}
		if bits == 32 {
			return float32(f), nil
		}
		return f, nil
	}
}

func parseBool(s string) (any, error) {
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	return nil, fmt.Errorf("unknown value `%s` for boolean", s)
}

func parseTime(s string) (any, error) {
	vv, err := scanTime(s)
	if !vv.Valid {
		return nil, err
	}
	return vv.Time, err
}

func parseString(s string) (any, error) { return s, nil }

var timeMeta = athenaTypeMeta{scanTypeTime, time.Time{}, parseTime}
var stringMeta = athenaTypeMeta{scanTypeString, "", parseString}

var athenaTypes = map[string]athenaTypeMeta{
	"tinyint":  {scanTypeInt8, 0, parseIntBits(8)},
	"smallint": {scanTypeInt16, 0, parseIntBits(16)},
	"integer":  {scanTypeInt32, 0, parseIntBits(32)},
	"bigint":   {scanTypeInt64, 0, parseIntBits(64)},

	"float":  {scanTypeFloat32, 0.0, parseFloatBits(32)},
	"real":   {scanTypeFloat32, 0.0, parseFloatBits(32)},
	"double": {scanTypeFloat64, 0.0, parseFloatBits(64)},

	"boolean": {scanTypeBool, false, parseBool},

	"date":                     timeMeta,
	"time":                     timeMeta,
	"time with time zone":      timeMeta,
	"timestamp":                timeMeta,
	"timestamp with time zone": timeMeta,

	"json":                   stringMeta,
	"char":                   stringMeta,
	"varchar":                stringMeta,
	"varbinary":              stringMeta,
	"row":                    stringMeta,
	"string":                 stringMeta,
	"binary":                 stringMeta,
	"struct":                 stringMeta,
	"interval year to month": stringMeta,
	"interval day to second": stringMeta,
	"decimal":                stringMeta,
	"ipaddress":              stringMeta,
	"array":                  stringMeta,
	"map":                    stringMeta,
	"unknown":                stringMeta,
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
	// colMetas is a per-column decoder lookup indexed by column position,
	// built once from the first page's ColumnInfo. Avoids a per-cell
	// map lookup + string-switch on *columnInfo.Type. nil entries fall
	// back to the "unknown type" error path.
	colMetas []*athenaTypeMeta
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
		columns = append(columns, aws.ToString(colInfo.Name))
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
// type that values from convertCell land in for the given column index.
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

// fetchNextPage retrieves the next result page carrying at least one usable
// row. A page can come back empty mid-stream while the paginator still has a
// NextToken, so keep pulling until rows arrive or the pages run out —
// stopping at the first empty page would silently truncate the result set.
func (r *Rows) fetchNextPage() error {
	for {
		if r.paginator == nil || !r.paginator.HasMorePages() {
			r.reachedLastPage = true
			return nil
		}
		if err := r.fetchOnePage(); err != nil {
			return err
		}
		if len(r.ResultOutput.ResultSet.Rows) > 0 {
			return nil
		}
	}
}

// fetchOnePage pulls exactly one page via the v2 SDK paginator and normalizes
// it. The paginator carries the NextToken state across calls, so callers don't
// pass a token in. An empty page leaves Rows empty; only fetchNextPage decides
// whether that means end-of-stream.
func (r *Rows) fetchOnePage() error {
	out, err := r.paginator.NextPage(r.ctx)
	if err != nil {
		r.tracer.Scope().Counter(DriverName + ".failure.fetchnextpage.getqueryresults").Inc(1)
		r.tracer.Log(ErrorLevel, "GetQueryResults failed", slog.String("error", err.Error()))
		r.reachedLastPage = true
		return err
	}
	// Athena can hand back a page with no ResultSet / ResultSetMetadata at
	// all. Normalize it to an empty result set here — the single point every
	// path converges on — so Columns/Next/ColumnType* can deref freely.
	if out == nil || out.ResultSet == nil || out.ResultSet.ResultSetMetadata == nil {
		r.ResultOutput = &athena.GetQueryResultsOutput{
			ResultSet: &athenatypes.ResultSet{
				ResultSetMetadata: &athenatypes.ResultSetMetadata{},
			},
		}
		return nil
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
	if r.ResultOutput.ResultSet.ResultSetMetadata.ColumnInfo != nil {
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
					items := strings.Split(aws.ToString(r.ResultOutput.ResultSet.Rows[k].Data[0].VarCharValue), "\t")
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
				if aws.ToString(r.ResultOutput.ResultSet.ResultSetMetadata.ColumnInfo[0].Name) == "rows" {
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
		ci := rs.ResultSetMetadata.ColumnInfo
		i := 0
		if len(ci) > 0 && len(rs.Rows) > 0 && len(rs.Rows[0].Data) > 0 && len(rs.Rows[0].Data) == len(ci) {
			for ; i < len(ci); i++ {
				if rs.Rows[0].Data[i].VarCharValue == nil {
					break
				}
				if aws.ToString(ci[i].Name) != *rs.Rows[0].Data[i].VarCharValue {
					break
				}
			}
			if i == len(ci) {
				rowOffset = 1
			}
		}
	}

	// no usable row on this page (also covers Rows being nil); leave Rows
	// empty and let fetchNextPage decide whether more pages remain.
	if len(r.ResultOutput.ResultSet.Rows) <= rowOffset {
		r.ResultOutput.ResultSet.Rows = nil
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

// buildColMetas populates the per-column decoder cache from the current
// page's ColumnInfo. Called once per Rows the first time a decode
// happens (colMetas is nil).
func (r *Rows) buildColMetas(columns []athenatypes.ColumnInfo) {
	r.colMetas = make([]*athenaTypeMeta, len(columns))
	for i, c := range columns {
		if c.Type == nil {
			continue
		}
		if m, ok := athenaTypes[*c.Type]; ok {
			r.colMetas[i] = &m
		}
	}
}

// convertRow is to convert data from Athena type to Golang SQL type and put them into an array of driver.Value.
func (r *Rows) convertRow(columns []athenatypes.ColumnInfo, rdata []athenatypes.Datum, ret []driver.Value,
	driverConfig *Config) error {
	if r.colMetas == nil {
		r.buildColMetas(columns)
	}
	// Iterate ret, not rdata: database/sql reuses the same dest slice across
	// Next calls, so every slot must be written. A row with fewer datums than
	// columns feeds a nil rawValue into convertCell, which applies the
	// configured MissingAs* policy instead of leaving the previous row's value
	// behind.
	for i := range ret {
		if i >= len(columns) || i >= len(r.colMetas) {
			ret[i] = nil
			continue
		}
		var rawValue *string
		if i < len(rdata) {
			rawValue = rdata[i].VarCharValue
		}
		value, err := r.convertCell(columns[i], r.colMetas[i], rawValue, driverConfig)
		if err != nil {
			r.tracer.Log(ErrorLevel, "convertrow failed", slog.String("error", err.Error()))
			r.tracer.Scope().Counter(DriverName + ".failure.convertrow").Inc(1)
			return err
		}
		ret[i] = value
	}
	return nil
}

// convertCell turns one Athena cell into a Go value.
// https://docs.aws.amazon.com/en_pv/athena/latest/ug/data-types.html
// https://docs.aws.amazon.com/athena/latest/ug/geospatial-input-data-formats-supported-geometry-types.html#geometry-data-types
// varbinary is undocumented above, but appears in geo query like:
//
//	SELECT ST_POINT(-74.006801, 40.705220).
//
// json is also undocumented above, but appears here https://docs.aws.amazon.com/athena/latest/ug/querying-JSON.html
// The full list is here: https://prestodb.io/docs/0.172/language/types.html
// Include ipaddress for forward compatibility.
func (r *Rows) convertCell(columnInfo athenatypes.ColumnInfo, meta *athenaTypeMeta, rawValue *string, driverConfig *Config) (any, error) {
	// Name and Type are both optional in the SDK — resolve once, guarded.
	colName := aws.ToString(columnInfo.Name)
	typeName := aws.ToString(columnInfo.Type)
	if maskedValue, masked := driverConfig.CheckColumnMasked(colName); masked { // "comma ok" idiom
		return maskedValue, nil
	}
	if rawValue == nil {
		r.tracer.Scope().Counter(DriverName + ".missingvalue").Inc(1)
		r.tracer.Log(DebugLevel, "missing data",
			slog.String("columnInfo.Name", colName),
			slog.String("queryID", r.queryID),
			slog.String("workgroup", workgroupName(driverConfig)))
		if driverConfig.MissingAsNil {
			return nil, nil
		} else if driverConfig.MissingAsEmptyString {
			return "", nil
		} else if driverConfig.MissingAsDefault {
			if meta != nil {
				return meta.defaultVal, nil
			}
			r.tracer.Scope().Counter(DriverName + ".failure.defaultvalueforcolumntype.type").Inc(1)
			r.tracer.Log(ErrorLevel, "column data type error", slog.String("columnInfo.Type", typeName))
			return "", nil
		}
		r.tracer.Scope().Counter(DriverName + ".failure.convertvalue.config").Inc(1)
		r.tracer.Log(ErrorLevel, "missing data", slog.String("columnInfo.Name", colName))
		return nil, fmt.Errorf("missing data at column %s", colName)
	}
	if meta == nil {
		r.tracer.Scope().Counter(DriverName + ".failure.convertvalue.type").Inc(1)
		r.tracer.Log(ErrorLevel, "column data type error", slog.String("columnInfo.Type", typeName))
		return nil, fmt.Errorf("unknown type `%s` with value %s", typeName, *rawValue)
	}
	val, err := meta.parse(*rawValue)
	if err != nil {
		bucket := "convertvalue"
		switch typeName {
		case "boolean":
			bucket = "convertvalue.boolean"
		case "date", "time", "time with time zone", "timestamp", "timestamp with time zone":
			bucket = "convertvalue.time"
		}
		r.tracer.Scope().Counter(DriverName + ".failure." + bucket).Inc(1)
	}
	return val, err
}
