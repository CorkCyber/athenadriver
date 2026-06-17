// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"strings"

	"github.com/CorkCyber/athenadriver/athenareader/lib/configfx"
	"github.com/CorkCyber/athenadriver/athenareader/lib/output"
	"github.com/CorkCyber/athenadriver/athenareader/lib/queryfx"
	"go.uber.org/fx"
)

func main() {
	app := fx.New(opts(), fx.Options(fx.NopLogger))
	ctx := context.Background()
	app.Start(ctx)
	defer app.Stop(ctx)
}

func opts() fx.Option {
	return fx.Options(
		configfx.Module,
		queryfx.Module,
		fx.Invoke(queryAthena),
	)
}

func queryAthena(qad queryfx.QueryAndDBConnection, mc configfx.AthenaDriverConfig) {
	for _, query := range qad.Query {
		query = strings.Trim(query, " \n\t")
		if query == "" {
			continue
		}
		rows, err := qad.DB.Query(query)
		if err != nil {
			println("ERROR: " + err.Error())
			if mc.OutputConfig.Fastfail {
				return
			}
			continue
		}
		defer rows.Close()
		if mc.OutputConfig.Rowonly {
			output.PrettyPrintSQLRows(rows, mc.OutputConfig.Style, mc.OutputConfig.Render, mc.OutputConfig.Page)
		} else {
			output.PrettyPrintSQLColsRows(rows, mc.OutputConfig.Style, mc.OutputConfig.Render, mc.OutputConfig.Page)
		}
	}
}
