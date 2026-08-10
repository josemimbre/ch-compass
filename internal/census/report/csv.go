package report

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"

	"github.com/josemimbre/ch-compass/internal/census"
)

// WriteCSV writes metrics as CSV (metric,labels,value) to w. labels is the
// JSON encoding of the Labels map (e.g. `{"engine":"MergeTree"}`), empty for
// scalar metrics — CSV has no native map type, and this keeps the column
// count fixed regardless of how many label keys a given metric has.
func WriteCSV(w io.Writer, metrics []census.Metric) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"metric", "labels", "value"}); err != nil {
		return err
	}
	for _, m := range metrics {
		labels, err := encodeLabels(m.Labels)
		if err != nil {
			return err
		}
		if err := cw.Write([]string{m.Name, labels, strconv.FormatFloat(m.Value, 'g', -1, 64)}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func encodeLabels(labels map[string]string) (string, error) {
	if len(labels) == 0 {
		return "", nil
	}
	b, err := json.Marshal(labels)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
