// SPDX-License-Identifier: MIT

package athenadriver

import (
	"database/sql/driver"
	"strings"
	"testing"
	"time"
)

// FuzzInterpolateParams stresses client-side param interpolation with a
// variety of arg types (string / int64 / uint64 / float64 / bool / []byte /
// time.Time / nil) drawn deterministically from the fuzzer's byte input.
// The type index rotates by position so each fuzz case exercises multiple
// branches of interpolateParams' type switch, not just strings.
//
// Assertions when interpolation returns nil error:
//  1. No unquoted `?` remains: a stray placeholder outside string literals
//     means interpolation silently skipped a slot.
//  2. Output non-empty for non-empty input.
//  3. Output length below a coarse per-arg escape blow-up bound: a runaway
//     allocation signals the pre-allocation heuristic in params.go drifted.
//
// Panics and nil-deref are also captured; go test's default recovery
// reports them as failures.
func FuzzInterpolateParams(f *testing.F) {
	f.Add("SELECT ?", "gopher", uint8(0))
	f.Add("SELECT ?, ?, ?", "a\x00b", uint8(1))
	f.Add("SELECT ?", "O'Brien", uint8(2))
	f.Add("SELECT ?", "line\nbreak\r\ttab", uint8(3))
	f.Add("SELECT '?'", "not-a-param", uint8(4))
	f.Add("SELECT ?", "", uint8(5))
	f.Add("SELECT ?, ?", "\\", uint8(6))
	f.Add("", "", uint8(0))

	c := &Connection{}
	f.Fuzz(func(t *testing.T, query, sarg string, kind uint8) {
		nQ := strings.Count(query, "?")
		args := make([]driver.Value, nQ)
		for i := range args {
			switch (kind + uint8(i)) % 8 {
			case 0:
				args[i] = sarg
			case 1:
				args[i] = int64(len(sarg))
			case 2:
				args[i] = uint64(len(sarg))
			case 3:
				args[i] = float64(len(sarg)) + 0.5
			case 4:
				args[i] = (len(sarg) % 2) == 0
			case 5:
				args[i] = []byte(sarg)
			case 6:
				args[i] = time.Unix(int64(len(sarg)), 0).UTC()
			case 7:
				args[i] = nil
			}
		}
		out, err := c.interpolateParams(query, args)
		if err != nil {
			return
		}

		// Property 1: every `?` in the original query must be substituted.
		// interpolateParams is not SQL-aware, so a `?` in the output can only
		// have come from a string / []byte arg. Count them and demand they
		// match: catches silent skips of arg positions.
		argQs := 0
		for _, a := range args {
			switch v := a.(type) {
			case string:
				argQs += strings.Count(v, "?")
			case []byte:
				argQs += strings.Count(string(v), "?")
			}
		}
		if got := strings.Count(out, "?"); got != argQs {
			t.Fatalf("output `?` count = %d, want %d (from args); query=%q out=%q", got, argQs, query, out)
		}

		// Property 2: non-empty output for non-empty input.
		if query != "" && out == "" {
			t.Fatalf("empty output for non-empty query %q", query)
		}

		// Property 3: length bound. Each arg byte at most 4x via escaping;
		// per-arg overhead 64 bytes for quotes / type-specific formatting.
		var argBytes int
		for _, a := range args {
			switch v := a.(type) {
			case string:
				argBytes += len(v)
			case []byte:
				argBytes += len(v)
			}
		}
		maxLen := len(query) + argBytes*4 + nQ*64
		if len(out) > maxLen+256 {
			t.Fatalf("output length %d exceeds bound %d (query=%q)", len(out), maxLen, query)
		}
	})
}
