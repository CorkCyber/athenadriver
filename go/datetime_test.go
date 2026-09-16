// SPDX-License-Identifier: MIT

package athenadriver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDateTime_ScanTime(t *testing.T) {
	r, e := scanTime("01:02:03.456")
	assert.Nil(t, e)
	assert.True(t, r.Valid)
	assert.NotEqual(t, r.Time.String(), ZeroDateTimeString)
}

func TestDateTime_ScanTimeWithTimeZone(t *testing.T) {
	r, e := scanTime("01:02:03.456 America/Los_Angeles")
	assert.Nil(t, e)
	assert.True(t, r.Valid)
	assert.NotEqual(t, r.Time.String(), ZeroDateTimeString)

}

func TestDateTime_ScanTimeStamp(t *testing.T) {
	r, e := scanTime("2001-08-22 03:04:05.321")
	assert.Nil(t, e)
	assert.True(t, r.Valid)
	assert.NotEqual(t, r.Time.String(), ZeroDateTimeString)

}

func TestDateTime_ScanTimeStampWithMicroseconds(t *testing.T) {
	r, e := scanTime("2001-08-22 03:04:05.321456")
	assert.Nil(t, e)
	assert.True(t, r.Valid)
	assert.NotEqual(t, r.Time.String(), ZeroDateTimeString)
}

func TestDateTime_ScanTimeStampWithNanoseconds(t *testing.T) {
	r, e := scanTime("2001-08-22 03:04:05.321456789")
	assert.Nil(t, e)
	assert.True(t, r.Valid)
	assert.NotEqual(t, r.Time.String(), ZeroDateTimeString)
}

func TestDateTime_ScanTimeStampWithTimeZone(t *testing.T) {
	r, e := scanTime("2001-08-22 03:04:05.321 America/Los_Angeles")
	assert.Nil(t, e)
	assert.True(t, r.Valid)
	assert.NotEqual(t, r.Time.String(), ZeroDateTimeString)
}

func TestDateTime_ScanTimeFail(t *testing.T) {
	r, e := scanTime("2001-08-22 03:04:05.321 PST")
	assert.NotNil(t, e)
	assert.False(t, r.Valid)
	assert.Equal(t, ZeroDateTimeString, r.Time.String())

	r, e = scanTime("abc")
	assert.NotNil(t, e)
	assert.False(t, r.Valid)
	assert.Equal(t, ZeroDateTimeString, r.Time.String())
}

func TestDateTime_ScanTimeFail_MonthOutOfRange(t *testing.T) {
	r, e := scanTime("2001-18-22 03:04:05.321 America/Los_Angeles")
	assert.NotNil(t, e)
	assert.False(t, r.Valid)
	assert.Equal(t, ZeroDateTimeString, r.Time.String())
}

func TestDateTime_ParseAthenaTimeWithLocation(t *testing.T) {
	r, e := parseAthenaTimeWithLocation("abc")
	assert.NotNil(t, e)
	assert.False(t, r.Valid)
	assert.Equal(t, ZeroDateTimeString, r.Time.String())

	r, e = parseAthenaTimeWithLocation("ab c")
	assert.NotNil(t, e)
	assert.False(t, r.Valid)
	assert.Equal(t, ZeroDateTimeString, r.Time.String())
}

// TestDateTime_ScanTimeFractionWidths pins that every fractional-second
// width parses, now that the layout sweep relies on time.Parse's implicit
// fraction handling instead of one layout per width.
func TestDateTime_ScanTimeFractionWidths(t *testing.T) {
	for _, frac := range []string{
		"", ".1", ".12", ".123", ".1234", ".12345", ".123456",
		".1234567", ".12345678", ".123456789",
	} {
		in := "2001-08-22 03:04:05" + frac
		r, e := scanTime(in)
		assert.NoError(t, e, in)
		assert.True(t, r.Valid, in)
		assert.Equal(t, in, r.Time.Format("2006-01-02 15:04:05")+frac, in)

		in += " America/Los_Angeles"
		r, e = scanTime(in)
		assert.NoError(t, e, in)
		assert.True(t, r.Valid, in)
		assert.Equal(t, "America/Los_Angeles", r.Time.Location().String(), in)
	}
	// Time-only and date-only shapes still work at any width.
	for _, in := range []string{"01:02:03", "01:02:03.4", "01:02:03.456789", "2001-08-22"} {
		r, e := scanTime(in)
		assert.NoError(t, e, in)
		assert.True(t, r.Valid, in)
	}
}

// TestDateTime_LocationCache pins that a named zone is resolved once and
// reused: the second scan must hit the cache and yield the same *Location.
func TestDateTime_LocationCache(t *testing.T) {
	locCache.Delete("America/Los_Angeles")
	r1, e1 := scanTime("2001-08-22 03:04:05.321 America/Los_Angeles")
	assert.NoError(t, e1)
	assert.True(t, r1.Valid)

	cached, ok := locCache.Load("America/Los_Angeles")
	assert.True(t, ok, "cache must be populated after first parse")

	r2, e2 := scanTime("2002-09-23 04:05:06.654 America/Los_Angeles")
	assert.NoError(t, e2)
	assert.True(t, r2.Valid)
	assert.Same(t, cached, r2.Time.Location(), "second parse must reuse cached *Location")
	assert.Same(t, r1.Time.Location(), r2.Time.Location())

	// A bad zone must not be cached.
	_, e3 := scanTime("2001-08-22 03:04:05.321 Not/AZone")
	assert.Error(t, e3)
	_, ok = locCache.Load("Not/AZone")
	assert.False(t, ok)
}

// TestDateTime_ScanTimeNumericOffset pins that a numeric UTC offset (what
// Trino renders when the session zone is an offset, not an IANA name) parses
// to the correct instant instead of failing in time.LoadLocation.
func TestDateTime_ScanTimeNumericOffset(t *testing.T) {
	for _, tt := range []struct {
		in  string
		utc string
	}{
		{"2021-06-01 10:00:00.000 -08:00", "2021-06-01 18:00:00"},
		{"2021-06-01 10:00:00.000 +05:30", "2021-06-01 04:30:00"},
		{"2021-06-01 10:00:00 +00:00", "2021-06-01 10:00:00"},
	} {
		r, e := scanTime(tt.in)
		assert.NoError(t, e, tt.in)
		assert.True(t, r.Valid, tt.in)
		assert.Equal(t, tt.utc, r.Time.UTC().Format("2006-01-02 15:04:05"), tt.in)
	}

	// Near-miss offset shapes must still be rejected, not silently accepted.
	for _, in := range []string{
		"2021-06-01 10:00:00 -8:00", "2021-06-01 10:00:00 +0a:00",
	} {
		r, e := scanTime(in)
		assert.Error(t, e, in)
		assert.False(t, r.Valid, in)
	}
}

// TestDateTime_ShapeDispatchAllocs pins that date-only and time-only values
// skip the failed layout attempts that used to allocate a *time.ParseError
// each (2 allocs for a TIME value, 1 for a DATE value) before matching.
func TestDateTime_ShapeDispatchAllocs(t *testing.T) {
	for _, in := range []string{"2001-08-22", "01:02:03", "01:02:03.456"} {
		n := testing.AllocsPerRun(100, func() {
			if r, _ := scanTime(in); !r.Valid {
				t.Fatalf("value %q failed to parse", in)
			}
		})
		assert.Zero(t, n, "value %q allocated %v times", in, n)
	}
}

// TestDateTime_ShapeDispatchFailsFast covers values that match the date-only
// or time-only shape but are not real dates/times: the direct attempt fails
// and no other layout could match, so the error is returned right away.
func TestDateTime_ShapeDispatchFailsFast(t *testing.T) {
	for _, in := range []string{"2024-13-45", "0000-00-00", "99:99:99", "ab:cd:ef"} {
		r, e := scanTime(in)
		assert.Error(t, e, "value %q", in)
		assert.False(t, r.Valid, "value %q", in)
		assert.Equal(t, ZeroDateTimeString, r.Time.String(), "value %q", in)

		// One failed parse, not two: a second identical attempt in the
		// sweep would roughly double this.
		n := testing.AllocsPerRun(100, func() { _, _ = scanTime(in) })
		assert.LessOrEqual(t, n, float64(4), "value %q allocated %v times", in, n)
	}
}

func TestDateTime_ScanTimeTrailingSpace(t *testing.T) {
	for _, tt := range []struct {
		in    string
		valid bool
	}{
		{"2024-01-01 10:00:00 ", false}, // trailing space: must not panic
		{" ", false},                    // bare space: must not panic
		{"", false},
		{"2001-08-22 03:04:05.321", true},
		{"2001-08-22 03:04:05.321 America/Los_Angeles", true},
		{"01:02:03.456", true},
	} {
		r, e := scanTime(tt.in)
		assert.Equal(t, tt.valid, r.Valid, "value %q", tt.in)
		if tt.valid {
			assert.NoError(t, e, "value %q", tt.in)
		} else {
			assert.Error(t, e, "value %q", tt.in)
		}
	}
}
