// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
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

func TestQueryAthenaSkipsEmptyQueries(t *testing.T) {
	qad := queryfx.QueryAndDBConnection{
		DB:    sql.OpenDB(failingConnector{}),
		Query: []string{"  \n\t"},
	}
	if err := queryAthena(qad, configfx.AthenaDriverConfig{}); err != nil {
		t.Fatalf("want nil for no-op query set, got %v", err)
	}
}
