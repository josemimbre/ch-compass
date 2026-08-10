// Package report renders analysis results for the terminal and for
// machine-readable consumption.
package report

import (
	"encoding/json"
	"io"

	"github.com/josemimbre/ch-compass/internal/analyze"
)

type jsonReport struct {
	ClickHouseVersion string         `json:"clickhouse_version"`
	Databases         []jsonDatabase `json:"databases"`
}

type jsonDatabase struct {
	Name            string                   `json:"name"`
	Tables          int                      `json:"tables"`
	Views           int                      `json:"views"`
	Recommendations []analyze.Recommendation `json:"recommendations"`
}

// WriteJSON writes results as a single JSON document to w.
func WriteJSON(w io.Writer, results []analyze.Result, version string, minSeverity *analyze.Severity) error {
	out := jsonReport{
		ClickHouseVersion: version,
		Databases:         make([]jsonDatabase, len(results)),
	}

	for i, r := range results {
		out.Databases[i] = jsonDatabase{
			Name:            r.Database,
			Tables:          len(r.Tables),
			Views:           len(r.Views),
			Recommendations: analyze.Filter(r.Recommendations, minSeverity),
		}
	}

	enc := json.NewEncoder(w)
	return enc.Encode(out)
}
