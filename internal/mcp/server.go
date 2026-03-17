package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/takutakahashi/memory-server/internal/memory"

	"github.com/aws/aws-sdk-go-v2/config"
)

// Server holds all components for the Memory MCP server.
type Server struct {
	store     *memory.Store
	vectors   *memory.S3VectorsClient
	embedding *memory.EmbeddingClient
	scorer    *memory.Scorer
	mcpServer *mcp.Server
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
		mcpServer: mcp.NewServer(&mcp.Implementation{
			Name:    serverName,
			Version: serverVersion,
		}, nil),
	}

	s.registerTools()
	return s, nil
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

// Start starts the MCP server over SSE/HTTP.
func (s *Server) Start(port string) error {
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	handler := mcp.NewSSEHandler(func(req *http.Request) *mcp.Server {
		return s.mcpServer
	}, nil)
	return http.ListenAndServe(addr, handler)
}

// --- Tool handlers ---

func (s *Server) handleAddMemory(ctx context.Context, req *mcp.CallToolRequest, input AddMemoryInput) (*mcp.CallToolResult, any, error) {
	userID := input.UserID
	if userID == "" {
		userID = "default"
	}
	content := input.Content
	if content == "" {
		return errorResult("content is required"), nil, nil
	}
	tags := input.Tags

	// Generate embedding
	embedding, err := s.embedding.Generate(ctx, content)
	if err != nil {
		return errorResult(fmt.Sprintf("generate embedding: %v", err)), nil, nil
	}

	memoryID := uuid.New().String()
	now := time.Now().UTC()

	// Put vector
	if err := s.vectors.PutVectors(ctx, memoryID, embedding, userID); err != nil {
		return errorResult(fmt.Sprintf("put vectors: %v", err)), nil, nil
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
		return errorResult(fmt.Sprintf("store memory: %v", err)), nil, nil
	}

	return jsonResult(map[string]string{
		"memory_id": memoryID,
		"status":    "ok",
	})
}

func (s *Server) handleSearchMemories(ctx context.Context, req *mcp.CallToolRequest, input SearchMemoriesInput) (*mcp.CallToolResult, any, error) {
	userID := input.UserID
	if userID == "" {
		userID = "default"
	}
	query := input.Query
	if query == "" {
		return errorResult("query is required"), nil, nil
	}
	tags := input.Tags
	if len(tags) > 5 {
		tags = tags[:5]
	}
	limit := input.Limit
	if limit == 0 {
		limit = 10
	}

	// Generate embedding for query
	embedding, err := s.embedding.Generate(ctx, query)
	if err != nil {
		return errorResult(fmt.Sprintf("generate query embedding: %v", err)), nil, nil
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
			return errorResult(fmt.Sprintf("query vectors: %v", err)), nil, nil
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
		return errorResult(fmt.Sprintf("get memories: %v", err)), nil, nil
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

func (s *Server) handleListMemories(ctx context.Context, req *mcp.CallToolRequest, input ListMemoriesInput) (*mcp.CallToolResult, any, error) {
	userID := input.UserID
	if userID == "" {
		userID = "default"
	}
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}
	nextTokenStr := input.NextToken
	var nextToken *string
	if nextTokenStr != "" {
		nextToken = &nextTokenStr
	}

	memories, newNextToken, err := s.store.ListByUserID(ctx, userID, limit, nextToken)
	if err != nil {
		return errorResult(fmt.Sprintf("list memories: %v", err)), nil, nil
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

func (s *Server) handleGetMemory(ctx context.Context, req *mcp.CallToolRequest, input GetMemoryInput) (*mcp.CallToolResult, any, error) {
	memoryID := input.MemoryID
	if memoryID == "" {
		return errorResult("memory_id is required"), nil, nil
	}

	m, err := s.store.Get(ctx, memoryID)
	if err != nil {
		return errorResult(fmt.Sprintf("get memory: %v", err)), nil, nil
	}

	// Update access count asynchronously
	go func() {
		_ = s.store.UpdateAccess(context.Background(), memoryID)
	}()

	return jsonResult(m)
}

func (s *Server) handleUpdateMemory(ctx context.Context, req *mcp.CallToolRequest, input UpdateMemoryInput) (*mcp.CallToolResult, any, error) {
	memoryID := input.MemoryID
	if memoryID == "" {
		return errorResult("memory_id is required"), nil, nil
	}

	newContent := input.Content
	newTags := input.Tags

	m, err := s.store.Get(ctx, memoryID)
	if err != nil {
		return errorResult(fmt.Sprintf("get memory: %v", err)), nil, nil
	}

	contentChanged := newContent != "" && newContent != m.Content
	if contentChanged {
		// Re-embed and update vectors
		embedding, embErr := s.embedding.Generate(ctx, newContent)
		if embErr != nil {
			return errorResult(fmt.Sprintf("generate embedding: %v", embErr)), nil, nil
		}
		// Delete old vector (best effort)
		_ = s.vectors.DeleteVectors(ctx, []string{m.VectorID})
		// Put new vector
		if putErr := s.vectors.PutVectors(ctx, memoryID, embedding, m.UserID); putErr != nil {
			return errorResult(fmt.Sprintf("put vectors: %v", putErr)), nil, nil
		}
		m.Content = newContent
		m.VectorID = memoryID
	}

	if newTags != nil {
		m.Tags = newTags
	}
	m.UpdatedAt = time.Now().UTC()

	if err := s.store.Update(ctx, m); err != nil {
		return errorResult(fmt.Sprintf("update memory: %v", err)), nil, nil
	}

	return jsonResult(m)
}

func (s *Server) handleDeleteMemory(ctx context.Context, req *mcp.CallToolRequest, input DeleteMemoryInput) (*mcp.CallToolResult, any, error) {
	memoryID := input.MemoryID
	if memoryID == "" {
		return errorResult("memory_id is required"), nil, nil
	}

	// Delete from S3 Vectors (best effort - DynamoDB is source of truth)
	_ = s.vectors.DeleteVectors(ctx, []string{memoryID})

	// Delete from DynamoDB
	if err := s.store.Delete(ctx, memoryID); err != nil {
		return errorResult(fmt.Sprintf("delete memory: %v", err)), nil, nil
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
