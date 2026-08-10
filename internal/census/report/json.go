package report

import (
	"encoding/json"
	"io"

	"github.com/josemimbre/ch-compass/internal/census"
)

// WriteJSON writes metrics as a JSON array to w.
func WriteJSON(w io.Writer, metrics []census.Metric) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(metrics)
}
