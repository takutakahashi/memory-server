package auth

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// ErrNotFound is returned when a user is not found.
var ErrNotFound = errors.New("not found")

// UserStorer is the interface used by handlers to read and write users.
// The concrete DynamoDB Store implements this; tests may use a fake.
type UserStorer interface {
	PutUser(ctx context.Context, u *User) error
	GetUser(ctx context.Context, userID string) (*User, error)
	GetUserByToken(ctx context.Context, token string) (*User, error)
}

// Store handles DynamoDB operations for users.
type Store struct {
	client     *dynamodb.Client
	usersTable string
}

// NewStore creates a new auth Store.
func NewStore(cfg aws.Config) *Store {
	usersTable := os.Getenv("USERS_TABLE_NAME")
	if usersTable == "" {
		usersTable = "users"
	}

	// Reuse endpoint override for local development (LocalStack)
	client := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if ep := os.Getenv("DYNAMODB_ENDPOINT_URL"); ep != "" {
			o.BaseEndpoint = aws.String(ep)
		}
	})

	return &Store{
		client:     client,
		usersTable: usersTable,
	}
}

// PutUser saves a User to DynamoDB.
func (s *Store) PutUser(ctx context.Context, u *User) error {
	item, err := attributevalue.MarshalMap(u)
	if err != nil {
		return fmt.Errorf("marshal user: %w", err)
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.usersTable),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("put user: %w", err)
	}
	return nil
}

// GetUserByToken looks up a User by their API token using GSI "token-index".
// Returns ErrNotFound if no user matches.
func (s *Store) GetUserByToken(ctx context.Context, token string) (*User, error) {
	result, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.usersTable),
		IndexName:              aws.String("token-index"),
		KeyConditionExpression: aws.String("#t = :token"),
		ExpressionAttributeNames: map[string]string{
			"#t": "token",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":token": &types.AttributeValueMemberS{Value: token},
		},
		Limit: aws.Int32(1),
	})
	if err != nil {
		return nil, fmt.Errorf("query user by token: %w", err)
	}
	if len(result.Items) == 0 {
		return nil, fmt.Errorf("%w: user token", ErrNotFound)
	}

	var u User
	if err := attributevalue.UnmarshalMap(result.Items[0], &u); err != nil {
		return nil, fmt.Errorf("unmarshal user: %w", err)
	}
	return &u, nil
}

// GetUser retrieves a User by user_id.
func (s *Store) GetUser(ctx context.Context, userID string) (*User, error) {
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.usersTable),
		Key: map[string]types.AttributeValue{
			"user_id": &types.AttributeValueMemberS{Value: userID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	if result.Item == nil {
		return nil, fmt.Errorf("%w: user %s", ErrNotFound, userID)
	}

	var u User
	if err := attributevalue.UnmarshalMap(result.Item, &u); err != nil {
		return nil, fmt.Errorf("unmarshal user: %w", err)
	}
	return &u, nil
}
