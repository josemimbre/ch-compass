package analyze

import (
	"fmt"
	"time"
)

// stuckThreshold is how long an incomplete mutation must run before it's
// flagged as stuck.
const stuckThreshold = time.Hour

// stuckMutations flags incomplete mutations running longer than
// stuckThreshold, since they can block merges and consume resources.
func stuckMutations(mutations []MutationInfo, database string, now time.Time) []Recommendation {
	var recs []Recommendation

	for _, m := range mutations {
		if m.IsDone {
			continue
		}

		running := now.Sub(m.CreateTime)
		if running <= stuckThreshold {
			continue
		}

		recs = append(recs, Recommendation{
			Type:       TypeStuckMutation,
			Object:     m.Table,
			Database:   database,
			Severity:   SeverityHigh,
			Message:    fmt.Sprintf("Mutation '%s' has been running for %.1fh. Command: %s", m.MutationID, running.Hours(), m.Command),
			Suggestion: fmt.Sprintf("KILL MUTATION WHERE mutation_id = '%s'", m.MutationID),
		})
	}

	return recs
}
