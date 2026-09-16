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

// Quoted time layouts: the single quotes are not layout tokens, so Format
// emits them literally and no separate quoting step is needed.
// timestampFormatDriverMicro is the string format we transform Go time.Time objects into. This is not meant for
// TIMESTAMP columns, as Athena timestamp columns only have a millisecond granularity.
const (
	timestampFormatDriverMicro = "'2006-01-02 15:04:05.000000'"
	timestampFormatDriver      = "'2006-01-02 15:04:05'"
)

// appendSQLValue renders one argument as an Athena SQL literal and appends it
// to dst. It is the single source of truth for argument rendering, shared by
// interpolateParams and buildExecutionParams.
func appendSQLValue(dst []byte, arg driver.Value) ([]byte, error) {
	if arg == nil {
		return append(dst, "NULL"...), nil
	}
	switch v := arg.(type) {
	case int64:
		return strconv.AppendInt(dst, v, 10), nil
	case uint64:
		return strconv.AppendUint(dst, v, 10), nil
	case float64:
		// strconv.AppendFloat renders NaN/Inf as the bare words NaN/+Inf/-Inf,
		// which aren't valid Trino/Athena numeric-literal syntax: emitting
		// them unquoted would send malformed SQL and surface as an opaque
		// server-side parse error instead of this clear client-side one.
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return dst, fmt.Errorf("athenadriver: float argument %v has no Athena SQL literal representation", v)
		}
		return strconv.AppendFloat(dst, v, 'g', -1, 64), nil
	case bool:
		// Trino/Athena boolean literals; "1"/"0" (the MySQL-driver
		// heritage) fails BOOLEAN type-checks server-side.
		return strconv.AppendBool(dst, v), nil
	case time.Time:
		// Produces a string good for STRING/CHAR/VARCHAR (microsecond
		// granularity, more than TIMESTAMP's millisecond support). For
		// DATE/TIME/TIMESTAMP columns prefer a string arg with a typecast.
		// No zero-date concept in Trino/Athena, so zero time.Time -> NULL,
		// same as nil.
		if v.IsZero() {
			return append(dst, "NULL"...), nil
		}
		v = v.In(time.UTC).Add(time.Nanosecond * 500) // to round under microsecond
		layout := timestampFormatDriver
		if v.Nanosecond()/1000 != 0 {
			layout = timestampFormatDriverMicro
		}
		return v.AppendFormat(dst, layout), nil
	case []byte:
		return append(dst, FormatString(string(v))...), nil
	case string:
		// Athena evaluates ExecutionParameters as SQL expressions, not
		// opaque values, so unquoted `1 OR 1=1` is a predicate, not a
		// value. Quoting fixes that; use Raw to opt out for an expression.
		return append(dst, FormatString(v)...), nil
	case Raw:
		return append(dst, v...), nil
	default:
		return dst, ErrQueryUnknownType
	}
}

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
	executionParams := make([]string, 0, len(args))
	var scratch []byte
	for _, arg := range args {
		var err error
		scratch, err = appendSQLValue(scratch[:0], arg)
		if err != nil {
			return []string{}, err
		}
		executionParams = append(executionParams, string(scratch))
	}
	return executionParams, nil
}

// placeholderIndexes returns the byte offset of every real `?` placeholder
// in query: one outside a string literal ('it?'), quoted identifier
// ("weird?column"), `--` comment, or `/* */` comment. A `?` inside any of
// those is data, not a placeholder. Doubled-quote escapes (Trino's rule)
// work fine with the plain in/out toggle below.
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

		var err error
		queryBuffer, err = appendSQLValue(queryBuffer, args[argPos])
		if err != nil {
			return "", err
		}

		if len(queryBuffer)+4 > 10*MAXQueryStringLength {
			return "", ErrQueryBufferOF
		}
	}
	queryBuffer = append(queryBuffer, query[prev:]...)
	return string(queryBuffer), nil
}
