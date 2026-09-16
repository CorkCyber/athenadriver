// SPDX-License-Identifier: MIT

package athenadriver

import (
	"database/sql/driver"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPlaceholderIndexes_SkipsQuotesAndComments(t *testing.T) {
	for _, tt := range []struct {
		name  string
		query string
		want  int
	}{
		{"plain", "SELECT * FROM t WHERE a = ? AND b = ?", 2},
		{"single-quoted literal", "SELECT * FROM t WHERE n = 'imported?' AND a = ?", 1},
		{"doubled quote in literal", "SELECT 'it''s? here', ?", 1},
		{"double-quoted identifier", `SELECT "weird?column" FROM t WHERE a = ?`, 1},
		{"doubled quote in identifier", `SELECT "he said ""what?""" FROM t WHERE a = ?`, 1},
		{"quote chars nested the other way", `SELECT "a'b?c" FROM t WHERE x = ?`, 1},
		{"apostrophe in identifier", `SELECT 'a"b?c' FROM t WHERE x = ?`, 1},
		{"line comment", "SELECT 1 -- really? yes\nWHERE a = ?", 1},
		{"line comment at end", "SELECT 1 WHERE a = ? -- what?", 1},
		{"block comment", "SELECT /* who? me? */ 1 WHERE a = ?", 1},
		{"block comment multiline", "SELECT /* a?\nb? */ 1 WHERE a = ? AND b = ?", 2},
		{"unterminated block comment", "SELECT 1 /* ? ", 0},
		{"minus is not a comment", "SELECT 1-1 WHERE a = ?", 1},
		{"divide is not a comment", "SELECT a/2 WHERE b = ?", 1},
		{"comment markers inside a literal", "SELECT '-- ? /* ?' , ?", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Len(t, placeholderIndexes(tt.query), tt.want)
		})
	}
}

func TestInterpolateParams_QuotedIdentifiersAndComments(t *testing.T) {
	c := &Connection{}

	q, err := c.interpolateParams(`SELECT "weird?column" FROM t WHERE a = ?`, []driver.Value{int64(7)})
	assert.NoError(t, err)
	assert.Equal(t, `SELECT "weird?column" FROM t WHERE a = 7`, q)

	q, err = c.interpolateParams("SELECT 1 -- really? yes\nWHERE a = ?", []driver.Value{int64(7)})
	assert.NoError(t, err)
	assert.Equal(t, "SELECT 1 -- really? yes\nWHERE a = 7", q)

	q, err = c.interpolateParams("SELECT /* who? me? */ 1 WHERE a = ?", []driver.Value{int64(7)})
	assert.NoError(t, err)
	assert.Equal(t, "SELECT /* who? me? */ 1 WHERE a = 7", q)

	// Argument count must match the REAL placeholder count, not the '?' count.
	_, err = c.interpolateParams(`SELECT "weird?column" WHERE a = ?`, []driver.Value{int64(7), int64(8)})
	assert.Equal(t, ErrInvalidQuery, err)
}

// TestRenderParity pins the shared appendSQLValue rendering: interpolateParams
// and buildExecutionParams must emit the same literal for the same argument.
func TestRenderParity(t *testing.T) {
	ts := time.Date(2024, 7, 1, 2, 3, 4, 0, time.UTC)
	tsMicro := time.Date(2024, 7, 1, 2, 3, 4, 123456000, time.UTC)
	cases := []struct {
		name string
		arg  driver.Value
		want string
	}{
		{"nil", nil, "NULL"},
		{"int64", int64(-10), "-10"},
		{"uint64", uint64(42), "42"},
		{"float64", 1.23, "1.23"},
		{"bool", true, "true"},
		{"time zero", time.Time{}, "NULL"},
		{"time", ts, "'2024-07-01 02:03:04'"},
		{"time micro", tsMicro, "'2024-07-01 02:03:04.123456'"},
		{"bytes", []byte("a'b"), "'a''b'"},
		{"string", "it's a \"test\"", "'it''s a \"test\"'"},
		{"raw", Raw("CURRENT_DATE"), "CURRENT_DATE"},
	}
	c := createTestConnection(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params, err := c.buildExecutionParams([]driver.Value{tc.arg})
			assert.NoError(t, err)
			assert.Equal(t, []string{tc.want}, params)

			q, err := c.interpolateParams("SELECT ?", []driver.Value{tc.arg})
			assert.NoError(t, err)
			assert.Equal(t, "SELECT "+tc.want, q)
		})
	}

	// Unknown types error out of both paths.
	_, err := c.buildExecutionParams([]driver.Value{struct{}{}})
	assert.Equal(t, ErrQueryUnknownType, err)
	_, err = c.interpolateParams("SELECT ?", []driver.Value{struct{}{}})
	assert.Equal(t, ErrQueryUnknownType, err)
}
