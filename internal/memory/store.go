package memory

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Store handles DynamoDB operations for memories.
type Store struct {
	client    *dynamodb.Client
	tableName string
}

// NewStore creates a new Store instance.
func NewStore(cfg aws.Config) *Store {
	tableName := os.Getenv("DYNAMODB_TABLE_NAME")
	if tableName == "" {
		tableName = "memories"
	}
	return &Store{
		client:    dynamodb.NewFromConfig(cfg),
		tableName: tableName,
	}
}

// Put saves a memory to DynamoDB.
func (s *Store) Put(ctx context.Context, m *Memory) error {
	item, err := attributevalue.MarshalMap(m)
	if err != nil {
		return fmt.Errorf("marshal memory: %w", err)
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

// normalizeScope ensures Scope is always set; legacy records with empty scope default to private.
func normalizeScope(m *Memory) {
	if m.Scope == "" {
		m.Scope = ScopePrivate
	}
}

// Get retrieves a single memory by memory_id.
func (s *Store) Get(ctx context.Context, memoryID string) (*Memory, error) {
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"memory_id": &types.AttributeValueMemberS{Value: memoryID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	if result.Item == nil {
		return nil, fmt.Errorf("%w: memory %s", ErrNotFound, memoryID)
	}

	var m Memory
	if err := attributevalue.UnmarshalMap(result.Item, &m); err != nil {
		return nil, fmt.Errorf("unmarshal memory: %w", err)
	}
	normalizeScope(&m)
	return &m, nil
}

// GetByIDs retrieves multiple memories by their IDs.
func (s *Store) GetByIDs(ctx context.Context, memoryIDs []string) ([]*Memory, error) {
	if len(memoryIDs) == 0 {
		return nil, nil
	}

	keys := make([]map[string]types.AttributeValue, 0, len(memoryIDs))
	for _, id := range memoryIDs {
		keys = append(keys, map[string]types.AttributeValue{
			"memory_id": &types.AttributeValueMemberS{Value: id},
		})
	}

	// BatchGetItem supports up to 100 items per request
	var memories []*Memory
	for i := 0; i < len(keys); i += 100 {
		end := i + 100
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[i:end]

		result, err := s.client.BatchGetItem(ctx, &dynamodb.BatchGetItemInput{
			RequestItems: map[string]types.KeysAndAttributes{
				s.tableName: {
					Keys: batch,
				},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("batch get items: %w", err)
		}

		items := result.Responses[s.tableName]
		for _, item := range items {
			var m Memory
			if err := attributevalue.UnmarshalMap(item, &m); err != nil {
				return nil, fmt.Errorf("unmarshal memory: %w", err)
			}
			normalizeScope(&m)
			memories = append(memories, &m)
		}
	}

	return memories, nil
}

// ListByUserID lists memories for a user using GSI1 (user_id-created_at-index), sorted by created_at descending.
func (s *Store) ListByUserID(ctx context.Context, userID string, limit int, nextToken *string) ([]*Memory, *string, error) {
	input := &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		IndexName:              aws.String("user_id-created_at-index"),
		KeyConditionExpression: aws.String("user_id = :uid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid": &types.AttributeValueMemberS{Value: userID},
		},
		ScanIndexForward: aws.Bool(false), // descending
		Limit:            aws.Int32(int32(limit)),
	}

	if nextToken != nil && *nextToken != "" {
		input.ExclusiveStartKey = map[string]types.AttributeValue{
			"memory_id": &types.AttributeValueMemberS{Value: *nextToken},
		}
	}

	result, err := s.client.Query(ctx, input)
	if err != nil {
		return nil, nil, fmt.Errorf("query memories: %w", err)
	}

	memories := make([]*Memory, 0, len(result.Items))
	for _, item := range result.Items {
		var m Memory
		if err := attributevalue.UnmarshalMap(item, &m); err != nil {
			return nil, nil, fmt.Errorf("unmarshal memory: %w", err)
		}
		normalizeScope(&m)
		memories = append(memories, &m)
	}

	var newNextToken *string
	if result.LastEvaluatedKey != nil {
		if v, ok := result.LastEvaluatedKey["memory_id"]; ok {
			if sv, ok := v.(*types.AttributeValueMemberS); ok {
				newNextToken = aws.String(sv.Value)
			}
		}
	}

	return memories, newNextToken, nil
}

// Update updates mutable fields of a memory in DynamoDB.
func (s *Store) Update(ctx context.Context, m *Memory) error {
	scope := string(m.Scope)
	if scope == "" {
		scope = string(ScopePrivate)
	}

	// "scope" is a DynamoDB reserved keyword; use #scope alias via ExpressionAttributeNames.
	updateExpr := "SET content = :c, tags = :t, updated_at = :ua, vector_id = :vi, #scope = :sc"
	exprNames := map[string]string{"#scope": "scope"}
	exprValues := map[string]types.AttributeValue{
		":c":  &types.AttributeValueMemberS{Value: m.Content},
		":t":  mustMarshalStringList(m.Tags),
		":ua": &types.AttributeValueMemberS{Value: m.UpdatedAt.Format(time.RFC3339)},
		":vi": &types.AttributeValueMemberS{Value: m.VectorID},
		":sc": &types.AttributeValueMemberS{Value: scope},
	}
	// Conditionally update org_id: set when present, remove when cleared.
	if m.OrgID != "" {
		updateExpr += ", org_id = :oid"
		exprValues[":oid"] = &types.AttributeValueMemberS{Value: m.OrgID}
	} else {
		updateExpr += " REMOVE org_id"
	}

	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"memory_id": &types.AttributeValueMemberS{Value: m.MemoryID},
		},
		UpdateExpression:          aws.String(updateExpr),
		ExpressionAttributeNames:  exprNames,
		ExpressionAttributeValues: exprValues,
	})
	if err != nil {
		return fmt.Errorf("update item: %w", err)
	}
	return nil
}

// ListByOrgID lists memories for an org using GSI "org_id-created_at-index", sorted by created_at descending.
func (s *Store) ListByOrgID(ctx context.Context, orgID string, limit int, nextToken *string) ([]*Memory, *string, error) {
	input := &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		IndexName:              aws.String("org_id-created_at-index"),
		KeyConditionExpression: aws.String("org_id = :oid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":oid": &types.AttributeValueMemberS{Value: orgID},
		},
		ScanIndexForward: aws.Bool(false), // descending
		Limit:            aws.Int32(int32(limit)),
	}

	if nextToken != nil && *nextToken != "" {
		input.ExclusiveStartKey = map[string]types.AttributeValue{
			"memory_id": &types.AttributeValueMemberS{Value: *nextToken},
		}
	}

	result, err := s.client.Query(ctx, input)
	if err != nil {
		return nil, nil, fmt.Errorf("query org memories: %w", err)
	}

	memories := make([]*Memory, 0, len(result.Items))
	for _, item := range result.Items {
		var m Memory
		if err := attributevalue.UnmarshalMap(item, &m); err != nil {
			return nil, nil, fmt.Errorf("unmarshal memory: %w", err)
		}
		normalizeScope(&m)
		memories = append(memories, &m)
	}

	var newNextToken *string
	if result.LastEvaluatedKey != nil {
		if v, ok := result.LastEvaluatedKey["memory_id"]; ok {
			if sv, ok := v.(*types.AttributeValueMemberS); ok {
				newNextToken = aws.String(sv.Value)
			}
		}
	}

	return memories, newNextToken, nil
}

// UpdateAccess updates last_accessed_at and access_count for a memory.
func (s *Store) UpdateAccess(ctx context.Context, memoryID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"memory_id": &types.AttributeValueMemberS{Value: memoryID},
		},
		UpdateExpression: aws.String("SET last_accessed_at = :laa ADD access_count :one"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":laa": &types.AttributeValueMemberS{Value: now},
			":one": &types.AttributeValueMemberN{Value: "1"},
		},
	})
	if err != nil {
		return fmt.Errorf("update access: %w", err)
	}
	return nil
}

// Delete removes a memory from DynamoDB.
func (s *Store) Delete(ctx context.Context, memoryID string) error {
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"memory_id": &types.AttributeValueMemberS{Value: memoryID},
		},
	})
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	return nil
}

// Ping checks connectivity to DynamoDB by describing the table.
// AccessDeniedException is treated as success: it proves the endpoint is reachable
// even when the caller lacks DescribeTable permission.
func (s *Store) Ping(ctx context.Context) error {
	_, err := s.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(s.tableName),
	})
	if err != nil && strings.Contains(err.Error(), "AccessDeniedException") {
		return nil
	}
	return err
}

func mustMarshalStringList(tags []string) types.AttributeValue {
	if len(tags) == 0 {
		return &types.AttributeValueMemberL{Value: []types.AttributeValue{}}
	}
	items := make([]types.AttributeValue, len(tags))
	for i, t := range tags {
		items[i] = &types.AttributeValueMemberS{Value: t}
	}
	return &types.AttributeValueMemberL{Value: items}
}
