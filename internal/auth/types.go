package auth

import "time"

// OrgToken represents an organization-level token used to register users.
type OrgToken struct {
	Token       string    `json:"token" dynamodbav:"token"`
	OrgID       string    `json:"org_id" dynamodbav:"org_id"`
	Description string    `json:"description" dynamodbav:"description"`
	CreatedAt   time.Time `json:"created_at" dynamodbav:"created_at"`
}

// User represents a registered user with an API token.
type User struct {
	UserID      string    `json:"user_id" dynamodbav:"user_id"`
	Token       string    `json:"token" dynamodbav:"token"`
	Description string    `json:"description" dynamodbav:"description"`
	OrgID       string    `json:"org_id" dynamodbav:"org_id"`
	CreatedAt   time.Time `json:"created_at" dynamodbav:"created_at"`
}
