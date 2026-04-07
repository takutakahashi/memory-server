package inbox

import "time"

// Status represents the processing status of an inbox entry.
type Status string

const (
	// StatusPending means the entry has not been processed by Curator yet.
	StatusPending Status = "pending"
	// StatusProcessed means the entry has been processed by Curator.
	StatusProcessed Status = "processed"
	// StatusArchived means the entry has been manually archived.
	StatusArchived Status = "archived"
)

// Entry represents a single inbox entry.
type Entry struct {
	InboxID     string     `json:"inbox_id" dynamodbav:"inbox_id"`
	UserID      string     `json:"user_id" dynamodbav:"user_id"`
	Content     string     `json:"content" dynamodbav:"content"`
	Source      string     `json:"source" dynamodbav:"source"`
	Status      Status     `json:"status" dynamodbav:"status"`
	Tags        []string   `json:"tags" dynamodbav:"tags"`
	CreatedAt   time.Time  `json:"created_at" dynamodbav:"created_at"`
	ProcessedAt *time.Time `json:"processed_at,omitempty" dynamodbav:"processed_at,omitempty"`
}
