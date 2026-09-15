// SPDX-License-Identifier: MIT

// Package output renders *sql.Rows to terminal-friendly formats (table,
// CSV, markdown, HTML) using jedib0t/go-pretty. It lives in the
// athenareader CLI module so the driver core does not pull a UI
// formatting dep into library consumers' module graph.
package output

import (
	"database/sql"
	"os"

	"github.com/jedib0t/go-pretty/v6/table"
)

// tableStyles maps the string names PrettyPrint* accept to the
// jedib0t/go-pretty styles. Anything not in the map falls back to
// table.StyleDefault.
var tableStyles = map[string]table.Style{
	"StyleBold":                       table.StyleBold,
	"StyleColoredBright":              table.StyleColoredBright,
	"StyleColoredDark":                table.StyleColoredDark,
	"StyleColoredBlackOnBlueWhite":    table.StyleColoredBlackOnBlueWhite,
	"StyleColoredBlackOnCyanWhite":    table.StyleColoredBlackOnCyanWhite,
	"StyleColoredBlackOnGreenWhite":   table.StyleColoredBlackOnGreenWhite,
	"StyleColoredBlackOnMagentaWhite": table.StyleColoredBlackOnMagentaWhite,
	"StyleColoredBlackOnYellowWhite":  table.StyleColoredBlackOnYellowWhite,
	"StyleColoredBlackOnRedWhite":     table.StyleColoredBlackOnRedWhite,
	"StyleColoredBlueWhiteOnBlack":    table.StyleColoredBlueWhiteOnBlack,
	"StyleColoredCyanWhiteOnBlack":    table.StyleColoredCyanWhiteOnBlack,
	"StyleColoredGreenWhiteOnBlack":   table.StyleColoredGreenWhiteOnBlack,
	"StyleColoredMagentaWhiteOnBlack": table.StyleColoredMagentaWhiteOnBlack,
	"StyleColoredRedWhiteOnBlack":     table.StyleColoredRedWhiteOnBlack,
	"StyleColoredYellowWhiteOnBlack":  table.StyleColoredYellowWhiteOnBlack,
	"StyleDouble":                     table.StyleDouble,
	"StyleLight":                      table.StyleLight,
	"StyleRounded":                    table.StyleRounded,
}

func getTableStyle(style string) table.Style {
	if s, ok := tableStyles[style]; ok {
		return s
	}
	return table.StyleDefault
}

func renderTable(renderType string, w table.Writer) string {
	switch renderType {
	case "markdown":
		return w.RenderMarkdown()
	case "table":
		return w.Render()
	case "html":
		return w.RenderHTML()
	}
	return w.RenderCSV()
}

// prettyPrint streams sql.Rows into a table writer. When withHeader is
// true, a header row built from rows.Columns() is emitted first.
// It returns any error that ended the row stream early, so a truncated
// result set is never mistaken for a complete one.
func prettyPrint(rows *sql.Rows, style, render string, page int, withHeader bool) error {
	if rows == nil {
		return nil
	}
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	columns, err := rows.Columns()
	if err != nil {
		return err
	}
	if withHeader && len(columns) > 0 {
		header := make(table.Row, len(columns))
		for i, c := range columns {
			header[i] = c
		}
		t.AppendHeader(header)
	}
	for rows.Next() {
		rawResult := make([][]byte, len(columns))
		scanTargets := make([]any, len(columns))
		for i := range rawResult {
			scanTargets[i] = &rawResult[i]
		}
		if err := rows.Scan(scanTargets...); err != nil {
			return err
		}
		row := make(table.Row, len(columns))
		for i, cell := range rawResult {
			row[i] = string(cell)
		}
		t.AppendRow(row)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	t.SetPageSize(page)
	t.SetStyle(getTableStyle(style))
	renderTable(render, t)
	return nil
}

// PrettyPrintSQLRows prints rows in the given style and render format,
// without a header.
func PrettyPrintSQLRows(rows *sql.Rows, style string, render string, page int) error {
	return prettyPrint(rows, style, render, page, false)
}

// PrettyPrintSQLColsRows prints rows in the given style and render format,
// with a header row built from rows.Columns().
func PrettyPrintSQLColsRows(rows *sql.Rows, style string, render string, page int) error {
	return prettyPrint(rows, style, render, page, true)
}
