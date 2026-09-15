// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/CorkCyber/athenadriver/athenareader/lib/configfx"
	"github.com/CorkCyber/athenadriver/athenareader/lib/output"
	"github.com/CorkCyber/athenadriver/athenareader/lib/queryfx"
)

func main() {
	os.Exit(run())
}

func run() int {
	mc, err := configfx.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: "+err.Error())
		return 1
	}
	qad, err := queryfx.New(mc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ERROR: "+err.Error())
		return 1
	}
	if err := queryAthena(qad, mc); err != nil {
		return 1
	}
	return 0
}

// queryAthena runs every query and prints its result set. It returns an
// error if any query failed, so the CLI can exit non-zero.
func queryAthena(qad queryfx.QueryAndDBConnection, mc configfx.AthenaDriverConfig) error {
	var failed error
	for _, query := range qad.Query {
		query = strings.Trim(query, " \n\t")
		if query == "" {
			continue
		}
		rows, err := qad.DB.Query(query)
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERROR: "+err.Error())
			failed = err
			if mc.OutputConfig.Fastfail {
				return failed
			}
			continue
		}
		if mc.OutputConfig.Rowonly {
			err = output.PrettyPrintSQLRows(rows, mc.OutputConfig.Style, mc.OutputConfig.Render, mc.OutputConfig.Page)
		} else {
			err = output.PrettyPrintSQLColsRows(rows, mc.OutputConfig.Style, mc.OutputConfig.Render, mc.OutputConfig.Page)
		}
		rows.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ERROR: "+err.Error())
			failed = err
			if mc.OutputConfig.Fastfail {
				return failed
			}
		}
	}
	return failed
}
