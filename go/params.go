// SPDX-License-Identifier: MIT

package athenadriver

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// timestampFormatDriverMicro is the string format we transform Go time.Time objects into. This is not meant for
// TIMESTAMP columns, as Athena timestamp columns only have a millisecond granularity.
const timestampFormatDriverMicro = "2006-01-02 15:04:05.000000"

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
			val = strconv.FormatFloat(v, 'g', -1, 64)
		case bool:
			if v {
				val = "1"
			} else {
				val = "0"
			}
		case time.Time:
			// Note: time.Time objects are transformed into strings for a STRING/CHAR/VARCHAR type column.
			// To maintain compatibility with the current interpolateParams() behavior, this function produces a string
			// up to microsecond granularity, which Athena does not support in TIMESTAMP columns (up to milliseconds).
			// For DATE/TIME/TIMESTAMP, it is better to pass in string arguments with a typecast. Refer to the string
			// case below.
			// Matches interpolateParams() behavior.
			val = "'0000-00-00'" // Special-cased.
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
			// Note: Different from interpolateParams() behavior.
			// Like the string case below, enclosing in single quotes would prevent typecasting or function calls in
			// execution parameters. Prior to passing in query arguments, Format* functions in utils.go can be used.
			val = string(v)
		case string:
			// Note: Different from interpolateParams() behavior.
			// For parameterized queries, typecasting or function calls go in the execution parameters. For example,
			// `WHERE created = TIMESTAMP '2024-07-01 00:00:00'` should be formatted as: `WHERE created = ?` (query) and
			// `TIMESTAMP '2024-07-01 00:00:00.000'` (arg). Therefore, we cannot simply enclose the full string with
			// single quotes here. Users should use the Format* functions in utils.go to format input string arguments.
			val = v
		default:
			return []string{}, ErrQueryUnknownType
		}
		executionParams = append(executionParams, val)
	}
	return executionParams, nil
}

func (c *Connection) interpolateParams(query string, args []driver.Value) (string, error) {
	// Number of ? should be same to len(args)
	if strings.Count(query, "?") != len(args) {
		return "", ErrInvalidQuery
	}

	queryBuffer := make([]byte, MAXQueryStringLength)
	queryBuffer = queryBuffer[:0]
	argPos := 0

	for i := 0; i < len(query); i++ {
		q := strings.IndexByte(query[i:], '?')
		if q == -1 {
			queryBuffer = append(queryBuffer, query[i:]...)
			break
		}
		queryBuffer = append(queryBuffer, query[i:i+q]...)
		i += q

		arg := args[argPos]
		argPos++

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
			queryBuffer = strconv.AppendFloat(queryBuffer, v, 'g', -1, 64)
		case bool:
			if v {
				queryBuffer = append(queryBuffer, '1')
			} else {
				queryBuffer = append(queryBuffer, '0')
			}
		case time.Time:
			if v.IsZero() {
				queryBuffer = append(queryBuffer, "'0000-00-00'"...)
			} else {
				v := v.In(time.UTC)
				v = v.Add(time.Nanosecond * 500) // To round under microsecond
				year := v.Year()
				year100 := year / 100
				year1 := year % 100
				month := v.Month()
				day := v.Day()
				hour := v.Hour()
				minute := v.Minute()
				second := v.Second()
				micro := v.Nanosecond() / 1000

				queryBuffer = append(queryBuffer, []byte{
					'\'',
					digits10[year100], digits01[year100],
					digits10[year1], digits01[year1],
					'-',
					digits10[month], digits01[month],
					'-',
					digits10[day], digits01[day],
					' ',
					digits10[hour], digits01[hour],
					':',
					digits10[minute], digits01[minute],
					':',
					digits10[second], digits01[second],
				}...)

				if micro != 0 {
					micro10000 := micro / 10000
					micro100 := micro / 100 % 100
					micro1 := micro % 100
					queryBuffer = append(queryBuffer, []byte{
						'.',
						digits10[micro10000], digits01[micro10000],
						digits10[micro100], digits01[micro100],
						digits10[micro1], digits01[micro1],
					}...)
				}
				queryBuffer = append(queryBuffer, '\'')
			}
		case []byte:
			queryBuffer = append(queryBuffer, "_binary'"...)
			queryBuffer = escapeBytesBackslash(queryBuffer, v)
			queryBuffer = append(queryBuffer, '\'')
		case string:
			queryBuffer = append(queryBuffer, '\'')
			queryBuffer = escapeStringBackslash(queryBuffer, v)
			queryBuffer = append(queryBuffer, '\'')
		default:
			return "", ErrQueryUnknownType
		}

		if len(queryBuffer)+4 > 10*MAXQueryStringLength {
			return "", ErrQueryBufferOF
		}
	}
	return string(queryBuffer), nil
}

// CheckNamedValue is to implement interface driver.NamedValueChecker.
func (c *Connection) CheckNamedValue(nv *driver.NamedValue) (err error) {
	nv.Value, err = driver.DefaultParameterConverter.ConvertValue(nv.Value)
	return
}
