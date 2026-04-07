package kb

import "time"

// Scope defines the visibility of a KB page.
type Scope string

const (
	// ScopePrivate means only the owner can see the page.
	ScopePrivate Scope = "private"
	// ScopePublic means all users can see the page.
	ScopePublic Scope = "public"
)

// Page represents a single Knowledge Base page.
type Page struct {
	PageID          string    `json:"page_id" dynamodbav:"page_id"`
	UserID          string    `json:"user_id" dynamodbav:"user_id"`
	Title           string    `json:"title" dynamodbav:"title"`
	Slug            string    `json:"slug" dynamodbav:"slug"`
	Content         string    `json:"content" dynamodbav:"content"`
	Summary         string    `json:"summary" dynamodbav:"summary"`
	Category        string    `json:"category" dynamodbav:"category"`
	Scope           Scope     `json:"scope" dynamodbav:"scope"`
	Tags            []string  `json:"tags" dynamodbav:"tags"`
	SourceMemoryIDs []string  `json:"source_memory_ids" dynamodbav:"source_memory_ids"`
	Version         int       `json:"version" dynamodbav:"version"`
	CreatedAt       time.Time `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt       time.Time `json:"updated_at" dynamodbav:"updated_at"`
}

// SearchResult represents a KB page with a relevance score.
type SearchResult struct {
	Page  *Page   `json:"page"`
	Score float64 `json:"score"`
}
