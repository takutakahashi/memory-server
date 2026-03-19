package memory

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// User represents a user with an auth token.
type User struct {
	UserID    string    `dynamodbav:"user_id" json:"user_id"`
	Token     string    `dynamodbav:"token"   json:"token"`
	CreatedAt time.Time `dynamodbav:"created_at" json:"created_at"`
}

// UserStore manages users in DynamoDB.
type UserStore struct {
	client    *dynamodb.Client
	tableName string
}

// NewUserStore creates a new UserStore using the given AWS config.
// Table name is read from USERS_TABLE_NAME env (default: "memory-users").
func NewUserStore(cfg aws.Config) *UserStore {
	tableName := os.Getenv("USERS_TABLE_NAME")
	if tableName == "" {
		tableName = "memory-users"
	}
	return &UserStore{
		client:    dynamodb.NewFromConfig(cfg),
		tableName: tableName,
	}
}

// CreateUser saves a new user to DynamoDB.
func (s *UserStore) CreateUser(ctx context.Context, u *User) error {
	item, err := attributevalue.MarshalMap(u)
	if err != nil {
		return fmt.Errorf("marshal user: %w", err)
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      item,
	})
	return err
}

// GetByToken looks up a user by their auth token using the token-index GSI.
func (s *UserStore) GetByToken(ctx context.Context, token string) (*User, error) {
	out, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		IndexName:              aws.String("token-index"),
		KeyConditionExpression: aws.String("token = :t"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":t": &types.AttributeValueMemberS{Value: token},
		},
		Limit: aws.Int32(1),
	})
	if err != nil {
		return nil, fmt.Errorf("query token-index: %w", err)
	}
	if len(out.Items) == 0 {
		return nil, ErrUnauthorized
	}
	var u User
	if err := attributevalue.UnmarshalMap(out.Items[0], &u); err != nil {
		return nil, fmt.Errorf("unmarshal user: %w", err)
	}
	return &u, nil
}
