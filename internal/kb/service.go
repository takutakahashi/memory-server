package kb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/google/uuid"
)

// Service provides high-level Knowledge Base operations.
type Service struct {
	Store *Store
}

// NewService creates a new Service using the given AWS config.
func NewService(cfg aws.Config) *Service {
	return &Service{
		Store: NewStore(cfg),
	}
}

// CreateInput holds input parameters for Create.
type CreateInput struct {
	UserID          string
	Title           string
	Slug            string
	Content         string
	Summary         string
	Category        string
	Scope           Scope
	Tags            []string
	SourceMemoryIDs []string
}

// Create stores a new KB page.
func (s *Service) Create(ctx context.Context, input CreateInput) (*Page, error) {
	userID := input.UserID
	if userID == "" {
		userID = "default"
	}
	if input.Title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if input.Content == "" {
		return nil, fmt.Errorf("content is required")
	}

	scope := input.Scope
	if scope == "" {
		scope = ScopePrivate
	}
	if scope != ScopePrivate && scope != ScopePublic {
		return nil, fmt.Errorf("invalid scope: %q (must be %q or %q)", scope, ScopePrivate, ScopePublic)
	}

	slug := input.Slug
	if slug == "" {
		slug = slugify(input.Title)
	}

	now := time.Now().UTC()
	p := &Page{
		PageID:          uuid.New().String(),
		UserID:          userID,
		Title:           input.Title,
		Slug:            slug,
		Content:         input.Content,
		Summary:         input.Summary,
		Category:        input.Category,
		Scope:           scope,
		Tags:            input.Tags,
		SourceMemoryIDs: input.SourceMemoryIDs,
		Version:         1,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if p.SourceMemoryIDs == nil {
		p.SourceMemoryIDs = []string{}
	}

	if err := s.Store.Put(ctx, p); err != nil {
		return nil, fmt.Errorf("store page: %w", err)
	}
	return p, nil
}

// UpdateInput holds input parameters for Update.
type UpdateInput struct {
	PageID          string
	Title           string
	Slug            string
	Content         string
	Summary         string
	Category        string
	Scope           Scope
	Tags            []string
	SourceMemoryIDs []string
}

// Update modifies an existing KB page.
func (s *Service) Update(ctx context.Context, input UpdateInput) (*Page, error) {
	if input.PageID == "" {
		return nil, fmt.Errorf("page_id is required")
	}

	p, err := s.Store.Get(ctx, input.PageID)
	if err != nil {
		return nil, fmt.Errorf("get page: %w", err)
	}

	if input.Title != "" {
		p.Title = input.Title
	}
	if input.Slug != "" {
		p.Slug = input.Slug
	}
	if input.Content != "" {
		p.Content = input.Content
	}
	if input.Summary != "" {
		p.Summary = input.Summary
	}
	if input.Category != "" {
		p.Category = input.Category
	}
	if input.Scope != "" {
		if input.Scope != ScopePrivate && input.Scope != ScopePublic {
			return nil, fmt.Errorf("invalid scope: %q (must be %q or %q)", input.Scope, ScopePrivate, ScopePublic)
		}
		p.Scope = input.Scope
	}
	if input.Tags != nil {
		p.Tags = input.Tags
	}
	if input.SourceMemoryIDs != nil {
		p.SourceMemoryIDs = input.SourceMemoryIDs
	}

	p.Version++
	p.UpdatedAt = time.Now().UTC()

	if err := s.Store.Update(ctx, p); err != nil {
		return nil, fmt.Errorf("update page: %w", err)
	}
	return p, nil
}

// Get retrieves a KB page by ID.
func (s *Service) Get(ctx context.Context, pageID string) (*Page, error) {
	if pageID == "" {
		return nil, fmt.Errorf("page_id is required")
	}
	p, err := s.Store.Get(ctx, pageID)
	if err != nil {
		return nil, fmt.Errorf("get page: %w", err)
	}
	return p, nil
}

// GetBySlug retrieves a KB page by slug.
func (s *Service) GetBySlug(ctx context.Context, slug string) (*Page, error) {
	if slug == "" {
		return nil, fmt.Errorf("slug is required")
	}
	p, err := s.Store.GetBySlug(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("get page by slug: %w", err)
	}
	return p, nil
}

// ListInput holds input parameters for List.
type ListInput struct {
	UserID    string
	Category  string
	Limit     int
	NextToken *string
}

// ListResult holds the result of List.
type ListResult struct {
	Pages     []*Page `json:"pages"`
	NextToken *string `json:"next_token,omitempty"`
}

// List returns KB pages for a user with optional category filtering.
func (s *Service) List(ctx context.Context, input ListInput) (*ListResult, error) {
	userID := input.UserID
	if userID == "" {
		userID = "default"
	}
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}

	pages, newNextToken, err := s.Store.ListByUserID(ctx, userID, input.Category, limit, input.NextToken)
	if err != nil {
		return nil, fmt.Errorf("list pages: %w", err)
	}

	return &ListResult{
		Pages:     pages,
		NextToken: newNextToken,
	}, nil
}

// Search performs a simple keyword search over KB page titles, summaries, and content.
// This is a basic implementation; semantic vector search can be added in a future phase.
func (s *Service) Search(ctx context.Context, userID string, query string, limit int) ([]*SearchResult, error) {
	if userID == "" {
		userID = "default"
	}
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if limit == 0 {
		limit = 10
	}

	// Fetch all pages for the user (up to 100) and do in-memory keyword search.
	result, err := s.List(ctx, ListInput{UserID: userID, Limit: 100})
	if err != nil {
		return nil, fmt.Errorf("list pages for search: %w", err)
	}

	queryLower := strings.ToLower(query)
	var results []*SearchResult
	for _, p := range result.Pages {
		score := 0.0
		if strings.Contains(strings.ToLower(p.Title), queryLower) {
			score += 3.0
		}
		if strings.Contains(strings.ToLower(p.Summary), queryLower) {
			score += 2.0
		}
		if strings.Contains(strings.ToLower(p.Content), queryLower) {
			score += 1.0
		}
		if score > 0 {
			results = append(results, &SearchResult{Page: p, Score: score})
		}
	}

	// Sort by score descending (simple insertion sort for small sets).
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].Score > results[j-1].Score; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// Delete removes a KB page.
func (s *Service) Delete(ctx context.Context, pageID string) error {
	if pageID == "" {
		return fmt.Errorf("page_id is required")
	}
	if err := s.Store.Delete(ctx, pageID); err != nil {
		return fmt.Errorf("delete page: %w", err)
	}
	return nil
}

// slugify converts a title to a URL-friendly slug.
func slugify(title string) string {
	lower := strings.ToLower(title)
	var b strings.Builder
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	// Collapse multiple dashes.
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if slug == "" {
		slug = uuid.New().String()[:8]
	}
	return slug
}
