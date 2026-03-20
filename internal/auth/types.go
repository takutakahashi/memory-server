package auth

import "time"

// User represents a registered user with an API token.
type User struct {
	UserID      string    `json:"user_id" dynamodbav:"user_id"`
	Token       string    `json:"token" dynamodbav:"token"`
	Description string    `json:"description" dynamodbav:"description"`
	CreatedAt   time.Time `json:"created_at" dynamodbav:"created_at"`
}
