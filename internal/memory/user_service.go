package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// UserService provides user management operations.
type UserService struct {
	Store *UserStore
}

// CreateUserResult is the result of CreateUser.
type CreateUserResult struct {
	UserID string `json:"user_id"`
	Token  string `json:"token"`
}

// CreateUser creates a new user with a generated ID and token.
func (us *UserService) CreateUser(ctx context.Context) (*CreateUserResult, error) {
	u := &User{
		UserID:    uuid.New().String(),
		Token:     uuid.New().String(),
		CreatedAt: time.Now().UTC(),
	}
	if err := us.Store.CreateUser(ctx, u); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &CreateUserResult{UserID: u.UserID, Token: u.Token}, nil
}

// ResolveUserByToken returns the User for the given token.
func (us *UserService) ResolveUserByToken(ctx context.Context, token string) (*User, error) {
	return us.Store.GetByToken(ctx, token)
}
