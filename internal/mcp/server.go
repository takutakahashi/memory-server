package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/takutakahashi/memory-server/internal/memory"

	"github.com/aws/aws-sdk-go-v2/config"
)

// Server holds all components for the Memory MCP server.
type Server struct {
	store     *memory.Store
	vectors   *memory.S3VectorsClient
	embedding *memory.EmbeddingClient
	scorer    *memory.Scorer
	mcpServer *server.MCPServer
}

// NewServer creates a new Server by loading AWS config and initializing all components.
func NewServer(ctx context.Context) (*Server, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	serverName := os.Getenv("MCP_SERVER_NAME")
	if serverName == "" {
		serverName = "memory-server"
	}
	serverVersion := os.Getenv("MCP_SERVER_VERSION")
	if serverVersion == "" {
		serverVersion = "1.0.0"
	}

	s := &Server{
		store:     memory.NewStore(cfg),
		vectors:   memory.NewS3VectorsClient(cfg),
		embedding: memory.NewEmbeddingClient(cfg),
		scorer:    memory.NewScorer(),
		mcpServer: server.NewMCPServer(serverName, serverVersion),
	}

	s.registerTools()
	return s, nil
}

func (s *Server) registerTools() {
	// add_memory
	s.mcpServer.AddTool(mcp.NewTool("add_memory",
		mcp.WithDescription("Add a new memory"),
		mcp.WithString("user_id", mcp.Description("User ID (default: 'default')")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Content of the memory")),
		mcp.WithArray("tags", mcp.Description("Tags for the memory")),
	), s.handleAddMemory)

	// search_memories
	s.mcpServer.AddTool(mcp.NewTool("search_memories",
		mcp.WithDescription("Search memories using semantic similarity"),
		mcp.WithString("user_id", mcp.Description("User ID (default: 'default')")),
		mcp.WithString("query", mcp.Required(), mcp.Description("Natural language search query")),
		mcp.WithArray("tags", mcp.Description("Tags for OR-filtered search (max 5)")),
		mcp.WithNumber("limit", mcp.Description("Number of results to return (default: 10)")),
	), s.handleSearchMemories)

	// list_memories
	s.mcpServer.AddTool(mcp.NewTool("list_memories",
		mcp.WithDescription("List memories for a user"),
		mcp.WithString("user_id", mcp.Description("User ID (default: 'default')")),
		mcp.WithNumber("limit", mcp.Description("Number of results per page (default: 20)")),
		mcp.WithString("next_token", mcp.Description("Pagination token")),
	), s.handleListMemories)

	// get_memory
	s.mcpServer.AddTool(mcp.NewTool("get_memory",
		mcp.WithDescription("Get a single memory by ID"),
		mcp.WithString("memory_id", mcp.Required(), mcp.Description("Memory ID")),
	), s.handleGetMemory)

	// update_memory
	s.mcpServer.AddTool(mcp.NewTool("update_memory",
		mcp.WithDescription("Update an existing memory"),
		mcp.WithString("memory_id", mcp.Required(), mcp.Description("Memory ID")),
		mcp.WithString("content", mcp.Description("New content")),
		mcp.WithArray("tags", mcp.Description("New tags")),
	), s.handleUpdateMemory)

	// delete_memory
	s.mcpServer.AddTool(mcp.NewTool("delete_memory",
		mcp.WithDescription("Delete a memory"),
		mcp.WithString("memory_id", mcp.Required(), mcp.Description("Memory ID")),
	), s.handleDeleteMemory)
}

// Start starts the MCP server over SSE/HTTP.
func (s *Server) Start(port string) error {
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	sseServer := server.NewSSEServer(s.mcpServer, server.WithBaseURL(fmt.Sprintf("http://localhost%s", addr)))
	return sseServer.Start(addr)
}

// --- Tool handlers ---

func (s *Server) handleAddMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := req.GetString("user_id", "default")
	content := req.GetString("content", "")
	if content == "" {
		return errorResult("content is required"), nil
	}
	tags := req.GetStringSlice("tags", nil)

	// Generate embedding
	embedding, err := s.embedding.Generate(ctx, content)
	if err != nil {
		return errorResult(fmt.Sprintf("generate embedding: %v", err)), nil
	}

	memoryID := uuid.New().String()
	now := time.Now().UTC()

	// Put vector
	if err := s.vectors.PutVectors(ctx, memoryID, embedding, userID); err != nil {
		return errorResult(fmt.Sprintf("put vectors: %v", err)), nil
	}

	// Save to DynamoDB
	m := &memory.Memory{
		MemoryID:       memoryID,
		UserID:         userID,
		Content:        content,
		Tags:           tags,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastAccessedAt: now,
		AccessCount:    0,
		VectorID:       memoryID,
	}
	if err := s.store.Put(ctx, m); err != nil {
		return errorResult(fmt.Sprintf("store memory: %v", err)), nil
	}

	return jsonResult(map[string]string{
		"memory_id": memoryID,
		"status":    "ok",
	})
}

func (s *Server) handleSearchMemories(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := req.GetString("user_id", "default")
	query := req.GetString("query", "")
	if query == "" {
		return errorResult("query is required"), nil
	}
	tags := req.GetStringSlice("tags", nil)
	if len(tags) > 5 {
		tags = tags[:5]
	}
	limit := req.GetInt("limit", 10)

	// Generate embedding for query
	embedding, err := s.embedding.Generate(ctx, query)
	if err != nil {
		return errorResult(fmt.Sprintf("generate query embedding: %v", err)), nil
	}

	var vectorResults []*memory.VectorResult

	if len(tags) > 0 {
		// Parallel queries per tag
		type tagResult struct {
			results []*memory.VectorResult
			err     error
		}
		resultCh := make(chan tagResult, len(tags))
		var wg sync.WaitGroup
		for _, tag := range tags {
			wg.Add(1)
			go func(t string) {
				defer wg.Done()
				results, qErr := s.vectors.QueryVectorsWithTag(ctx, embedding, 20, userID, t)
				resultCh <- tagResult{results: results, err: qErr}
			}(tag)
		}
		wg.Wait()
		close(resultCh)

		// Merge results (max score per key)
		scoreMap := make(map[string]*memory.VectorResult)
		for tr := range resultCh {
			if tr.err != nil {
				continue // best effort
			}
			for _, r := range tr.results {
				if existing, ok := scoreMap[r.Key]; !ok || r.Score > existing.Score {
					scoreMap[r.Key] = r
				}
			}
		}
		for _, v := range scoreMap {
			vectorResults = append(vectorResults, v)
		}
	} else {
		vectorResults, err = s.vectors.QueryVectors(ctx, embedding, 50, userID)
		if err != nil {
			return errorResult(fmt.Sprintf("query vectors: %v", err)), nil
		}
	}

	if len(vectorResults) == 0 {
		return jsonResult([]interface{}{})
	}

	// Get memory IDs and build score map
	vectorScoreMap := make(map[string]float64, len(vectorResults))
	memoryIDs := make([]string, 0, len(vectorResults))
	for _, vr := range vectorResults {
		vectorScoreMap[vr.Key] = vr.Score
		memoryIDs = append(memoryIDs, vr.Key)
	}

	// Fetch metadata from DynamoDB
	memories, err := s.store.GetByIDs(ctx, memoryIDs)
	if err != nil {
		return errorResult(fmt.Sprintf("get memories: %v", err)), nil
	}

	// Filter by user_id and score
	type scoredMemory struct {
		m     *memory.Memory
		score float64
	}
	var scored []scoredMemory
	for _, m := range memories {
		if m.UserID != userID {
			continue
		}
		simScore := vectorScoreMap[m.MemoryID]
		finalScore := s.scorer.Score(simScore, m)
		scored = append(scored, scoredMemory{m: m, score: finalScore})
	}

	// Sort by final score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	// Limit results
	if len(scored) > limit {
		scored = scored[:limit]
	}

	// Async update access metadata
	go func() {
		bgCtx := context.Background()
		for _, sm := range scored {
			_ = s.store.UpdateAccess(bgCtx, sm.m.MemoryID)
		}
	}()

	// Build response
	results := make([]map[string]interface{}, 0, len(scored))
	for _, sm := range scored {
		results = append(results, map[string]interface{}{
			"memory":           sm.m,
			"similarity_score": vectorScoreMap[sm.m.MemoryID],
			"final_score":      sm.score,
		})
	}

	return jsonResult(results)
}

func (s *Server) handleListMemories(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := req.GetString("user_id", "default")
	limit := req.GetInt("limit", 20)
	nextTokenStr := req.GetString("next_token", "")
	var nextToken *string
	if nextTokenStr != "" {
		nextToken = &nextTokenStr
	}

	memories, newNextToken, err := s.store.ListByUserID(ctx, userID, limit, nextToken)
	if err != nil {
		return errorResult(fmt.Sprintf("list memories: %v", err)), nil
	}

	resp := map[string]interface{}{
		"memories": memories,
	}
	if newNextToken != nil {
		resp["next_token"] = *newNextToken
	} else {
		resp["next_token"] = ""
	}

	return jsonResult(resp)
}

func (s *Server) handleGetMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	memoryID := req.GetString("memory_id", "")
	if memoryID == "" {
		return errorResult("memory_id is required"), nil
	}

	m, err := s.store.Get(ctx, memoryID)
	if err != nil {
		return errorResult(fmt.Sprintf("get memory: %v", err)), nil
	}

	// Update access count asynchronously
	go func() {
		_ = s.store.UpdateAccess(context.Background(), memoryID)
	}()

	return jsonResult(m)
}

func (s *Server) handleUpdateMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	memoryID := req.GetString("memory_id", "")
	if memoryID == "" {
		return errorResult("memory_id is required"), nil
	}

	newContent := req.GetString("content", "")
	newTags := req.GetStringSlice("tags", nil)

	m, err := s.store.Get(ctx, memoryID)
	if err != nil {
		return errorResult(fmt.Sprintf("get memory: %v", err)), nil
	}

	contentChanged := newContent != "" && newContent != m.Content
	if contentChanged {
		// Re-embed and update vectors
		embedding, embErr := s.embedding.Generate(ctx, newContent)
		if embErr != nil {
			return errorResult(fmt.Sprintf("generate embedding: %v", embErr)), nil
		}
		// Delete old vector (best effort)
		_ = s.vectors.DeleteVectors(ctx, []string{m.VectorID})
		// Put new vector
		if putErr := s.vectors.PutVectors(ctx, memoryID, embedding, m.UserID); putErr != nil {
			return errorResult(fmt.Sprintf("put vectors: %v", putErr)), nil
		}
		m.Content = newContent
		m.VectorID = memoryID
	}

	if newTags != nil {
		m.Tags = newTags
	}
	m.UpdatedAt = time.Now().UTC()

	if err := s.store.Update(ctx, m); err != nil {
		return errorResult(fmt.Sprintf("update memory: %v", err)), nil
	}

	return jsonResult(m)
}

func (s *Server) handleDeleteMemory(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	memoryID := req.GetString("memory_id", "")
	if memoryID == "" {
		return errorResult("memory_id is required"), nil
	}

	// Delete from S3 Vectors (best effort - DynamoDB is source of truth)
	_ = s.vectors.DeleteVectors(ctx, []string{memoryID})

	// Delete from DynamoDB
	if err := s.store.Delete(ctx, memoryID); err != nil {
		return errorResult(fmt.Sprintf("delete memory: %v", err)), nil
	}

	return jsonResult(map[string]string{"status": "ok"})
}

// --- Helpers ---

func jsonResult(v interface{}) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return errorResult(fmt.Sprintf("marshal result: %v", err)), nil
	}
	return mcp.NewToolResultText(string(b)), nil
}

func errorResult(msg string) *mcp.CallToolResult {
	return mcp.NewToolResultError(msg)
}
