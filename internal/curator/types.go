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

// RunInput holds options for a single Curator run.
type RunInput struct {
	// AnthropicAPIKey is the Anthropic API key to use for this run.
	// When non-empty it overrides the ANTHROPIC_API_KEY environment variable
	// that was present at server start-up. Typically populated from the
	// X-Anthropic-Key HTTP request header.
	AnthropicAPIKey string
}
