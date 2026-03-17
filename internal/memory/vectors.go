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

	endpoint := fmt.Sprintf("https://s3vectors.%s.amazonaws.com", region)

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

// putVectorsRequest is the request body for PutVectors.
type putVectorsRequest struct {
	Vectors []vectorItem `json:"vectors"`
}

type vectorItem struct {
	Key      string            `json:"key"`
	Data     vectorData        `json:"data"`
	Metadata map[string]string `json:"metadata"`
}

type vectorData struct {
	Float32 []float64 `json:"float32"`
}

// queryVectorsRequest is the request body for QueryVectors.
type queryVectorsRequest struct {
	QueryVector vectorData         `json:"queryVector"`
	TopK        int                `json:"topK"`
	Filter      map[string]interface{} `json:"filter,omitempty"`
}

// queryVectorsResponse is the response body for QueryVectors.
type queryVectorsResponse struct {
	Vectors []queryVectorItem `json:"vectors"`
}

type queryVectorItem struct {
	Key      string            `json:"key"`
	Score    float64           `json:"score"`
	Metadata map[string]string `json:"metadata"`
}

// deleteVectorsRequest is the request body for DeleteVectors.
type deleteVectorsRequest struct {
	Keys []string `json:"keys"`
}

// PutVectors stores a vector in S3 Vectors.
func (c *S3VectorsClient) PutVectors(ctx context.Context, key string, embedding []float64, userID string) error {
	reqBody := putVectorsRequest{
		Vectors: []vectorItem{
			{
				Key:  key,
				Data: vectorData{Float32: embedding},
				Metadata: map[string]string{
					"user_id": userID,
				},
			},
		},
	}

	path := fmt.Sprintf("/vector-buckets/%s/vector-indexes/%s/vectors", c.bucketName, c.indexName)
	return c.doRequest(ctx, http.MethodPut, path, reqBody, nil)
}

// QueryVectors performs a similarity search in S3 Vectors.
func (c *S3VectorsClient) QueryVectors(ctx context.Context, embedding []float64, topK int, userID string) ([]*VectorResult, error) {
	reqBody := queryVectorsRequest{
		QueryVector: vectorData{Float32: embedding},
		TopK:        topK,
	}
	if userID != "" {
		reqBody.Filter = map[string]interface{}{
			"user_id": map[string]interface{}{
				"$eq": userID,
			},
		}
	}

	var resp queryVectorsResponse
	path := fmt.Sprintf("/vector-buckets/%s/vector-indexes/%s/vectors/query", c.bucketName, c.indexName)
	if err := c.doRequest(ctx, http.MethodPost, path, reqBody, &resp); err != nil {
		return nil, err
	}

	results := make([]*VectorResult, 0, len(resp.Vectors))
	for _, v := range resp.Vectors {
		results = append(results, &VectorResult{
			Key:      v.Key,
			Score:    v.Score,
			Metadata: v.Metadata,
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
	reqBody := deleteVectorsRequest{Keys: keys}
	path := fmt.Sprintf("/vector-buckets/%s/vector-indexes/%s/vectors", c.bucketName, c.indexName)
	return c.doRequest(ctx, http.MethodDelete, path, reqBody, nil)
}

// doRequest performs a SigV4-signed HTTP request to S3 Vectors API.
func (c *S3VectorsClient) doRequest(ctx context.Context, method, path string, body interface{}, out interface{}) error {
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

	url := c.endpoint + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
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
