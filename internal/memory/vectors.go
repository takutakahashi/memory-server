package memory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

// S3VectorsClient handles S3 Vectors API operations using SigV4-signed HTTP requests.
// The S3 Vectors API uses POST /{OperationName} with operation parameters in the request body.
// Endpoint format: https://s3vectors.{region}.api.aws
type S3VectorsClient struct {
	cfg        aws.Config
	bucketName string
	indexName  string
	region     string
	endpoint   string
	httpClient *http.Client
	signer     *v4.Signer
}

// NewS3VectorsClient creates a new S3VectorsClient.
func NewS3VectorsClient(cfg aws.Config) *S3VectorsClient {
	bucketName := os.Getenv("S3_VECTORS_BUCKET_NAME")
	indexName := os.Getenv("S3_VECTORS_INDEX_NAME")
	if indexName == "" {
		indexName = "memories"
	}

	region := cfg.Region
	if region == "" {
		region = "ap-northeast-1"
	}

	endpoint := fmt.Sprintf("https://s3vectors.%s.api.aws", region)

	return &S3VectorsClient{
		cfg:        cfg,
		bucketName: bucketName,
		indexName:  indexName,
		region:     region,
		endpoint:   endpoint,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		signer:     v4.NewSigner(),
	}
}

// vectorData is the VectorData union type (only float32 is supported).
type vectorData struct {
	Float32 []float64 `json:"float32"`
}

// putVectorsRequest is the request body for POST /PutVectors.
type putVectorsRequest struct {
	VectorBucketName string       `json:"vectorBucketName"`
	IndexName        string       `json:"indexName"`
	Vectors          []vectorItem `json:"vectors"`
}

type vectorItem struct {
	Key      string                 `json:"key"`
	Data     vectorData             `json:"data"`
	Metadata map[string]interface{} `json:"metadata"`
}

// queryVectorsRequest is the request body for POST /QueryVectors.
type queryVectorsRequest struct {
	VectorBucketName string                 `json:"vectorBucketName"`
	IndexName        string                 `json:"indexName"`
	TopK             int                    `json:"topK"`
	QueryVector      vectorData             `json:"queryVector"`
	Filter           map[string]interface{} `json:"filter,omitempty"`
	ReturnMetadata   bool                   `json:"returnMetadata"`
}

// queryVectorsResponse is the response body for POST /QueryVectors.
type queryVectorsResponse struct {
	Vectors        []queryVectorItem `json:"vectors"`
	DistanceMetric string            `json:"distanceMetric"`
}

type queryVectorItem struct {
	Key      string                 `json:"key"`
	Distance float64                `json:"distance"`
	Metadata map[string]interface{} `json:"metadata"`
}

// deleteVectorsRequest is the request body for POST /DeleteVectors.
type deleteVectorsRequest struct {
	VectorBucketName string   `json:"vectorBucketName"`
	IndexName        string   `json:"indexName"`
	Keys             []string `json:"keys"`
}

// PutVectors stores a vector in S3 Vectors.
// orgID is optional; when non-empty it is stored as metadata so that org-scoped queries can filter by it.
func (c *S3VectorsClient) PutVectors(ctx context.Context, key string, embedding []float64, userID string, scope Scope, orgID string) error {
	if scope == "" {
		scope = ScopePrivate
	}
	metadata := map[string]interface{}{
		"user_id": userID,
		"scope":   string(scope),
	}
	if orgID != "" {
		metadata["org_id"] = orgID
	}
	reqBody := putVectorsRequest{
		VectorBucketName: c.bucketName,
		IndexName:        c.indexName,
		Vectors: []vectorItem{
			{
				Key:      key,
				Data:     vectorData{Float32: embedding},
				Metadata: metadata,
			},
		},
	}

	return c.doRequest(ctx, "/PutVectors", reqBody, nil)
}

// QueryVectorsOrg performs a similarity search for org-scoped vectors filtered by org_id.
func (c *S3VectorsClient) QueryVectorsOrg(ctx context.Context, embedding []float64, topK int, orgID string) ([]*VectorResult, error) {
	reqBody := queryVectorsRequest{
		VectorBucketName: c.bucketName,
		IndexName:        c.indexName,
		TopK:             topK,
		QueryVector:      vectorData{Float32: embedding},
		ReturnMetadata:   true,
		Filter: map[string]interface{}{
			"org_id": map[string]interface{}{
				"$eq": orgID,
			},
		},
	}

	return c.doQueryVectors(ctx, reqBody)
}

// QueryVectors performs a similarity search in S3 Vectors filtered by user_id.
func (c *S3VectorsClient) QueryVectors(ctx context.Context, embedding []float64, topK int, userID string) ([]*VectorResult, error) {
	reqBody := queryVectorsRequest{
		VectorBucketName: c.bucketName,
		IndexName:        c.indexName,
		TopK:             topK,
		QueryVector:      vectorData{Float32: embedding},
		ReturnMetadata:   true,
	}
	if userID != "" {
		reqBody.Filter = map[string]interface{}{
			"user_id": map[string]interface{}{
				"$eq": userID,
			},
		}
	}

	return c.doQueryVectors(ctx, reqBody)
}

// QueryVectorsPublic performs a similarity search for public-scoped vectors across all users.
func (c *S3VectorsClient) QueryVectorsPublic(ctx context.Context, embedding []float64, topK int) ([]*VectorResult, error) {
	reqBody := queryVectorsRequest{
		VectorBucketName: c.bucketName,
		IndexName:        c.indexName,
		TopK:             topK,
		QueryVector:      vectorData{Float32: embedding},
		ReturnMetadata:   true,
		Filter: map[string]interface{}{
			"scope": map[string]interface{}{
				"$eq": string(ScopePublic),
			},
		},
	}

	return c.doQueryVectors(ctx, reqBody)
}

// doQueryVectors executes a query request and parses the response.
func (c *S3VectorsClient) doQueryVectors(ctx context.Context, reqBody queryVectorsRequest) ([]*VectorResult, error) {
	var resp queryVectorsResponse
	if err := c.doRequest(ctx, "/QueryVectors", reqBody, &resp); err != nil {
		return nil, err
	}

	results := make([]*VectorResult, 0, len(resp.Vectors))
	for _, v := range resp.Vectors {
		// Convert distance to score: for cosine distance, lower is more similar.
		// Use 1 - distance as similarity score (score=1 means identical).
		score := 1.0 - v.Distance

		metadata := make(map[string]string)
		for k, val := range v.Metadata {
			if s, ok := val.(string); ok {
				metadata[k] = s
			}
		}
		results = append(results, &VectorResult{
			Key:      v.Key,
			Score:    score,
			Metadata: metadata,
		})
	}
	return results, nil
}

// QueryVectorsWithTag performs a similarity search filtered by a specific tag value in metadata.
// Note: S3 Vectors metadata filter uses user_id; tags are stored in DynamoDB and filtered after retrieval.
// This function queries with userID filter and topK, tag filtering is post-processed from DynamoDB.
func (c *S3VectorsClient) QueryVectorsWithTag(ctx context.Context, embedding []float64, topK int, userID string, _ string) ([]*VectorResult, error) {
	return c.QueryVectors(ctx, embedding, topK, userID)
}

// DeleteVectors removes vectors from S3 Vectors.
func (c *S3VectorsClient) DeleteVectors(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	reqBody := deleteVectorsRequest{
		VectorBucketName: c.bucketName,
		IndexName:        c.indexName,
		Keys:             keys,
	}
	return c.doRequest(ctx, "/DeleteVectors", reqBody, nil)
}

// doRequest performs a SigV4-signed HTTP POST request to S3 Vectors API.
func (c *S3VectorsClient) doRequest(ctx context.Context, operation string, body interface{}, out interface{}) error {
	var bodyReader io.Reader
	var bodyBytes []byte
	var err error

	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	url := c.endpoint + operation
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Compute payload hash
	payloadHash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" // empty
	if len(bodyBytes) > 0 {
		h := sha256.Sum256(bodyBytes)
		payloadHash = hex.EncodeToString(h[:])
	}

	// Get credentials
	creds, err := c.cfg.Credentials.Retrieve(ctx)
	if err != nil {
		return fmt.Errorf("retrieve credentials: %w", err)
	}

	// Sign the request
	if err := c.signer.SignHTTP(ctx, creds, req, payloadHash, "s3vectors", c.region, time.Now()); err != nil {
		return fmt.Errorf("sign request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("s3vectors API error (status %d): %s", resp.StatusCode, string(respBytes))
	}

	if out != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, out); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return nil
}
