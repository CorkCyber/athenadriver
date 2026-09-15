// SPDX-License-Identifier: MIT

package athenadriver

import (
	"database/sql/driver"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// timestampFormatDriverMicro is the string format we transform Go time.Time objects into. This is not meant for
// TIMESTAMP columns, as Athena timestamp columns only have a millisecond granularity.
const timestampFormatDriverMicro = "2006-01-02 15:04:05.000000"

// CheckNamedValue lets Raw arguments reach the driver unconverted;
// database/sql's default converter would otherwise flatten Raw to a plain
// string (Kind == String) and the value would get quoted like any other
// string. Everything else falls back to the default conversion.
func (c *Connection) CheckNamedValue(nv *driver.NamedValue) error {
	if _, ok := nv.Value.(Raw); ok {
		return nil
	}
	return driver.ErrSkip
}

var _ driver.NamedValueChecker = (*Connection)(nil)

// buildExecutionParams converts Go data types into strings for query arguments in parameterized queries.
// Returns nil for empty args so the resulting StartQueryExecution call leaves
// ExecutionParameters unset rather than sending an empty array, which Athena
// rejects for non-parameterized queries.
func (c *Connection) buildExecutionParams(args []driver.Value) ([]string, error) {
	if len(args) == 0 {
		return nil, nil
	}
	executionParams := []string{}
	for _, arg := range args {
		if arg == nil {
			executionParams = append(executionParams, "NULL")
			continue
		}
		// type switches of arg to handle different query parameter types
		val := ""
		switch v := arg.(type) {
		case int64:
			val = strconv.FormatInt(v, 10)
		case uint64:
			val = strconv.FormatUint(v, 10)
		case float64:
			// strconv.FormatFloat renders NaN/Inf as the bare words
			// NaN/+Inf/-Inf, which aren't valid Trino/Athena numeric-literal
			// syntax: emitting them would send malformed SQL and surface as
			// an opaque server-side parse error instead of this clear
			// client-side one.
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return []string{}, fmt.Errorf("athenadriver: float argument %v has no Athena SQL literal representation", v)
			}
			val = strconv.FormatFloat(v, 'g', -1, 64)
		case bool:
			// Trino/Athena boolean literals; "1"/"0" (the MySQL-driver
			// heritage) fails BOOLEAN type-checks server-side.
			val = strconv.FormatBool(v)
		case time.Time:
			// Note: time.Time objects are transformed into strings for a STRING/CHAR/VARCHAR type column.
			// To maintain compatibility with the current interpolateParams() behavior, this function produces a string
			// up to microsecond granularity, which Athena does not support in TIMESTAMP columns (up to milliseconds).
			// For DATE/TIME/TIMESTAMP, it is better to pass in string arguments with a typecast. Refer to the string
			// case below.
			// Matches interpolateParams() behavior.
			// Trino/Athena has no zero-date concept: DATE '0000-00-00' is a
			// parse error and the bare string matches nothing, so a zero
			// time.Time is NULL, same as a nil argument above.
			val = "NULL"
			if !v.IsZero() {
				v := v.In(time.UTC)
				v = v.Add(time.Nanosecond * 500) // To round under microsecond
				dateFormat := timestampFormatDriverMicro
				if v.Nanosecond()/1000 == 0 {
					// Omit microseconds if that part is zero
					dateFormat = time.DateTime
				}
				val = fmt.Sprintf("'%s'", v.Format(dateFormat))
			}
		case []byte:
			val = string(FormatBytes(v))
		case string:
			// Athena evaluates each ExecutionParameters entry as a SQL
			// EXPRESSION, not as an opaque bound value, so an unquoted
			// string argument is executable SQL — quoting is what makes
			// `1 OR 1=1` a value instead of a predicate. A caller that
			// genuinely needs an expression (typecast, function call)
			// must opt out explicitly with the Raw type.
			val = FormatString(v)
		case Raw:
			val = string(v)
		default:
			return []string{}, ErrQueryUnknownType
		}
		executionParams = append(executionParams, val)
	}
	return executionParams, nil
}

// placeholderIndexes returns the byte offset of every `?` in query that is a
// real placeholder, i.e. one that sits OUTSIDE a single-quoted string literal
// ('it?'), a double-quoted identifier ("weird?column"), a `--` line comment
// and a `/* */` block comment. A `?` in any of those is data or prose, not a
// placeholder: substituting it would inject the argument into the middle of a
// literal/identifier/comment and shift every later argument by one.
// Trino escapes a quote inside a literal or identifier by doubling it, which
// the plain toggle handles: the second quote of the pair re-enters it.
func placeholderIndexes(query string) []int {
	var idx []int
	inString, inIdent := false, false
	for i := 0; i < len(query); i++ {
		switch query[i] {
		case '\'':
			if !inIdent {
				inString = !inString
			}
		case '"':
			if !inString {
				inIdent = !inIdent
			}
		case '-':
			if !inString && !inIdent && i+1 < len(query) && query[i+1] == '-' {
				if nl := strings.IndexByte(query[i:], '\n'); nl < 0 {
					return idx // comment runs to end of query
				} else {
					i += nl
				}
			}
		case '/':
			if !inString && !inIdent && i+1 < len(query) && query[i+1] == '*' {
				end := strings.Index(query[i+2:], "*/")
				if end < 0 {
					return idx // unterminated block comment
				}
				i += 2 + end + 1
			}
		case '?':
			if !inString && !inIdent {
				idx = append(idx, i)
			}
		}
	}
	return idx
}

func (c *Connection) interpolateParams(query string, args []driver.Value) (string, error) {
	// Number of real (unquoted) ? should be same to len(args)
	placeholders := placeholderIndexes(query)
	if len(placeholders) != len(args) {
		return "", ErrInvalidQuery
	}

	// Grow-as-needed buffer sized to the query plus a 64-byte cushion per
	// argument. Previous fixed 256 KiB pre-alloc was garbage on every
	// parameterized query, regardless of query length.
	queryBuffer := make([]byte, 0, len(query)+64*len(args))
	prev := 0

	for argPos, p := range placeholders {
		queryBuffer = append(queryBuffer, query[prev:p]...)
		prev = p + 1

		arg := args[argPos]

		if arg == nil {
			queryBuffer = append(queryBuffer, "NULL"...)
			continue
		}
		// type switches of arg to handle different query parameter types
		switch v := arg.(type) {
		case int64:
			queryBuffer = strconv.AppendInt(queryBuffer, v, 10)
		case uint64:
			queryBuffer = strconv.AppendUint(queryBuffer, v, 10)
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return "", fmt.Errorf("athenadriver: float argument %v has no Athena SQL literal representation", v)
			}
			queryBuffer = strconv.AppendFloat(queryBuffer, v, 'g', -1, 64)
		case bool:
			queryBuffer = strconv.AppendBool(queryBuffer, v)
		case time.Time:
			if v.IsZero() {
				queryBuffer = append(queryBuffer, "NULL"...)
			} else {
				v := v.In(time.UTC).Add(time.Nanosecond * 500)
				layout := "'2006-01-02 15:04:05'"
				if v.Nanosecond()/1000 != 0 {
					layout = "'2006-01-02 15:04:05.000000'"
				}
				queryBuffer = v.AppendFormat(queryBuffer, layout)
			}
		case []byte:
			queryBuffer = append(queryBuffer, FormatBytes(v)...)
		case string:
			queryBuffer = append(queryBuffer, FormatString(v)...)
		case Raw:
			queryBuffer = append(queryBuffer, v...)
		default:
			return "", ErrQueryUnknownType
		}

		if len(queryBuffer)+4 > 10*MAXQueryStringLength {
			return "", ErrQueryBufferOF
		}
	}
	queryBuffer = append(queryBuffer, query[prev:]...)
	return string(queryBuffer), nil
}
