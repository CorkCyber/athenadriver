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
