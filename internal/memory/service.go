package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/google/uuid"
)

// Service provides high-level memory operations by wrapping the underlying components.
type Service struct {
	Store     *Store
	Vectors   *S3VectorsClient
	Embedding *EmbeddingClient
	Scorer    *Scorer
}

// NewService creates a new Service using the given AWS config.
func NewService(cfg aws.Config) *Service {
	return &Service{
		Store:     NewStore(cfg),
		Vectors:   NewS3VectorsClient(cfg),
		Embedding: NewEmbeddingClient(cfg),
		Scorer:    NewScorer(),
	}
}

// AddInput holds input parameters for Add.
type AddInput struct {
	UserID  string
	Content string
	Tags    []string
}

// AddResult holds the result of Add.
type AddResult struct {
	MemoryID string
}

// Add stores a new memory and its vector embedding.
func (s *Service) Add(ctx context.Context, input AddInput) (*AddResult, error) {
	userID := input.UserID
	if userID == "" {
		userID = "default"
	}
	if input.Content == "" {
		return nil, fmt.Errorf("content is required")
	}

	embedding, err := s.Embedding.Generate(ctx, input.Content)
	if err != nil {
		return nil, fmt.Errorf("generate embedding: %w", err)
	}

	memoryID := uuid.New().String()
	now := time.Now().UTC()

	if err := s.Vectors.PutVectors(ctx, memoryID, embedding, userID); err != nil {
		return nil, fmt.Errorf("put vectors: %w", err)
	}

	m := &Memory{
		MemoryID:       memoryID,
		UserID:         userID,
		Content:        input.Content,
		Tags:           input.Tags,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastAccessedAt: now,
		AccessCount:    0,
		VectorID:       memoryID,
	}
	if err := s.Store.Put(ctx, m); err != nil {
		return nil, fmt.Errorf("store memory: %w", err)
	}

	return &AddResult{MemoryID: memoryID}, nil
}

// SearchInput holds input parameters for Search.
type SearchInput struct {
	UserID string
	Query  string
	Tags   []string
	Limit  int
}

// Search performs a semantic similarity search over memories.
func (s *Service) Search(ctx context.Context, input SearchInput) ([]*SearchResult, error) {
	userID := input.UserID
	if userID == "" {
		userID = "default"
	}
	if input.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	tags := input.Tags
	if len(tags) > 5 {
		tags = tags[:5]
	}
	limit := input.Limit
	if limit == 0 {
		limit = 10
	}

	embedding, err := s.Embedding.Generate(ctx, input.Query)
	if err != nil {
		return nil, fmt.Errorf("generate query embedding: %w", err)
	}

	var vectorResults []*VectorResult

	if len(tags) > 0 {
		type tagResult struct {
			results []*VectorResult
			err     error
		}
		resultCh := make(chan tagResult, len(tags))
		var wg sync.WaitGroup
		for _, tag := range tags {
			wg.Add(1)
			go func(t string) {
				defer wg.Done()
				results, qErr := s.Vectors.QueryVectorsWithTag(ctx, embedding, 20, userID, t)
				resultCh <- tagResult{results: results, err: qErr}
			}(tag)
		}
		wg.Wait()
		close(resultCh)

		scoreMap := make(map[string]*VectorResult)
		for tr := range resultCh {
			if tr.err != nil {
				continue
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
		vectorResults, err = s.Vectors.QueryVectors(ctx, embedding, 50, userID)
		if err != nil {
			return nil, fmt.Errorf("query vectors: %w", err)
		}
	}

	if len(vectorResults) == 0 {
		return []*SearchResult{}, nil
	}

	vectorScoreMap := make(map[string]float64, len(vectorResults))
	memoryIDs := make([]string, 0, len(vectorResults))
	for _, vr := range vectorResults {
		vectorScoreMap[vr.Key] = vr.Score
		memoryIDs = append(memoryIDs, vr.Key)
	}

	memories, err := s.Store.GetByIDs(ctx, memoryIDs)
	if err != nil {
		return nil, fmt.Errorf("get memories: %w", err)
	}

	type scoredMemory struct {
		m     *Memory
		score float64
	}
	var scored []scoredMemory
	for _, m := range memories {
		if m.UserID != userID {
			continue
		}
		simScore := vectorScoreMap[m.MemoryID]
		finalScore := s.Scorer.Score(simScore, m)
		scored = append(scored, scoredMemory{m: m, score: finalScore})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	go func() {
		bgCtx := context.Background()
		for _, sm := range scored {
			_ = s.Store.UpdateAccess(bgCtx, sm.m.MemoryID)
		}
	}()

	results := make([]*SearchResult, 0, len(scored))
	for _, sm := range scored {
		results = append(results, &SearchResult{
			Memory:          sm.m,
			SimilarityScore: vectorScoreMap[sm.m.MemoryID],
			FinalScore:      sm.score,
		})
	}

	return results, nil
}

// ListInput holds input parameters for List.
type ListInput struct {
	UserID    string
	Limit     int
	NextToken *string
}

// ListResult holds the result of List.
type ListResult struct {
	Memories  []*Memory `json:"memories"`
	NextToken *string   `json:"next_token,omitempty"`
}

// List returns memories for a user with optional pagination.
func (s *Service) List(ctx context.Context, input ListInput) (*ListResult, error) {
	userID := input.UserID
	if userID == "" {
		userID = "default"
	}
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}

	memories, newNextToken, err := s.Store.ListByUserID(ctx, userID, limit, input.NextToken)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}

	return &ListResult{
		Memories:  memories,
		NextToken: newNextToken,
	}, nil
}

// Get retrieves a single memory by ID.
func (s *Service) Get(ctx context.Context, memoryID string) (*Memory, error) {
	if memoryID == "" {
		return nil, fmt.Errorf("memory_id is required")
	}

	m, err := s.Store.Get(ctx, memoryID)
	if err != nil {
		return nil, fmt.Errorf("get memory: %w", err)
	}

	go func() {
		_ = s.Store.UpdateAccess(context.Background(), memoryID)
	}()

	return m, nil
}

// UpdateInput holds input parameters for Update.
type UpdateInput struct {
	MemoryID string
	Content  string
	Tags     []string
}

// Update modifies an existing memory's content and/or tags.
func (s *Service) Update(ctx context.Context, input UpdateInput) (*Memory, error) {
	if input.MemoryID == "" {
		return nil, fmt.Errorf("memory_id is required")
	}

	m, err := s.Store.Get(ctx, input.MemoryID)
	if err != nil {
		return nil, fmt.Errorf("get memory: %w", err)
	}

	if input.Content != "" && input.Content != m.Content {
		embedding, embErr := s.Embedding.Generate(ctx, input.Content)
		if embErr != nil {
			return nil, fmt.Errorf("generate embedding: %w", embErr)
		}
		_ = s.Vectors.DeleteVectors(ctx, []string{m.VectorID})
		if putErr := s.Vectors.PutVectors(ctx, input.MemoryID, embedding, m.UserID); putErr != nil {
			return nil, fmt.Errorf("put vectors: %w", putErr)
		}
		m.Content = input.Content
		m.VectorID = input.MemoryID
	}

	if input.Tags != nil {
		m.Tags = input.Tags
	}
	m.UpdatedAt = time.Now().UTC()

	if err := s.Store.Update(ctx, m); err != nil {
		return nil, fmt.Errorf("update memory: %w", err)
	}

	return m, nil
}

// Delete removes a memory and its associated vector.
func (s *Service) Delete(ctx context.Context, memoryID string) error {
	if memoryID == "" {
		return fmt.Errorf("memory_id is required")
	}

	_ = s.Vectors.DeleteVectors(ctx, []string{memoryID})

	if err := s.Store.Delete(ctx, memoryID); err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}

	return nil
}
