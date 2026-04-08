package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/takutakahashi/memory-server/internal/curator"
	"github.com/takutakahashi/memory-server/internal/inbox"
	"github.com/takutakahashi/memory-server/internal/kb"
	"github.com/takutakahashi/memory-server/internal/memory"

	"github.com/aws/aws-sdk-go-v2/config"
)

// Server holds all components for the Memory MCP server.
type Server struct {
	memorySvc *memory.Service
	inboxSvc  *inbox.Service
	kbSvc     *kb.Service
	curator   *curator.Curator
	mcpServer *mcp.Server
}

// NewServerWithServices creates a new Server using existing services.
func NewServerWithServices(
	memorySvc *memory.Service,
	inboxSvc *inbox.Service,
	kbSvc *kb.Service,
	cur *curator.Curator,
) *Server {
	serverName := os.Getenv("MCP_SERVER_NAME")
	if serverName == "" {
		serverName = "memory-server"
	}
	serverVersion := os.Getenv("MCP_SERVER_VERSION")
	if serverVersion == "" {
		serverVersion = "1.0.0"
	}

	s := &Server{
		memorySvc: memorySvc,
		inboxSvc:  inboxSvc,
		kbSvc:     kbSvc,
		curator:   cur,
		mcpServer: mcp.NewServer(&mcp.Implementation{
			Name:    serverName,
			Version: serverVersion,
		}, nil),
	}

	s.registerTools()
	return s
}

// NewServerWithService creates a new Server using an existing memory Service (legacy).
func NewServerWithService(svc *memory.Service) *Server {
	return NewServerWithServices(svc, nil, nil, nil)
}

// NewServer creates a new Server by loading AWS config and initializing all components.
func NewServer(ctx context.Context) (*Server, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	memorySvc := memory.NewService(cfg)
	inboxSvc := inbox.NewService(cfg)
	kbSvc := kb.NewService(cfg)
	cur := curator.New(inboxSvc)

	return NewServerWithServices(memorySvc, inboxSvc, kbSvc, cur), nil
}

// --- Tool input types: Memory ---

type AddMemoryInput struct {
	UserID  string       `json:"user_id" jsonschema:"User ID"`
	Content string       `json:"content" jsonschema:"Content of the memory"`
	Tags    []string     `json:"tags" jsonschema:"Tags for the memory"`
	Scope   memory.Scope `json:"scope" jsonschema:"Visibility scope: 'private' (default, owner only) or 'public' (all users)"`
}

type SearchMemoriesInput struct {
	UserID string   `json:"user_id" jsonschema:"User ID"`
	Query  string   `json:"query" jsonschema:"Natural language search query"`
	Tags   []string `json:"tags" jsonschema:"Tags for OR-filtered search (max 5)"`
	Limit  int      `json:"limit" jsonschema:"Number of results to return (default: 10)"`
}

type ListMemoriesInput struct {
	UserID    string `json:"user_id" jsonschema:"User ID"`
	Limit     int    `json:"limit" jsonschema:"Number of results per page (default: 20)"`
	NextToken string `json:"next_token" jsonschema:"Pagination token"`
}

type GetMemoryInput struct {
	MemoryID string `json:"memory_id" jsonschema:"Memory ID"`
}

type UpdateMemoryInput struct {
	MemoryID string       `json:"memory_id" jsonschema:"Memory ID"`
	Content  string       `json:"content" jsonschema:"New content"`
	Tags     []string     `json:"tags" jsonschema:"New tags"`
	Scope    memory.Scope `json:"scope" jsonschema:"New visibility scope: 'private' (owner only) or 'public' (all users)"`
}

type DeleteMemoryInput struct {
	MemoryID string `json:"memory_id" jsonschema:"Memory ID"`
}

// --- Tool input types: Inbox ---

type AddInboxInput struct {
	UserID  string   `json:"user_id" jsonschema:"User ID"`
	Content string   `json:"content" jsonschema:"Raw content to store in the inbox"`
	Source  string   `json:"source" jsonschema:"Origin of the content (e.g. 'slack', 'chat', 'manual')"`
	Tags    []string `json:"tags" jsonschema:"Optional tags"`
}

type ListInboxInput struct {
	UserID    string `json:"user_id" jsonschema:"User ID"`
	Status    string `json:"status" jsonschema:"Filter by status: 'pending', 'processed', or 'archived'. Leave empty for all."`
	Limit     int    `json:"limit" jsonschema:"Number of results per page (default: 20)"`
	NextToken string `json:"next_token" jsonschema:"Pagination token"`
}

type GetInboxInput struct {
	InboxID string `json:"inbox_id" jsonschema:"Inbox entry ID"`
}

type ArchiveInboxInput struct {
	InboxID string `json:"inbox_id" jsonschema:"Inbox entry ID to archive"`
}

type DeleteInboxInput struct {
	InboxID string `json:"inbox_id" jsonschema:"Inbox entry ID to delete"`
}

// --- Tool input types: Knowledge Base ---

type CreateKBPageInput struct {
	UserID          string   `json:"user_id" jsonschema:"User ID"`
	Title           string   `json:"title" jsonschema:"Page title"`
	Slug            string   `json:"slug" jsonschema:"URL-friendly slug (auto-generated from title if empty)"`
	Content         string   `json:"content" jsonschema:"Markdown content"`
	Summary         string   `json:"summary" jsonschema:"Short summary (used for search)"`
	Category        string   `json:"category" jsonschema:"Category path (e.g. 'dev/backend')"`
	Scope           kb.Scope `json:"scope" jsonschema:"Visibility: 'private' (default) or 'public'"`
	Tags            []string `json:"tags" jsonschema:"Tags"`
	SourceMemoryIDs []string `json:"source_memory_ids" jsonschema:"Memory IDs that were used to build this page"`
}

type UpdateKBPageInput struct {
	PageID          string   `json:"page_id" jsonschema:"Page ID"`
	Title           string   `json:"title" jsonschema:"New title"`
	Slug            string   `json:"slug" jsonschema:"New slug"`
	Content         string   `json:"content" jsonschema:"New Markdown content"`
	Summary         string   `json:"summary" jsonschema:"New summary"`
	Category        string   `json:"category" jsonschema:"New category"`
	Scope           kb.Scope `json:"scope" jsonschema:"New visibility scope"`
	Tags            []string `json:"tags" jsonschema:"New tags"`
	SourceMemoryIDs []string `json:"source_memory_ids" jsonschema:"Updated source memory IDs"`
}

type GetKBPageInput struct {
	PageID string `json:"page_id" jsonschema:"Page ID (use either page_id or slug)"`
	Slug   string `json:"slug" jsonschema:"Page slug (use either page_id or slug)"`
}

type SearchKBInput struct {
	UserID string `json:"user_id" jsonschema:"User ID"`
	Query  string `json:"query" jsonschema:"Search query"`
	Limit  int    `json:"limit" jsonschema:"Number of results (default: 10)"`
}

type ListKBPagesInput struct {
	UserID    string `json:"user_id" jsonschema:"User ID"`
	Category  string `json:"category" jsonschema:"Filter by category"`
	Limit     int    `json:"limit" jsonschema:"Number of results per page (default: 20)"`
	NextToken string `json:"next_token" jsonschema:"Pagination token"`
}

type DeleteKBPageInput struct {
	PageID string `json:"page_id" jsonschema:"Page ID to delete"`
}

// --- Tool input types: Curator ---

type RunCuratorInput struct {
	// No input parameters required for the no-op curator.
}

type GetCuratorStatusInput struct {
	// No input parameters required.
}

func (s *Server) registerTools() {
	// Memory tools
	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "add_memory",
		Description: "Add a new memory",
	}, s.handleAddMemory)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "search_memories",
		Description: "Search memories using semantic similarity",
	}, s.handleSearchMemories)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "list_memories",
		Description: "List memories for a user",
	}, s.handleListMemories)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "get_memory",
		Description: "Get a single memory by ID",
	}, s.handleGetMemory)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "update_memory",
		Description: "Update an existing memory",
	}, s.handleUpdateMemory)

	mcp.AddTool(s.mcpServer, &mcp.Tool{
		Name:        "delete_memory",
		Description: "Delete a memory",
	}, s.handleDeleteMemory)

	// Inbox tools
	if s.inboxSvc != nil {
		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "add_inbox",
			Description: "Add a raw entry to the Inbox (unorganized input: chat logs, URLs, notes, etc.)",
		}, s.handleAddInbox)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "list_inbox",
			Description: "List Inbox entries for a user, optionally filtered by status (pending/processed/archived)",
		}, s.handleListInbox)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "get_inbox",
			Description: "Get a single Inbox entry by ID",
		}, s.handleGetInbox)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "archive_inbox",
			Description: "Archive an Inbox entry (mark as archived)",
		}, s.handleArchiveInbox)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "delete_inbox",
			Description: "Delete an Inbox entry permanently",
		}, s.handleDeleteInbox)
	}

	// Knowledge Base tools
	if s.kbSvc != nil {
		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "create_kb_page",
			Description: "Create a new Knowledge Base page (structured wiki document in Markdown)",
		}, s.handleCreateKBPage)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "update_kb_page",
			Description: "Update an existing Knowledge Base page",
		}, s.handleUpdateKBPage)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "get_kb_page",
			Description: "Get a Knowledge Base page by ID or slug",
		}, s.handleGetKBPage)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "search_kb",
			Description: "Search Knowledge Base pages by keyword",
		}, s.handleSearchKB)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "list_kb_pages",
			Description: "List Knowledge Base pages for a user, optionally filtered by category",
		}, s.handleListKBPages)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "delete_kb_page",
			Description: "Delete a Knowledge Base page",
		}, s.handleDeleteKBPage)
	}

	// Curator tools
	if s.curator != nil {
		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "run_curator",
			Description: "Run the Curator: processes pending Inbox entries and organizes them into Memories and KB pages. Currently a no-op implementation that marks entries as processed.",
		}, s.handleRunCurator)

		mcp.AddTool(s.mcpServer, &mcp.Tool{
			Name:        "get_curator_status",
			Description: "Get the result of the most recent Curator run",
		}, s.handleGetCuratorStatus)
	}
}

// RegisterRoutes registers the MCP-specific routes (/sse and /mcp) on the given mux.
//
// When the CURATOR_TOKEN environment variable is set, both endpoints are
// protected by Bearer token authentication. Requests must supply either the
// CURATOR_TOKEN or the ADMIN_TOKEN in an "Authorization: Bearer <token>"
// header. This allows the curator agent subprocess to authenticate while
// keeping unauthenticated access blocked.
//
// When CURATOR_TOKEN is not set the endpoints remain open (previous behaviour).
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	getServer := func(req *http.Request) *mcp.Server {
		return s.mcpServer
	}

	sseHandler := mcp.NewSSEHandler(getServer, nil)
	streamableHandler := mcp.NewStreamableHTTPHandler(getServer, nil)

	// Optionally wrap with Bearer auth when CURATOR_TOKEN is configured.
	var handler http.Handler = http.NewServeMux()
	inner := handler.(*http.ServeMux)
	inner.Handle("/sse", sseHandler)
	inner.Handle("/mcp", streamableHandler)

	wrapped := mcpAuthMiddleware(inner)
	mux.Handle("/sse", wrapped)
	mux.Handle("/mcp", wrapped)
}

// mcpAuthMiddleware returns a middleware that enforces Bearer token auth on
// MCP endpoints when CURATOR_TOKEN is set, and passes through otherwise.
func mcpAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		curatorToken := os.Getenv("CURATOR_TOKEN")
		adminToken := os.Getenv("ADMIN_TOKEN")

		// If neither token is configured, allow all requests (open mode).
		if curatorToken == "" && adminToken == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Extract Bearer token.
		token := ""
		if h := r.Header.Get("Authorization"); h != "" {
			if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
				token = strings.TrimSpace(h[7:])
			}
		}

		if token == "" {
			http.Error(w, `{"error":"missing Authorization Bearer token"}`, http.StatusUnauthorized)
			return
		}

		if (curatorToken != "" && token == curatorToken) ||
			(adminToken != "" && token == adminToken) {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
	})
}

// Start starts the MCP server over SSE and Streamable HTTP.
func (s *Server) Start(port string) error {
	if port == "" {
		port = "8080"
	}
	addr := ":" + port

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"ok"}`)
	})
	s.RegisterRoutes(mux)

	return http.ListenAndServe(addr, mux)
}

// ============================================================
// Memory handlers
// ============================================================

func (s *Server) handleAddMemory(ctx context.Context, req *mcp.CallToolRequest, input AddMemoryInput) (*mcp.CallToolResult, any, error) {
	result, err := s.memorySvc.Add(ctx, memory.AddInput{
		UserID:  input.UserID,
		Content: input.Content,
		Tags:    input.Tags,
		Scope:   input.Scope,
	})
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	return jsonResult(map[string]string{
		"memory_id": result.MemoryID,
		"status":    "ok",
	})
}

func (s *Server) handleSearchMemories(ctx context.Context, req *mcp.CallToolRequest, input SearchMemoriesInput) (*mcp.CallToolResult, any, error) {
	results, err := s.memorySvc.Search(ctx, memory.SearchInput{
		UserID: input.UserID,
		Query:  input.Query,
		Tags:   input.Tags,
		Limit:  input.Limit,
	})
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	if len(results) == 0 {
		return jsonResult([]interface{}{})
	}

	out := make([]map[string]interface{}, 0, len(results))
	for _, r := range results {
		out = append(out, map[string]interface{}{
			"memory":           r.Memory,
			"similarity_score": r.SimilarityScore,
			"final_score":      r.FinalScore,
		})
	}

	return jsonResult(out)
}

func (s *Server) handleListMemories(ctx context.Context, req *mcp.CallToolRequest, input ListMemoriesInput) (*mcp.CallToolResult, any, error) {
	var nextToken *string
	if input.NextToken != "" {
		nextToken = &input.NextToken
	}

	result, err := s.memorySvc.List(ctx, memory.ListInput{
		UserID:    input.UserID,
		Limit:     input.Limit,
		NextToken: nextToken,
	})
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	resp := map[string]interface{}{
		"memories": result.Memories,
	}
	if result.NextToken != nil {
		resp["next_token"] = *result.NextToken
	} else {
		resp["next_token"] = ""
	}

	return jsonResult(resp)
}

func (s *Server) handleGetMemory(ctx context.Context, req *mcp.CallToolRequest, input GetMemoryInput) (*mcp.CallToolResult, any, error) {
	m, err := s.memorySvc.Get(ctx, input.MemoryID)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	return jsonResult(m)
}

func (s *Server) handleUpdateMemory(ctx context.Context, req *mcp.CallToolRequest, input UpdateMemoryInput) (*mcp.CallToolResult, any, error) {
	m, err := s.memorySvc.Update(ctx, memory.UpdateInput{
		MemoryID: input.MemoryID,
		Content:  input.Content,
		Tags:     input.Tags,
		Scope:    input.Scope,
	})
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	return jsonResult(m)
}

func (s *Server) handleDeleteMemory(ctx context.Context, req *mcp.CallToolRequest, input DeleteMemoryInput) (*mcp.CallToolResult, any, error) {
	if err := s.memorySvc.Delete(ctx, input.MemoryID); err != nil {
		return errorResult(err.Error()), nil, nil
	}

	return jsonResult(map[string]string{"status": "ok"})
}

// ============================================================
// Inbox handlers
// ============================================================

func (s *Server) handleAddInbox(ctx context.Context, req *mcp.CallToolRequest, input AddInboxInput) (*mcp.CallToolResult, any, error) {
	e, err := s.inboxSvc.Add(ctx, inbox.AddInput{
		UserID:  input.UserID,
		Content: input.Content,
		Source:  input.Source,
		Tags:    input.Tags,
	})
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return jsonResult(e)
}

func (s *Server) handleListInbox(ctx context.Context, req *mcp.CallToolRequest, input ListInboxInput) (*mcp.CallToolResult, any, error) {
	var nextToken *string
	if input.NextToken != "" {
		nextToken = &input.NextToken
	}

	result, err := s.inboxSvc.List(ctx, inbox.ListInput{
		UserID:    input.UserID,
		Status:    inbox.Status(input.Status),
		Limit:     input.Limit,
		NextToken: nextToken,
	})
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	resp := map[string]interface{}{
		"entries": result.Entries,
	}
	if result.NextToken != nil {
		resp["next_token"] = *result.NextToken
	} else {
		resp["next_token"] = ""
	}
	return jsonResult(resp)
}

func (s *Server) handleGetInbox(ctx context.Context, req *mcp.CallToolRequest, input GetInboxInput) (*mcp.CallToolResult, any, error) {
	e, err := s.inboxSvc.Get(ctx, input.InboxID)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return jsonResult(e)
}

func (s *Server) handleArchiveInbox(ctx context.Context, req *mcp.CallToolRequest, input ArchiveInboxInput) (*mcp.CallToolResult, any, error) {
	if err := s.inboxSvc.Archive(ctx, input.InboxID); err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return jsonResult(map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteInbox(ctx context.Context, req *mcp.CallToolRequest, input DeleteInboxInput) (*mcp.CallToolResult, any, error) {
	if err := s.inboxSvc.Delete(ctx, input.InboxID); err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return jsonResult(map[string]string{"status": "ok"})
}

// ============================================================
// Knowledge Base handlers
// ============================================================

func (s *Server) handleCreateKBPage(ctx context.Context, req *mcp.CallToolRequest, input CreateKBPageInput) (*mcp.CallToolResult, any, error) {
	p, err := s.kbSvc.Create(ctx, kb.CreateInput{
		UserID:          input.UserID,
		Title:           input.Title,
		Slug:            input.Slug,
		Content:         input.Content,
		Summary:         input.Summary,
		Category:        input.Category,
		Scope:           input.Scope,
		Tags:            input.Tags,
		SourceMemoryIDs: input.SourceMemoryIDs,
	})
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return jsonResult(p)
}

func (s *Server) handleUpdateKBPage(ctx context.Context, req *mcp.CallToolRequest, input UpdateKBPageInput) (*mcp.CallToolResult, any, error) {
	p, err := s.kbSvc.Update(ctx, kb.UpdateInput{
		PageID:          input.PageID,
		Title:           input.Title,
		Slug:            input.Slug,
		Content:         input.Content,
		Summary:         input.Summary,
		Category:        input.Category,
		Scope:           input.Scope,
		Tags:            input.Tags,
		SourceMemoryIDs: input.SourceMemoryIDs,
	})
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return jsonResult(p)
}

func (s *Server) handleGetKBPage(ctx context.Context, req *mcp.CallToolRequest, input GetKBPageInput) (*mcp.CallToolResult, any, error) {
	if input.PageID == "" && input.Slug == "" {
		return errorResult("either page_id or slug is required"), nil, nil
	}

	var p *kb.Page
	var err error
	if input.PageID != "" {
		p, err = s.kbSvc.Get(ctx, input.PageID)
	} else {
		p, err = s.kbSvc.GetBySlug(ctx, input.Slug)
	}
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return jsonResult(p)
}

func (s *Server) handleSearchKB(ctx context.Context, req *mcp.CallToolRequest, input SearchKBInput) (*mcp.CallToolResult, any, error) {
	results, err := s.kbSvc.Search(ctx, input.UserID, input.Query, input.Limit)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	if len(results) == 0 {
		return jsonResult([]interface{}{})
	}
	return jsonResult(results)
}

func (s *Server) handleListKBPages(ctx context.Context, req *mcp.CallToolRequest, input ListKBPagesInput) (*mcp.CallToolResult, any, error) {
	var nextToken *string
	if input.NextToken != "" {
		nextToken = &input.NextToken
	}

	result, err := s.kbSvc.List(ctx, kb.ListInput{
		UserID:    input.UserID,
		Category:  input.Category,
		Limit:     input.Limit,
		NextToken: nextToken,
	})
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	resp := map[string]interface{}{
		"pages": result.Pages,
	}
	if result.NextToken != nil {
		resp["next_token"] = *result.NextToken
	} else {
		resp["next_token"] = ""
	}
	return jsonResult(resp)
}

func (s *Server) handleDeleteKBPage(ctx context.Context, req *mcp.CallToolRequest, input DeleteKBPageInput) (*mcp.CallToolResult, any, error) {
	if err := s.kbSvc.Delete(ctx, input.PageID); err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return jsonResult(map[string]string{"status": "ok"})
}

// ============================================================
// Curator handlers
// ============================================================

func (s *Server) handleRunCurator(ctx context.Context, req *mcp.CallToolRequest, input RunCuratorInput) (*mcp.CallToolResult, any, error) {
	// MCP has no HTTP headers, so ANTHROPIC_API_KEY must come from the environment.
	result, err := s.curator.Run(ctx, curator.RunInput{})
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}
	return jsonResult(result)
}

func (s *Server) handleGetCuratorStatus(ctx context.Context, req *mcp.CallToolRequest, input GetCuratorStatusInput) (*mcp.CallToolResult, any, error) {
	result := s.curator.GetLastResult()
	if result == nil {
		return jsonResult(map[string]string{
			"status":  "never_run",
			"message": "Curator has not been run yet",
		})
	}
	return jsonResult(result)
}

// ============================================================
// Helpers
// ============================================================

func jsonResult(v interface{}) (*mcp.CallToolResult, any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return errorResult(fmt.Sprintf("marshal result: %v", err)), nil, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(b)},
		},
	}, nil, nil
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
	}
}
