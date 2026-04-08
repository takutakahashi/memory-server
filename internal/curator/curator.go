// Package curator provides the Curator service, which periodically processes
// pending Inbox entries and organises them into Memories and KB pages.
//
// The Curator launches a TypeScript agent script (scripts/curator_agent.ts)
// via Bun as a subprocess. The agent uses the Anthropic claude-agent-sdk to
// classify each Inbox entry and returns a JSON action list. The Go service then
// calls the Memory and KB services to persist the results.
//
// API-key resolution order:
//
//  1. RunInput.AnthropicAPIKey (populated from the X-Anthropic-Key request header)
//  2. ANTHROPIC_API_KEY from the environment that was inherited at server start-up
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
	"github.com/takutakahashi/memory-server/internal/kb"
	"github.com/takutakahashi/memory-server/internal/memory"
)

// ----------------------------------------------------------------------------
// Curator
// ----------------------------------------------------------------------------

// Curator orchestrates the periodic processing of Inbox entries.
type Curator struct {
	inboxSvc   *inbox.Service
	memorySvc  *memory.Service
	kbSvc      *kb.Service
	mu         sync.RWMutex
	lastResult *RunResult
}

// New creates a new Curator.
func New(inboxSvc *inbox.Service, memorySvc *memory.Service, kbSvc *kb.Service) *Curator {
	return &Curator{
		inboxSvc:  inboxSvc,
		memorySvc: memorySvc,
		kbSvc:     kbSvc,
	}
}

// Run executes one Curator cycle.
//
// It fetches up to 100 pending Inbox entries, passes them to the TypeScript
// curator agent (via Bun), interprets the returned action list, and persists
// the resulting Memories / KB pages. Successfully processed entries are marked
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

	// Call the TypeScript agent.
	actions, err := c.runAgent(ctx, input, entries)
	if err != nil {
		result.CompletedAt = time.Now().UTC()
		result.Status = RunStatusFailed
		result.Message = fmt.Sprintf("agent error: %v", err)
		c.setLastResult(result)
		return result, fmt.Errorf("run agent: %w", err)
	}

	// Apply actions: create resources and mark entries as processed.
	processed := c.applyActions(ctx, runID, actions)

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

// agentAction is the per-entry output produced by the TypeScript agent.
type agentAction struct {
	InboxID    string          `json:"inbox_id"`
	Action     string          `json:"action"` // "memory" | "kb" | "both" | "skip"
	Memory     *agentMemory    `json:"memory,omitempty"`
	KBPage     *agentKBPage    `json:"kb_page,omitempty"`
	SkipReason string          `json:"skip_reason,omitempty"`
}

type agentMemory struct {
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
	Scope   string   `json:"scope"`
}

type agentKBPage struct {
	Title    string   `json:"title"`
	Slug     string   `json:"slug"`
	Content  string   `json:"content"`
	Summary  string   `json:"summary"`
	Category string   `json:"category"`
	Tags     []string `json:"tags"`
	Scope    string   `json:"scope"`
}

// runAgent serialises entries to JSON, launches the Bun/TypeScript subprocess,
// and deserialises the returned action list.
func (c *Curator) runAgent(ctx context.Context, input RunInput, entries []*inbox.Entry) ([]agentAction, error) {
	// Serialise entries for stdin.
	inputJSON, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("marshal entries: %w", err)
	}

	// Resolve the script path.
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

	var actions []agentAction
	if err := json.Unmarshal(stdout.Bytes(), &actions); err != nil {
		return nil, fmt.Errorf("unmarshal agent output: %w (output: %s)", err, stdout.String())
	}

	return actions, nil
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

	// Replace ANTHROPIC_API_KEY with the per-request key.
	result := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			result = append(result, e)
		}
	}
	return append(result, "ANTHROPIC_API_KEY="+headerAPIKey)
}

// ----------------------------------------------------------------------------
// Action application
// ----------------------------------------------------------------------------

// applyActions iterates over the agent's action list, creates the requested
// resources via the Memory and KB services, and marks each handled entry as
// processed in the Inbox. It returns the number of entries that were
// successfully processed.
func (c *Curator) applyActions(ctx context.Context, runID string, actions []agentAction) int {
	processed := 0

	for _, a := range actions {
		if err := c.applyAction(ctx, a); err != nil {
			log.Printf("[curator] run %s: entry %s action=%s failed: %v", runID, a.InboxID, a.Action, err)
			continue
		}

		if err := c.inboxSvc.MarkProcessed(ctx, a.InboxID); err != nil {
			log.Printf("[curator] run %s: mark processed %s: %v", runID, a.InboxID, err)
			continue
		}
		processed++
	}

	return processed
}

// applyAction executes a single agent action (create memory, KB page, or skip).
func (c *Curator) applyAction(ctx context.Context, a agentAction) error {
	switch a.Action {
	case "memory":
		return c.createMemory(ctx, a)
	case "kb":
		return c.createKBPage(ctx, a)
	case "both":
		if err := c.createMemory(ctx, a); err != nil {
			return fmt.Errorf("create memory: %w", err)
		}
		if err := c.createKBPage(ctx, a); err != nil {
			return fmt.Errorf("create kb page: %w", err)
		}
	case "skip":
		log.Printf("[curator] skipping entry %s: %s", a.InboxID, a.SkipReason)
	default:
		log.Printf("[curator] unknown action %q for entry %s, skipping", a.Action, a.InboxID)
	}
	return nil
}

// createMemory persists a new Memory entry using the agent's payload.
func (c *Curator) createMemory(ctx context.Context, a agentAction) error {
	if c.memorySvc == nil {
		return fmt.Errorf("memory service not available")
	}
	if a.Memory == nil {
		return fmt.Errorf("action=memory but memory payload is nil")
	}

	// Resolve the user_id from the Inbox entry so that memories are scoped
	// to the correct owner.
	userID, err := c.userIDForEntry(ctx, a.InboxID)
	if err != nil {
		log.Printf("[curator] could not resolve user_id for entry %s, using default", a.InboxID)
		userID = "default"
	}

	_, err = c.memorySvc.Add(ctx, memory.AddInput{
		UserID:  userID,
		Content: a.Memory.Content,
		Tags:    a.Memory.Tags,
		Scope:   memory.Scope(a.Memory.Scope),
	})
	return err
}

// createKBPage persists a new Knowledge Base page using the agent's payload.
func (c *Curator) createKBPage(ctx context.Context, a agentAction) error {
	if c.kbSvc == nil {
		return fmt.Errorf("kb service not available")
	}
	if a.KBPage == nil {
		return fmt.Errorf("action=kb but kb_page payload is nil")
	}

	userID, err := c.userIDForEntry(ctx, a.InboxID)
	if err != nil {
		log.Printf("[curator] could not resolve user_id for entry %s, using default", a.InboxID)
		userID = "default"
	}

	_, err = c.kbSvc.Create(ctx, kb.CreateInput{
		UserID:   userID,
		Title:    a.KBPage.Title,
		Slug:     a.KBPage.Slug,
		Content:  a.KBPage.Content,
		Summary:  a.KBPage.Summary,
		Category: a.KBPage.Category,
		Tags:     a.KBPage.Tags,
		Scope:    kb.Scope(a.KBPage.Scope),
	})
	return err
}

// userIDForEntry retrieves the owner user_id for the given Inbox entry.
func (c *Curator) userIDForEntry(ctx context.Context, inboxID string) (string, error) {
	e, err := c.inboxSvc.Get(ctx, inboxID)
	if err != nil {
		return "", err
	}
	if e.UserID == "" {
		return "default", nil
	}
	return e.UserID, nil
}
