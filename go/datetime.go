// SPDX-License-Identifier: MIT

package athenadriver

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

// AthenaTime represents a time.Time value that can be null.
// The AthenaTime supports Athena's Date, Time and Timestamp data types,
// with or without time zone.
type AthenaTime struct {
	Time  time.Time
	Valid bool
}

var timeLayouts = []string{
	"2006-01-02",
	"15:04:05.000",
	"2006-01-02 15:04:05.000000000",
	"2006-01-02 15:04:05.000000",
	"2006-01-02 15:04:05.000",
	// Athena emits TIMESTAMPs without fractional seconds when the
	// underlying value has none — e.g. CAST('2024-01-01 10:00:00' AS
	// TIMESTAMP). Without this layout Scan of such rows fails.
	"2006-01-02 15:04:05",
}

// likelyTimeLayout picks the single layout matching the value's shape so
// the common case parses on the first attempt instead of failing through
// up to five layouts per cell (a real per-row cost on large scans).
// parseAthenaTime still falls back to the full timeLayouts sweep if the
// shape-guess fails to parse.
func likelyTimeLayout(v string) string {
	dot := strings.IndexByte(v, '.')
	if strings.IndexByte(v, ' ') >= 0 {
		if dot < 0 {
			return "2006-01-02 15:04:05"
		}
		switch len(v) - dot - 1 {
		case 9:
			return "2006-01-02 15:04:05.000000000"
		case 6:
			return "2006-01-02 15:04:05.000000"
		default:
			return "2006-01-02 15:04:05.000"
		}
	}
	if strings.IndexByte(v, ':') >= 0 {
		return "15:04:05.000"
	}
	return "2006-01-02"
}

func scanTime(vv string) (AthenaTime, error) {
	parts := strings.Split(vv, " ")
	if len(parts) > 1 && !unicode.IsDigit(rune(parts[len(parts)-1][0])) {
		return parseAthenaTimeWithLocation(vv)
	}
	return parseAthenaTime(vv)
}

func parseAthenaTime(v string) (AthenaTime, error) {
	return parseAthenaTimeIn(v, time.Local)
}

func parseAthenaTimeIn(v string, loc *time.Location) (AthenaTime, error) {
	t, err := time.ParseInLocation(likelyTimeLayout(v), v, loc)
	if err == nil {
		return AthenaTime{Valid: true, Time: t}, nil
	}
	for _, layout := range timeLayouts {
		t, err = time.ParseInLocation(layout, v, loc)
		if err == nil {
			return AthenaTime{Valid: true, Time: t}, nil
		}
	}
	return AthenaTime{}, err
}

func parseAthenaTimeWithLocation(v string) (AthenaTime, error) {
	idx := strings.LastIndex(v, " ")
	if idx == -1 {
		return AthenaTime{}, fmt.Errorf("cannot convert %v (%T) to time+zone", v, v)
	}
	stamp, location := v[:idx], v[idx+1:]
	loc, err := time.LoadLocation(location)
	if err != nil {
		return AthenaTime{}, fmt.Errorf("cannot load timezone %q: %w", location, err)
	}
	return parseAthenaTimeIn(stamp, loc)
}
