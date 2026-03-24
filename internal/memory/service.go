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
	OrgID   string // required when Scope == ScopeOrg
	Content string
	Tags    []string
	Scope   Scope
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
	scope := input.Scope
	if scope == "" {
		scope = ScopePrivate
	}
	if scope != ScopePrivate && scope != ScopePublic && scope != ScopeOrg {
		return nil, fmt.Errorf("invalid scope: %q (must be %q, %q, or %q)", scope, ScopePrivate, ScopePublic, ScopeOrg)
	}
	if scope == ScopeOrg && input.OrgID == "" {
		return nil, fmt.Errorf("org_id is required when scope is %q", ScopeOrg)
	}

	embedding, err := s.Embedding.Generate(ctx, input.Content)
	if err != nil {
		return nil, fmt.Errorf("generate embedding: %w", err)
	}

	memoryID := uuid.New().String()
	now := time.Now().UTC()

	if err := s.Vectors.PutVectors(ctx, memoryID, embedding, userID, scope, input.OrgID); err != nil {
		return nil, fmt.Errorf("put vectors: %w", err)
	}

	m := &Memory{
		MemoryID:       memoryID,
		UserID:         userID,
		OrgID:          input.OrgID,
		Scope:          scope,
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
	OrgID  string // when set, org-scoped memories for this org are included in results
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
		return nil, fmt.Errorf("too many tags: max 5, got %d", len(tags))
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

	// mergeVectorResults deduplicates results keeping the highest score.
	mergeVectorResults := func(a, b []*VectorResult) []*VectorResult {
		scoreMap := make(map[string]*VectorResult, len(a)+len(b))
		for _, r := range a {
			scoreMap[r.Key] = r
		}
		for _, r := range b {
			if existing, ok := scoreMap[r.Key]; !ok || r.Score > existing.Score {
				scoreMap[r.Key] = r
			}
		}
		merged := make([]*VectorResult, 0, len(scoreMap))
		for _, v := range scoreMap {
			merged = append(merged, v)
		}
		return merged
	}

	if len(tags) > 0 {
		type tagResult struct {
			results []*VectorResult
			err     error
		}
		resultCh := make(chan tagResult, len(tags)+1)
		var wg sync.WaitGroup
		for _, tag := range tags {
			wg.Add(1)
			go func(t string) {
				defer wg.Done()
				results, qErr := s.Vectors.QueryVectorsWithTag(ctx, embedding, 20, userID, t)
				resultCh <- tagResult{results: results, err: qErr}
			}(tag)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, qErr := s.Vectors.QueryVectorsPublic(ctx, embedding, 50)
			resultCh <- tagResult{results: results, err: qErr}
		}()
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
		// Query user's own memories (any scope).
		ownResults, qErr := s.Vectors.QueryVectors(ctx, embedding, 50, userID)
		if qErr != nil {
			return nil, fmt.Errorf("query vectors: %w", qErr)
		}

		// Also query public memories from all users.
		publicResults, qErr := s.Vectors.QueryVectorsPublic(ctx, embedding, 50)
		if qErr != nil {
			// Non-fatal: public search may fail (e.g. empty index); continue with own results.
			publicResults = nil
		}

		vectorResults = mergeVectorResults(ownResults, publicResults)

		// Also query org-scoped memories when an org_id is provided.
		if input.OrgID != "" {
			orgResults, qErr := s.Vectors.QueryVectorsOrg(ctx, embedding, 50, input.OrgID)
			if qErr == nil {
				vectorResults = mergeVectorResults(vectorResults, orgResults)
			}
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
		// Include if: own memory (any scope) OR public memory OR org-scoped memory matching requested org.
		if m.UserID != userID && m.Scope != ScopePublic {
			if m.Scope != ScopeOrg || m.OrgID != input.OrgID {
				continue
			}
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
	OrgID     string // when set, list org-scoped memories instead of user memories
	Limit     int
	NextToken *string
}

// ListResult holds the result of List.
type ListResult struct {
	Memories  []*Memory `json:"memories"`
	NextToken *string   `json:"next_token,omitempty"`
}

// List returns memories for a user or org with optional pagination.
func (s *Service) List(ctx context.Context, input ListInput) (*ListResult, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}

	var memories []*Memory
	var newNextToken *string
	var err error

	if input.OrgID != "" {
		memories, newNextToken, err = s.Store.ListByOrgID(ctx, input.OrgID, limit, input.NextToken)
		if err != nil {
			return nil, fmt.Errorf("list org memories: %w", err)
		}
	} else {
		userID := input.UserID
		if userID == "" {
			userID = "default"
		}
		memories, newNextToken, err = s.Store.ListByUserID(ctx, userID, limit, input.NextToken)
		if err != nil {
			return nil, fmt.Errorf("list memories: %w", err)
		}
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
	OrgID    string // required when changing scope to ScopeOrg; cleared when scope changes away from ScopeOrg
	Content  string
	Tags     []string
	Scope    Scope
}

// Update modifies an existing memory's content, tags, and/or scope.
func (s *Service) Update(ctx context.Context, input UpdateInput) (*Memory, error) {
	if input.MemoryID == "" {
		return nil, fmt.Errorf("memory_id is required")
	}

	if input.Scope != "" && input.Scope != ScopePrivate && input.Scope != ScopePublic && input.Scope != ScopeOrg {
		return nil, fmt.Errorf("invalid scope: %q (must be %q, %q, or %q)", input.Scope, ScopePrivate, ScopePublic, ScopeOrg)
	}

	m, err := s.Store.Get(ctx, input.MemoryID)
	if err != nil {
		return nil, fmt.Errorf("get memory: %w", err)
	}

	// Resolve effective scope.
	newScope := m.Scope
	if input.Scope != "" {
		newScope = input.Scope
	}
	if newScope == "" {
		newScope = ScopePrivate
	}

	// Resolve effective org_id.
	newOrgID := m.OrgID
	if input.OrgID != "" {
		newOrgID = input.OrgID
	}
	// Clear org_id when scope is not org.
	if newScope != ScopeOrg {
		newOrgID = ""
	}
	if newScope == ScopeOrg && newOrgID == "" {
		return nil, fmt.Errorf("org_id is required when scope is %q", ScopeOrg)
	}

	contentChanged := input.Content != "" && input.Content != m.Content
	scopeChanged := newScope != m.Scope
	orgChanged := newOrgID != m.OrgID

	if contentChanged || scopeChanged || orgChanged {
		// Re-embed if content changed; reuse existing embedding if only scope/org changed.
		var embedding []float64
		if contentChanged {
			var embErr error
			embedding, embErr = s.Embedding.Generate(ctx, input.Content)
			if embErr != nil {
				return nil, fmt.Errorf("generate embedding: %w", embErr)
			}
			m.Content = input.Content
		} else {
			// Re-generate embedding from existing content to re-store with updated metadata.
			var embErr error
			embedding, embErr = s.Embedding.Generate(ctx, m.Content)
			if embErr != nil {
				return nil, fmt.Errorf("generate embedding: %w", embErr)
			}
		}
		_ = s.Vectors.DeleteVectors(ctx, []string{m.VectorID})
		if putErr := s.Vectors.PutVectors(ctx, input.MemoryID, embedding, m.UserID, newScope, newOrgID); putErr != nil {
			return nil, fmt.Errorf("put vectors: %w", putErr)
		}
		m.VectorID = input.MemoryID
	}

	if input.Tags != nil {
		m.Tags = input.Tags
	}
	m.Scope = newScope
	m.OrgID = newOrgID
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
