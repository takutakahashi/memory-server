package curator

import "time"

// RunStatus represents the outcome of a Curator run.
type RunStatus string

const (
	// RunStatusSuccess means the Curator run completed without errors.
	RunStatusSuccess RunStatus = "success"
	// RunStatusFailed means the Curator run encountered an error.
	RunStatusFailed RunStatus = "failed"
	// RunStatusNoop means no pending entries were found.
	RunStatusNoop RunStatus = "noop"
)

// RunResult holds the result of a single Curator run.
type RunResult struct {
	RunID          string    `json:"run_id"`
	StartedAt      time.Time `json:"started_at"`
	CompletedAt    time.Time `json:"completed_at"`
	Status         RunStatus `json:"run_status"`
	ProcessedCount int       `json:"processed_count"`
	Message        string    `json:"message"`
}
