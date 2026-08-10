// Package termtable renders bordered tables for terminal ("text" format)
// output, shared by every report package (internal/analyze/report,
// internal/census/report) so each doesn't redefine the same lipgloss
// styling independently.
package termtable

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// Bold and Faint are the two text styles every report's text format uses
// for headings and de-emphasized annotations, respectively.
var (
	Bold  = lipgloss.NewStyle().Bold(true)
	Faint = lipgloss.NewStyle().Faint(true)

	borderColor = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

// Render draws headers/rows as a rounded-border table with a bolded header
// row — the one table look every text report format uses.
func Render(headers []string, rows [][]string) string {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(borderColor).
		StyleFunc(func(row, col int) lipgloss.Style {
			style := lipgloss.NewStyle().Padding(0, 1)
			if row == table.HeaderRow {
				return style.Bold(true)
			}
			return style
		}).
		Headers(headers...).
		Rows(rows...)
	return t.Render()
}
