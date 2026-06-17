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
