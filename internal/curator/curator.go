// Package curator provides the Curator service, which periodically processes
// pending Inbox entries and organizes them into Memories and KB pages.
//
// This is a "no-op Curator": it finds all pending Inbox entries and marks them
// as processed without performing any actual analysis or extraction.
// A future implementation will use an AI model to distill Inbox entries into
// Memories and Knowledge Base pages.
package curator

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/memory-server/internal/inbox"
)

// Curator orchestrates the periodic processing of Inbox entries.
type Curator struct {
	inboxSvc   *inbox.Service
	mu         sync.RWMutex
	lastResult *RunResult
}

// New creates a new Curator.
func New(inboxSvc *inbox.Service) *Curator {
	return &Curator{
		inboxSvc: inboxSvc,
	}
}

// Run executes one Curator cycle.
//
// No-op behaviour: fetches all pending Inbox entries and marks them as
// processed. No AI analysis, Memory extraction, or KB page generation is
// performed in this implementation.
func (c *Curator) Run(ctx context.Context) (*RunResult, error) {
	runID := uuid.New().String()
	startedAt := time.Now().UTC()
	log.Printf("[curator] run %s started", runID)

	result := &RunResult{
		RunID:     runID,
		StartedAt: startedAt,
	}

	entries, err := c.inboxSvc.ListPending(ctx, 100)
	if err != nil {
		result.CompletedAt = time.Now().UTC()
		result.Status = RunStatusFailed
		result.Message = fmt.Sprintf("list pending entries: %v", err)
		c.setLastResult(result)
		return result, fmt.Errorf("list pending entries: %w", err)
	}

	if len(entries) == 0 {
		result.CompletedAt = time.Now().UTC()
		result.Status = RunStatusNoop
		result.Message = "no pending inbox entries found"
		result.ProcessedCount = 0
		log.Printf("[curator] run %s completed: noop (no pending entries)", runID)
		c.setLastResult(result)
		return result, nil
	}

	processed := 0
	for _, e := range entries {
		if err := c.inboxSvc.MarkProcessed(ctx, e.InboxID); err != nil {
			log.Printf("[curator] run %s: failed to mark entry %s as processed: %v", runID, e.InboxID, err)
			continue
		}
		processed++
	}

	result.CompletedAt = time.Now().UTC()
	result.Status = RunStatusSuccess
	result.ProcessedCount = processed
	result.Message = fmt.Sprintf("processed %d inbox entries (no-op: entries marked as processed without analysis)", processed)

	log.Printf("[curator] run %s completed: processed %d entries", runID, processed)
	c.setLastResult(result)
	return result, nil
}

// GetLastResult returns the result of the most recent Curator run.
// Returns nil if the Curator has never been run.
func (c *Curator) GetLastResult() *RunResult {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastResult
}

func (c *Curator) setLastResult(r *RunResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastResult = r
}
