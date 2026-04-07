package inbox

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/google/uuid"
)


// Service provides high-level inbox operations.
type Service struct {
	Store *Store
}

// NewService creates a new Service using the given AWS config.
func NewService(cfg aws.Config) *Service {
	return &Service{
		Store: NewStore(cfg),
	}
}

// AddInput holds input parameters for Add.
type AddInput struct {
	UserID  string
	Content string
	Source  string
	Tags    []string
}

// Add stores a new inbox entry.
func (s *Service) Add(ctx context.Context, input AddInput) (*Entry, error) {
	userID := input.UserID
	if userID == "" {
		userID = "default"
	}
	if input.Content == "" {
		return nil, fmt.Errorf("content is required")
	}

	now := time.Now().UTC()
	e := &Entry{
		InboxID:   uuid.New().String(),
		UserID:    userID,
		Content:   input.Content,
		Source:    input.Source,
		Status:    StatusPending,
		Tags:      input.Tags,
		CreatedAt: now,
	}
	if e.Tags == nil {
		e.Tags = []string{}
	}

	if err := s.Store.Put(ctx, e); err != nil {
		return nil, fmt.Errorf("store entry: %w", err)
	}
	return e, nil
}

// ListInput holds input parameters for List.
type ListInput struct {
	UserID    string
	Status    Status
	Limit     int
	NextToken *string
}

// ListResult holds the result of List.
type ListResult struct {
	Entries   []*Entry `json:"entries"`
	NextToken *string  `json:"next_token,omitempty"`
}

// List returns inbox entries for a user with optional status filtering.
func (s *Service) List(ctx context.Context, input ListInput) (*ListResult, error) {
	userID := input.UserID
	if userID == "" {
		userID = "default"
	}
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}

	entries, newNextToken, err := s.Store.ListByUserID(ctx, userID, input.Status, limit, input.NextToken)
	if err != nil {
		return nil, fmt.Errorf("list entries: %w", err)
	}

	return &ListResult{
		Entries:   entries,
		NextToken: newNextToken,
	}, nil
}

// Get retrieves a single inbox entry by ID.
func (s *Service) Get(ctx context.Context, inboxID string) (*Entry, error) {
	if inboxID == "" {
		return nil, fmt.Errorf("inbox_id is required")
	}
	e, err := s.Store.Get(ctx, inboxID)
	if err != nil {
		return nil, fmt.Errorf("get entry: %w", err)
	}
	return e, nil
}

// Archive marks an inbox entry as archived.
func (s *Service) Archive(ctx context.Context, inboxID string) error {
	if inboxID == "" {
		return fmt.Errorf("inbox_id is required")
	}
	if _, err := s.Store.Get(ctx, inboxID); err != nil {
		return fmt.Errorf("get entry: %w", err)
	}
	if err := s.Store.UpdateStatus(ctx, inboxID, StatusArchived); err != nil {
		return fmt.Errorf("archive entry: %w", err)
	}
	return nil
}

// Delete removes an inbox entry.
func (s *Service) Delete(ctx context.Context, inboxID string) error {
	if inboxID == "" {
		return fmt.Errorf("inbox_id is required")
	}
	if err := s.Store.Delete(ctx, inboxID); err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	return nil
}

// MarkProcessed marks an inbox entry as processed.
func (s *Service) MarkProcessed(ctx context.Context, inboxID string) error {
	if err := s.Store.UpdateStatus(ctx, inboxID, StatusProcessed); err != nil {
		return fmt.Errorf("mark processed: %w", err)
	}
	return nil
}

// ListPending returns all pending entries (used by Curator).
func (s *Service) ListPending(ctx context.Context, limit int) ([]*Entry, error) {
	if limit == 0 {
		limit = 100
	}
	entries, err := s.Store.ListPendingByStatus(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	return entries, nil
}

