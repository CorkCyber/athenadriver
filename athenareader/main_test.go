// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/CorkCyber/athenadriver/athenareader/lib/configfx"
	"github.com/CorkCyber/athenadriver/athenareader/lib/queryfx"
)

// failingConnector fails every connection attempt, so DB.Query fails.
type failingConnector struct{}

func (failingConnector) Connect(context.Context) (driver.Conn, error) {
	return nil, errors.New("boom")
}
func (failingConnector) Driver() driver.Driver { return nil }

func TestQueryAthenaReturnsErrorOnFailure(t *testing.T) {
	qad := queryfx.QueryAndDBConnection{
		DB:    sql.OpenDB(failingConnector{}),
		Query: []string{"select 1"},
	}
	if err := queryAthena(qad, configfx.AthenaDriverConfig{}); err == nil {
		t.Fatal("want error from failing query, got nil")
	}
}

// streamErrConnector serves one row stream that fails mid-read: Next reports
// an error, which surfaces through rows.Err() exactly like a clean EOF would
// not.
type streamErrConnector struct{}

func (c streamErrConnector) Connect(context.Context) (driver.Conn, error) {
	return streamErrConn{}, nil
}
func (c streamErrConnector) Driver() driver.Driver { return nil }

type streamErrConn struct{}

func (streamErrConn) Prepare(string) (driver.Stmt, error) { return streamErrStmt{}, nil }
func (streamErrConn) Close() error                        { return nil }
func (streamErrConn) Begin() (driver.Tx, error)           { return nil, errors.New("no tx") }

type streamErrStmt struct{}

func (streamErrStmt) Close() error  { return nil }
func (streamErrStmt) NumInput() int { return 0 }
func (streamErrStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, errors.New("no exec")
}
func (streamErrStmt) Query([]driver.Value) (driver.Rows, error) { return &streamErrRows{}, nil }

type streamErrRows struct{}

func (*streamErrRows) Columns() []string { return []string{"c"} }
func (*streamErrRows) Close() error      { return nil }
func (*streamErrRows) Next([]driver.Value) error {
	return errors.New("connection reset mid-stream")
}

func TestQueryAthenaReturnsErrorOnStreamFailure(t *testing.T) {
	qad := queryfx.QueryAndDBConnection{
		DB:    sql.OpenDB(streamErrConnector{}),
		Query: []string{"select 1"},
	}
	if err := queryAthena(qad, configfx.AthenaDriverConfig{}); err == nil {
		t.Fatal("want error from failing row stream, got nil")
	}
}

func TestQueryAthenaSkipsEmptyQueries(t *testing.T) {
	qad := queryfx.QueryAndDBConnection{
		DB:    sql.OpenDB(failingConnector{}),
		Query: []string{"  \n\t"},
	}
	if err := queryAthena(qad, configfx.AthenaDriverConfig{}); err != nil {
		t.Fatalf("want nil for no-op query set, got %v", err)
	}
}

// countingFailConnector records how many times Connect was called, so tests
// can prove Fastfail stops at the first failure instead of running every
// query in the set.
type countingFailConnector struct{ calls *int }

func (c countingFailConnector) Connect(context.Context) (driver.Conn, error) {
	*c.calls++
	return nil, errors.New("boom")
}
func (countingFailConnector) Driver() driver.Driver { return nil }

func TestQueryAthenaFastfailStopsAtFirstFailure(t *testing.T) {
	calls := 0
	qad := queryfx.QueryAndDBConnection{
		DB:    sql.OpenDB(countingFailConnector{calls: &calls}),
		Query: []string{"select 1", "select 2", "select 3"},
	}
	mc := configfx.AthenaDriverConfig{OutputConfig: configfx.ReaderOutputConfig{Fastfail: true}}
	if err := queryAthena(qad, mc); err == nil {
		t.Fatal("want error from failing query, got nil")
	}
	if calls != 1 {
		t.Errorf("Connect called %d times, want 1 (fastfail must stop at the first failure)", calls)
	}
}

func TestQueryAthenaWithoutFastfailRunsEveryQuery(t *testing.T) {
	calls := 0
	qad := queryfx.QueryAndDBConnection{
		DB:    sql.OpenDB(countingFailConnector{calls: &calls}),
		Query: []string{"select 1", "select 2", "select 3"},
	}
	mc := configfx.AthenaDriverConfig{OutputConfig: configfx.ReaderOutputConfig{Fastfail: false}}
	if err := queryAthena(qad, mc); err == nil {
		t.Fatal("want error from failing query, got nil")
	}
	if calls != 3 {
		t.Errorf("Connect called %d times, want 3 (no fastfail must run every query)", calls)
	}
}

// TestRun_ConfigErrorExitsNonZero pins run()'s exit-code mapping for a
// configfx.New() failure: a malformed config file must make run() return 1,
// not panic or silently succeed. run()'s success path and its queryfx.New()
// failure path both require a live Athena connection to exercise honestly
// and aren't covered here; queryAthena (the part of run() that owns actual
// query/error handling) is covered directly by the tests above instead.
func TestRun_ConfigErrorExitsNonZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.WriteFile(filepath.Join(home, "athenareader.config"),
		[]byte("athenareader: [not, a, map\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldArgs, oldFlags := os.Args, flag.CommandLine
	defer func() { os.Args, flag.CommandLine = oldArgs, oldFlags }()
	os.Args = []string{"athenareader"}
	flag.CommandLine = flag.NewFlagSet("athenareader", flag.ContinueOnError)

	if code := run(); code != 1 {
		t.Errorf("run() = %d, want 1 for a malformed config file", code)
	}
}
