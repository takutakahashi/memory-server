// Package curator provides the Curator service, which periodically processes
// pending Inbox entries and organises them into Memories and KB pages.
//
// The Curator launches a TypeScript agent script (scripts/curator_agent.ts)
// via Bun as a subprocess. The agent connects to the memory-server MCP
// endpoint and autonomously decides how to handle each Inbox entry —
// searching for existing content, updating it when relevant, or creating
// new memories / KB pages as appropriate.
//
// API-key resolution order:
//
//  1. RunInput.AnthropicAPIKey (populated from the X-Anthropic-Key request header)
//  2. ANTHROPIC_API_KEY from the environment inherited at server start-up
package curator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/memory-server/internal/inbox"
)

// ----------------------------------------------------------------------------
// Curator
// ----------------------------------------------------------------------------

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
// It fetches up to 100 pending Inbox entries, passes them to the TypeScript
// curator agent (via Bun) connected to the memory-server MCP endpoint.
// The agent autonomously searches, creates, and updates memories/KB pages.
// The agent returns the list of processed inbox_ids, which are then marked
// as "processed" in the Inbox.
func (c *Curator) Run(ctx context.Context, input RunInput) (*RunResult, error) {
	runID := uuid.New().String()
	startedAt := time.Now().UTC()
	log.Printf("[curator] run %s started", runID)

	result := &RunResult{
		RunID:     runID,
		StartedAt: startedAt,
	}

	// Fetch pending entries.
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
		log.Printf("[curator] run %s completed: noop (no pending entries)", runID)
		c.setLastResult(result)
		return result, nil
	}

	// Run the TypeScript agent — it connects to the MCP server and handles
	// everything: search, create, update.
	processedIDs, err := c.runAgent(ctx, input, entries)
	if err != nil {
		result.CompletedAt = time.Now().UTC()
		result.Status = RunStatusFailed
		result.Message = fmt.Sprintf("agent error: %v", err)
		c.setLastResult(result)
		return result, fmt.Errorf("run agent: %w", err)
	}

	// Mark entries reported as handled by the agent.
	processed := 0
	for _, id := range processedIDs {
		if err := c.inboxSvc.MarkProcessed(ctx, id); err != nil {
			log.Printf("[curator] run %s: mark processed %s: %v", runID, id, err)
			continue
		}
		processed++
	}

	result.CompletedAt = time.Now().UTC()
	result.Status = RunStatusSuccess
	result.ProcessedCount = processed
	result.Message = fmt.Sprintf("processed %d inbox entries", processed)

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

// ----------------------------------------------------------------------------
// Agent subprocess
// ----------------------------------------------------------------------------

// runAgent serialises entries to JSON, launches the Bun/TypeScript subprocess,
// and returns the list of inbox_ids that the agent successfully processed.
func (c *Curator) runAgent(ctx context.Context, input RunInput, entries []*inbox.Entry) ([]string, error) {
	inputJSON, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("marshal entries: %w", err)
	}

	scriptPath := os.Getenv("CURATOR_AGENT_SCRIPT")
	if scriptPath == "" {
		scriptPath = "scripts/curator_agent.ts"
	}

	cmd := exec.CommandContext(ctx, "bun", "run", scriptPath)
	cmd.Env = buildEnv(input.AnthropicAPIKey)
	cmd.Stdin = bytes.NewReader(inputJSON)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("[curator] launching agent: bun run %s (%d entries)", scriptPath, len(entries))

	if err := cmd.Run(); err != nil {
		if stderr.Len() > 0 {
			log.Printf("[curator] agent stderr:\n%s", stderr.String())
		}
		return nil, fmt.Errorf("agent subprocess: %w", err)
	}

	if stderr.Len() > 0 {
		log.Printf("[curator] agent diagnostics:\n%s", stderr.String())
	}

	var processedIDs []string
	if err := json.Unmarshal(stdout.Bytes(), &processedIDs); err != nil {
		return nil, fmt.Errorf("unmarshal agent output: %w (output: %s)", err, stdout.String())
	}

	return processedIDs, nil
}

// buildEnv constructs the subprocess environment.
//
// It starts from the current process's full environment (os.Environ()) so that
// all configuration — AWS credentials, region, table names, etc. — is inherited
// automatically. When headerAPIKey is non-empty it replaces the
// ANTHROPIC_API_KEY entry so that per-request keys take precedence over the
// server-level default.
func buildEnv(headerAPIKey string) []string {
	env := os.Environ()
	if headerAPIKey == "" {
		return env
	}

	result := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			result = append(result, e)
		}
	}
	return append(result, "ANTHROPIC_API_KEY="+headerAPIKey)
}
