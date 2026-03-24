package memory

import "time"

// Scope defines the visibility of a memory.
type Scope string

const (
	// ScopePrivate means only the owner can see the memory.
	ScopePrivate Scope = "private"
	// ScopePublic means all users can see the memory.
	ScopePublic Scope = "public"
	// ScopeOrg means all members of the org (identified by org_id) can see the memory.
	ScopeOrg Scope = "org"
)

// Memory represents a single memory entry.
type Memory struct {
	MemoryID       string    `json:"memory_id" dynamodbav:"memory_id"`
	UserID         string    `json:"user_id" dynamodbav:"user_id"`
	OrgID          string    `json:"org_id,omitempty" dynamodbav:"org_id,omitempty"`
	Scope          Scope     `json:"scope" dynamodbav:"scope"`
	Content        string    `json:"content" dynamodbav:"content"`
	Tags           []string  `json:"tags" dynamodbav:"tags"`
	CreatedAt      time.Time `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" dynamodbav:"updated_at"`
	LastAccessedAt time.Time `json:"last_accessed_at" dynamodbav:"last_accessed_at"`
	AccessCount    int64     `json:"access_count" dynamodbav:"access_count"`
	VectorID       string    `json:"vector_id" dynamodbav:"vector_id"`
}

// SearchResult represents a memory with its relevance score.
type SearchResult struct {
	Memory          *Memory `json:"memory"`
	SimilarityScore float64 `json:"similarity_score"`
	FinalScore      float64 `json:"final_score"`
}

// VectorResult represents a result from S3 Vectors query.
type VectorResult struct {
	Key      string            `json:"key"`
	Score    float64           `json:"score"`
	Metadata map[string]string `json:"metadata"`
}
