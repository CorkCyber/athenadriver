// SPDX-License-Identifier: MIT

package athenadriver

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// AthenaTime represents a time.Time value that can be null.
// The AthenaTime supports Athena's Date, Time and Timestamp data types,
// with or without time zone.
type AthenaTime struct {
	Time  time.Time
	Valid bool
}

// timeLayouts is ordered most-common-first: the first match wins, and every
// failed attempt allocates a *time.ParseError. No fractional-second layouts
// are needed since time.Parse accepts a fractional suffix after the seconds
// field even when the layout omits one, so these three cover every Athena
// DATE/TIME/TIMESTAMP shape at any fraction width.
var timeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02",
	"15:04:05",
}

// locCache memoizes time.LoadLocation, which has no cache of its own and
// re-reads tzdata from disk on every call (~42us, 6.6KB). Zone names in a
// result set are a small fixed set, so the cache is bounded in practice.
var locCache sync.Map // string -> *time.Location

func scanTime(vv string) (AthenaTime, error) {
	// Peek at the byte after the last space without allocating a []string:
	// a non-digit there means a trailing timezone name. Bounds-checked so a
	// trailing space (or a bare " ") falls through instead of panicking.
	if i := strings.LastIndexByte(vv, ' '); i >= 0 && i+1 < len(vv) &&
		(vv[i+1] < '0' || vv[i+1] > '9') {
		return parseAthenaTimeWithLocation(vv)
	}
	return parseAthenaTime(vv)
}

func parseAthenaTime(v string) (AthenaTime, error) {
	return parseAthenaTimeIn(v, time.Local)
}

func parseAthenaTimeIn(v string, loc *time.Location) (AthenaTime, error) {
	var t time.Time
	var err error
	// Shape dispatch: skip the timestamp-first sweep below (costly failed
	// parses for date-only/time-only values) via a few byte comparisons.
	// Unrecognized shapes fall through to the full sweep.
	switch {
	case len(v) == 10 && v[4] == '-' && v[7] == '-':
		if t, err = time.ParseInLocation(timeLayouts[1], v, loc); err != nil {
			return AthenaTime{}, err
		}
		return AthenaTime{Valid: true, Time: t}, nil
	case len(v) >= 8 && v[2] == ':' && v[5] == ':':
		if t, err = time.ParseInLocation(timeLayouts[2], v, loc); err != nil {
			return AthenaTime{}, err
		}
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
	loc, ok := locCache.Load(location)
	if !ok {
		l, err := loadZone(location)
		if err != nil {
			return AthenaTime{}, fmt.Errorf("cannot load timezone %q: %w", location, err)
		}
		loc, _ = locCache.LoadOrStore(location, l)
	}
	return parseAthenaTimeIn(stamp, loc.(*time.Location))
}

// loadZone resolves a trailing zone token. Trino renders `timestamp with time
// zone` with a numeric UTC offset ("-08:00", "+05:30") whenever the session
// zone or an AT TIME ZONE expression is an offset rather than an IANA name,
// and time.LoadLocation only knows IANA names, so handle that shape here.
func loadZone(s string) (*time.Location, error) {
	if len(s) == 6 && (s[0] == '+' || s[0] == '-') && s[3] == ':' &&
		isDigit(s[1]) && isDigit(s[2]) && isDigit(s[4]) && isDigit(s[5]) {
		off := (int(s[1]-'0')*10+int(s[2]-'0'))*3600 + (int(s[4]-'0')*10+int(s[5]-'0'))*60
		if s[0] == '-' {
			off = -off
		}
		return time.FixedZone(s, off), nil
	}
	return time.LoadLocation(s)
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
