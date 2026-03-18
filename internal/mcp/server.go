package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/takutakahashi/memory-server/internal/memory"

	"github.com/aws/aws-sdk-go-v2/config"
)

// Server holds all components for the Memory MCP server.
type Server struct {
	svc       *memory.Service
	mcpServer *mcp.Server
}

// NewServerWithService creates a new Server using an existing Service.
func NewServerWithService(svc *memory.Service) *Server {
	serverName := os.Getenv("MCP_SERVER_NAME")
	if serverName == "" {
		serverName = "memory-server"
	}
	serverVersion := os.Getenv("MCP_SERVER_VERSION")
	if serverVersion == "" {
		serverVersion = "1.0.0"
	}

	s := &Server{
		svc: svc,
		mcpServer: mcp.NewServer(&mcp.Implementation{
			Name:    serverName,
			Version: serverVersion,
		}, nil),
	}

	s.registerTools()
	return s
}

// NewServer creates a new Server by loading AWS config and initializing all components.
func NewServer(ctx context.Context) (*Server, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	svc := memory.NewService(cfg)
	return NewServerWithService(svc), nil
}

// --- Tool input types ---

type AddMemoryInput struct {
	UserID  string   `json:"user_id" jsonschema:"User ID (default: 'default')"`
	Content string   `json:"content" jsonschema:"Content of the memory"`
	Tags    []string `json:"tags" jsonschema:"Tags for the memory"`
}

type SearchMemoriesInput struct {
	UserID string   `json:"user_id" jsonschema:"User ID (default: 'default')"`
	Query  string   `json:"query" jsonschema:"Natural language search query"`
	Tags   []string `json:"tags" jsonschema:"Tags for OR-filtered search (max 5)"`
	Limit  int      `json:"limit" jsonschema:"Number of results to return (default: 10)"`
}

type ListMemoriesInput struct {
	UserID    string `json:"user_id" jsonschema:"User ID (default: 'default')"`
	Limit     int    `json:"limit" jsonschema:"Number of results per page (default: 20)"`
	NextToken string `json:"next_token" jsonschema:"Pagination token"`
}

type GetMemoryInput struct {
	MemoryID string `json:"memory_id" jsonschema:"Memory ID"`
}

type UpdateMemoryInput struct {
	MemoryID string   `json:"memory_id" jsonschema:"Memory ID"`
	Content  string   `json:"content" jsonschema:"New content"`
	Tags     []string `json:"tags" jsonschema:"New tags"`
}

type DeleteMemoryInput struct {
	MemoryID string `json:"memory_id" jsonschema:"Memory ID"`
}

func (s *Server) registerTools() {
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
}

// RegisterRoutes registers the MCP-specific routes (/sse and /mcp) on the given mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	getServer := func(req *http.Request) *mcp.Server {
		return s.mcpServer
	}

	sseHandler := mcp.NewSSEHandler(getServer, nil)
	streamableHandler := mcp.NewStreamableHTTPHandler(getServer, nil)

	mux.Handle("/sse", sseHandler)
	mux.Handle("/mcp", streamableHandler)
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

// --- Tool handlers ---

func (s *Server) handleAddMemory(ctx context.Context, req *mcp.CallToolRequest, input AddMemoryInput) (*mcp.CallToolResult, any, error) {
	result, err := s.svc.Add(ctx, memory.AddInput{
		UserID:  input.UserID,
		Content: input.Content,
		Tags:    input.Tags,
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
	results, err := s.svc.Search(ctx, memory.SearchInput{
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

	result, err := s.svc.List(ctx, memory.ListInput{
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
	m, err := s.svc.Get(ctx, input.MemoryID)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	return jsonResult(m)
}

func (s *Server) handleUpdateMemory(ctx context.Context, req *mcp.CallToolRequest, input UpdateMemoryInput) (*mcp.CallToolResult, any, error) {
	m, err := s.svc.Update(ctx, memory.UpdateInput{
		MemoryID: input.MemoryID,
		Content:  input.Content,
		Tags:     input.Tags,
	})
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	return jsonResult(m)
}

func (s *Server) handleDeleteMemory(ctx context.Context, req *mcp.CallToolRequest, input DeleteMemoryInput) (*mcp.CallToolResult, any, error) {
	if err := s.svc.Delete(ctx, input.MemoryID); err != nil {
		return errorResult(err.Error()), nil, nil
	}

	return jsonResult(map[string]string{"status": "ok"})
}

// --- Helpers ---

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
