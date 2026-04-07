package inbox

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

// Store handles DynamoDB operations for inbox entries.
type Store struct {
	client    *dynamodb.Client
	tableName string
}

// NewStore creates a new Store instance.
func NewStore(cfg aws.Config) *Store {
	tableName := os.Getenv("INBOX_TABLE_NAME")
	if tableName == "" {
		tableName = "inbox"
	}
	opts := []func(*dynamodb.Options){}
	if ep := os.Getenv("DYNAMODB_ENDPOINT_URL"); ep != "" {
		opts = append(opts, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(ep)
		})
	}
	return &Store{
		client:    dynamodb.NewFromConfig(cfg, opts...),
		tableName: tableName,
	}
}

// Put saves an inbox entry to DynamoDB.
func (s *Store) Put(ctx context.Context, e *Entry) error {
	item, err := attributevalue.MarshalMap(e)
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.tableName),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("put item: %w", err)
	}
	return nil
}

// Get retrieves a single inbox entry by inbox_id.
func (s *Store) Get(ctx context.Context, inboxID string) (*Entry, error) {
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"inbox_id": &types.AttributeValueMemberS{Value: inboxID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	if result.Item == nil {
		return nil, fmt.Errorf("%w: inbox entry %s", ErrNotFound, inboxID)
	}
	var e Entry
	if err := attributevalue.UnmarshalMap(result.Item, &e); err != nil {
		return nil, fmt.Errorf("unmarshal entry: %w", err)
	}
	return &e, nil
}

// ListByUserID lists inbox entries for a user, sorted by created_at descending.
func (s *Store) ListByUserID(ctx context.Context, userID string, status Status, limit int, nextToken *string) ([]*Entry, *string, error) {
	input := &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		IndexName:              aws.String("user_id-created_at-index"),
		KeyConditionExpression: aws.String("user_id = :uid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid": &types.AttributeValueMemberS{Value: userID},
		},
		ScanIndexForward: aws.Bool(false),
		Limit:            aws.Int32(int32(limit)),
	}

	if status != "" {
		input.FilterExpression = aws.String("#s = :status")
		input.ExpressionAttributeNames = map[string]string{"#s": "status"}
		input.ExpressionAttributeValues[":status"] = &types.AttributeValueMemberS{Value: string(status)}
	}

	if nextToken != nil && *nextToken != "" {
		input.ExclusiveStartKey = map[string]types.AttributeValue{
			"inbox_id": &types.AttributeValueMemberS{Value: *nextToken},
		}
	}

	result, err := s.client.Query(ctx, input)
	if err != nil {
		return nil, nil, fmt.Errorf("query inbox entries: %w", err)
	}

	entries := make([]*Entry, 0, len(result.Items))
	for _, item := range result.Items {
		var e Entry
		if err := attributevalue.UnmarshalMap(item, &e); err != nil {
			return nil, nil, fmt.Errorf("unmarshal entry: %w", err)
		}
		entries = append(entries, &e)
	}

	var newNextToken *string
	if result.LastEvaluatedKey != nil {
		if v, ok := result.LastEvaluatedKey["inbox_id"]; ok {
			if sv, ok := v.(*types.AttributeValueMemberS); ok {
				newNextToken = aws.String(sv.Value)
			}
		}
	}

	return entries, newNextToken, nil
}

// ListPendingByStatus returns all pending entries across all users (used by Curator).
func (s *Store) ListPendingByStatus(ctx context.Context, limit int) ([]*Entry, error) {
	input := &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		IndexName:              aws.String("status-created_at-index"),
		KeyConditionExpression: aws.String("#s = :status"),
		ExpressionAttributeNames: map[string]string{
			"#s": "status",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":status": &types.AttributeValueMemberS{Value: string(StatusPending)},
		},
		ScanIndexForward: aws.Bool(true),
		Limit:            aws.Int32(int32(limit)),
	}

	result, err := s.client.Query(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("query pending entries: %w", err)
	}

	entries := make([]*Entry, 0, len(result.Items))
	for _, item := range result.Items {
		var e Entry
		if err := attributevalue.UnmarshalMap(item, &e); err != nil {
			return nil, fmt.Errorf("unmarshal entry: %w", err)
		}
		entries = append(entries, &e)
	}
	return entries, nil
}

// UpdateStatus updates the status and processed_at of an inbox entry.
func (s *Store) UpdateStatus(ctx context.Context, inboxID string, status Status) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"inbox_id": &types.AttributeValueMemberS{Value: inboxID},
		},
		UpdateExpression: aws.String("SET #s = :status, processed_at = :now"),
		ExpressionAttributeNames: map[string]string{
			"#s": "status",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":status": &types.AttributeValueMemberS{Value: string(status)},
			":now":    &types.AttributeValueMemberS{Value: now},
		},
	})
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	return nil
}

// Delete removes an inbox entry from DynamoDB.
func (s *Store) Delete(ctx context.Context, inboxID string) error {
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"inbox_id": &types.AttributeValueMemberS{Value: inboxID},
		},
	})
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	return nil
}

// Ping checks connectivity to DynamoDB.
func (s *Store) Ping(ctx context.Context) error {
	_, err := s.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(s.tableName),
	})
	return err
}
