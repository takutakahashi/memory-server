package kb

import (
	"context"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// Store handles DynamoDB operations for KB pages.
type Store struct {
	client    *dynamodb.Client
	tableName string
}

// NewStore creates a new Store instance.
func NewStore(cfg aws.Config) *Store {
	tableName := os.Getenv("KB_TABLE_NAME")
	if tableName == "" {
		tableName = "kb_pages"
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

// Put saves a KB page to DynamoDB.
func (s *Store) Put(ctx context.Context, p *Page) error {
	item, err := attributevalue.MarshalMap(p)
	if err != nil {
		return fmt.Errorf("marshal page: %w", err)
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

// Get retrieves a single KB page by page_id.
func (s *Store) Get(ctx context.Context, pageID string) (*Page, error) {
	result, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"page_id": &types.AttributeValueMemberS{Value: pageID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get item: %w", err)
	}
	if result.Item == nil {
		return nil, fmt.Errorf("%w: kb page %s", ErrNotFound, pageID)
	}
	var p Page
	if err := attributevalue.UnmarshalMap(result.Item, &p); err != nil {
		return nil, fmt.Errorf("unmarshal page: %w", err)
	}
	normalizeScope(&p)
	return &p, nil
}

// GetBySlug retrieves a KB page by slug using the slug-index GSI.
func (s *Store) GetBySlug(ctx context.Context, slug string) (*Page, error) {
	result, err := s.client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		IndexName:              aws.String("slug-index"),
		KeyConditionExpression: aws.String("slug = :slug"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":slug": &types.AttributeValueMemberS{Value: slug},
		},
		Limit: aws.Int32(1),
	})
	if err != nil {
		return nil, fmt.Errorf("query by slug: %w", err)
	}
	if len(result.Items) == 0 {
		return nil, fmt.Errorf("%w: kb page with slug %q", ErrNotFound, slug)
	}
	var p Page
	if err := attributevalue.UnmarshalMap(result.Items[0], &p); err != nil {
		return nil, fmt.Errorf("unmarshal page: %w", err)
	}
	normalizeScope(&p)
	return &p, nil
}

// ListByUserID lists KB pages for a user, sorted by updated_at descending.
func (s *Store) ListByUserID(ctx context.Context, userID string, category string, limit int, nextToken *string) ([]*Page, *string, error) {
	input := &dynamodb.QueryInput{
		TableName:              aws.String(s.tableName),
		IndexName:              aws.String("user_id-updated_at-index"),
		KeyConditionExpression: aws.String("user_id = :uid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":uid": &types.AttributeValueMemberS{Value: userID},
		},
		ScanIndexForward: aws.Bool(false),
		Limit:            aws.Int32(int32(limit)),
	}

	if category != "" {
		input.FilterExpression = aws.String("category = :category")
		input.ExpressionAttributeValues[":category"] = &types.AttributeValueMemberS{Value: category}
	}

	if nextToken != nil && *nextToken != "" {
		input.ExclusiveStartKey = map[string]types.AttributeValue{
			"page_id": &types.AttributeValueMemberS{Value: *nextToken},
		}
	}

	result, err := s.client.Query(ctx, input)
	if err != nil {
		return nil, nil, fmt.Errorf("query kb pages: %w", err)
	}

	pages := make([]*Page, 0, len(result.Items))
	for _, item := range result.Items {
		var p Page
		if err := attributevalue.UnmarshalMap(item, &p); err != nil {
			return nil, nil, fmt.Errorf("unmarshal page: %w", err)
		}
		normalizeScope(&p)
		pages = append(pages, &p)
	}

	var newNextToken *string
	if result.LastEvaluatedKey != nil {
		if v, ok := result.LastEvaluatedKey["page_id"]; ok {
			if sv, ok := v.(*types.AttributeValueMemberS); ok {
				newNextToken = aws.String(sv.Value)
			}
		}
	}

	return pages, newNextToken, nil
}

// Update updates a KB page in DynamoDB.
func (s *Store) Update(ctx context.Context, p *Page) error {
	scope := string(p.Scope)
	if scope == "" {
		scope = string(ScopePrivate)
	}

	tags, err := marshalStringList(p.Tags)
	if err != nil {
		return fmt.Errorf("marshal tags: %w", err)
	}
	sourceMemoryIDs, err := marshalStringList(p.SourceMemoryIDs)
	if err != nil {
		return fmt.Errorf("marshal source_memory_ids: %w", err)
	}

	_, err = s.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"page_id": &types.AttributeValueMemberS{Value: p.PageID},
		},
		UpdateExpression: aws.String("SET title = :title, slug = :slug, content = :content, summary = :summary, category = :category, #scope = :scope, tags = :tags, source_memory_ids = :smi, version = :version, updated_at = :updated_at"),
		ExpressionAttributeNames: map[string]string{
			"#scope": "scope",
		},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":title":      &types.AttributeValueMemberS{Value: p.Title},
			":slug":       &types.AttributeValueMemberS{Value: p.Slug},
			":content":    &types.AttributeValueMemberS{Value: p.Content},
			":summary":    &types.AttributeValueMemberS{Value: p.Summary},
			":category":   &types.AttributeValueMemberS{Value: p.Category},
			":scope":      &types.AttributeValueMemberS{Value: scope},
			":tags":       tags,
			":smi":        sourceMemoryIDs,
			":version":    &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", p.Version)},
			":updated_at": &types.AttributeValueMemberS{Value: p.UpdatedAt.Format("2006-01-02T15:04:05Z07:00")},
		},
	})
	if err != nil {
		return fmt.Errorf("update item: %w", err)
	}
	return nil
}

// Delete removes a KB page from DynamoDB.
func (s *Store) Delete(ctx context.Context, pageID string) error {
	_, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.tableName),
		Key: map[string]types.AttributeValue{
			"page_id": &types.AttributeValueMemberS{Value: pageID},
		},
	})
	if err != nil {
		return fmt.Errorf("delete item: %w", err)
	}
	return nil
}

// normalizeScope ensures Scope is always set.
func normalizeScope(p *Page) {
	if p.Scope == "" {
		p.Scope = ScopePrivate
	}
}

func marshalStringList(ss []string) (types.AttributeValue, error) {
	if len(ss) == 0 {
		return &types.AttributeValueMemberL{Value: []types.AttributeValue{}}, nil
	}
	items := make([]types.AttributeValue, len(ss))
	for i, s := range ss {
		items[i] = &types.AttributeValueMemberS{Value: s}
	}
	return &types.AttributeValueMemberL{Value: items}, nil
}
