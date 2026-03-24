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
	// defaultSummarizeModelID is the Bedrock model used for text summarization.
	// Can be overridden via BEDROCK_SUMMARIZE_MODEL_ID environment variable.
	defaultSummarizeModelID = "anthropic.claude-3-haiku-20240307-v1:0"
)

// SummarizeClient generates text summaries using Amazon Bedrock.
type SummarizeClient struct {
	client  *bedrockruntime.Client
	modelID string
}

// NewSummarizeClient creates a new SummarizeClient.
func NewSummarizeClient(cfg aws.Config) *SummarizeClient {
	modelID := os.Getenv("BEDROCK_SUMMARIZE_MODEL_ID")
	if modelID == "" {
		modelID = defaultSummarizeModelID
	}

	bedrockRegion := os.Getenv("BEDROCK_REGION")
	if bedrockRegion != "" && bedrockRegion != cfg.Region {
		bedrockCfg := cfg.Copy()
		bedrockCfg.Region = bedrockRegion
		return &SummarizeClient{
			client:  bedrockruntime.NewFromConfig(bedrockCfg),
			modelID: modelID,
		}
	}
	return &SummarizeClient{
		client:  bedrockruntime.NewFromConfig(cfg),
		modelID: modelID,
	}
}

// claudeMessage represents a single message in the Claude API format.
type claudeMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// claudeRequest is the request body for Claude via Bedrock InvokeModel.
type claudeRequest struct {
	AnthropicVersion string          `json:"anthropic_version"`
	MaxTokens        int             `json:"max_tokens"`
	Messages         []claudeMessage `json:"messages"`
}

// claudeResponse is a partial response from Claude via Bedrock InvokeModel.
type claudeResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// Summarize generates a concise summary from a list of memory contents.
// The prompt instructs the model to merge and condense the given memories.
func (s *SummarizeClient) Summarize(ctx context.Context, memories []*Memory) (string, error) {
	if len(memories) == 0 {
		return "", fmt.Errorf("no memories to summarize")
	}

	// Build a numbered list of memory contents for the prompt.
	var memList string
	for i, m := range memories {
		memList += fmt.Sprintf("%d. %s\n", i+1, m.Content)
	}

	prompt := fmt.Sprintf(`以下のメモリエントリを1つの簡潔なメモリにまとめてください。
重複する情報は削除し、重要な情報だけを残してください。
出力はまとめられたメモリの内容のみ（説明文なし）で返してください。

メモリ一覧:
%s`, memList)

	reqBody, err := json.Marshal(claudeRequest{
		AnthropicVersion: "bedrock-2023-05-31",
		MaxTokens:        1024,
		Messages: []claudeMessage{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal summarize request: %w", err)
	}

	result, err := s.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(s.modelID),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        reqBody,
	})
	if err != nil {
		return "", fmt.Errorf("invoke bedrock summarize model: %w", err)
	}

	var resp claudeResponse
	if err := json.Unmarshal(result.Body, &resp); err != nil {
		return "", fmt.Errorf("unmarshal summarize response: %w", err)
	}
	if len(resp.Content) == 0 {
		return "", fmt.Errorf("empty summarize response")
	}

	return resp.Content[0].Text, nil
}

// SummarizeInput holds parameters for the Service.Summarize operation.
type SummarizeInput struct {
	// UserID is the owner of the memories to summarize.
	UserID string
	// MinCount is the minimum number of memories required before summarization runs.
	// Defaults to 3 if zero.
	MinCount int
	// DeleteOriginals controls whether the source memories are deleted after summarization.
	DeleteOriginals bool
}

// SummarizeResult holds the result of Service.Summarize.
type SummarizeResult struct {
	// SummarizedMemoryID is the ID of the newly created summary memory.
	SummarizedMemoryID string `json:"summarized_memory_id"`
	// MergedCount is the number of memories that were merged.
	MergedCount int `json:"merged_count"`
	// Summary is the generated summary text.
	Summary string `json:"summary"`
}
