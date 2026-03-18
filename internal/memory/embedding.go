package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

const (
	titanEmbedModelID = "amazon.titan-embed-text-v2:0"
	embeddingDims     = 1024 // Titan Embed Text v2 supports 256, 512, 1024 only (1536 is invalid)
)

// EmbeddingClient generates embeddings using Amazon Bedrock Titan.
type EmbeddingClient struct {
	client *bedrockruntime.Client
}

// NewEmbeddingClient creates a new EmbeddingClient.
func NewEmbeddingClient(cfg aws.Config) *EmbeddingClient {
	// Use BEDROCK_REGION if specified, otherwise use default region
	bedrockRegion := os.Getenv("BEDROCK_REGION")
	if bedrockRegion != "" && bedrockRegion != cfg.Region {
		bedrockCfg := cfg.Copy()
		bedrockCfg.Region = bedrockRegion
		return &EmbeddingClient{
			client: bedrockruntime.NewFromConfig(bedrockCfg),
		}
	}
	return &EmbeddingClient{
		client: bedrockruntime.NewFromConfig(cfg),
	}
}

type titanEmbedRequest struct {
	InputText  string `json:"inputText"`
	Dimensions int    `json:"dimensions"`
	Normalize  bool   `json:"normalize"`
}

type titanEmbedResponse struct {
	Embedding           []float64 `json:"embedding"`
	InputTextTokenCount int       `json:"inputTextTokenCount"`
}

// Generate generates an embedding vector for the given text.
func (e *EmbeddingClient) Generate(ctx context.Context, text string) ([]float64, error) {
	reqBody, err := json.Marshal(titanEmbedRequest{
		InputText:  text,
		Dimensions: embeddingDims,
		Normalize:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal embedding request: %w", err)
	}

	result, err := e.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(titanEmbedModelID),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        reqBody,
	})
	if err != nil {
		return nil, fmt.Errorf("invoke bedrock model: %w", err)
	}

	var resp titanEmbedResponse
	if err := json.Unmarshal(result.Body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal embedding response: %w", err)
	}

	return resp.Embedding, nil
}
