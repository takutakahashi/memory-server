package memory

import (
	"math"
	"os"
	"strconv"
	"time"
)

const (
	defaultLambda = 0.01
	defaultMu     = 0.005
)

// Scorer computes final relevance scores for memories.
type Scorer struct {
	lambda float64 // decay factor for creation age
	mu     float64 // decay factor for last access age
}

// NewScorer creates a new Scorer with values from environment or defaults.
func NewScorer() *Scorer {
	lambda := getEnvFloat("DECAY_LAMBDA", defaultLambda)
	mu := getEnvFloat("DECAY_MU", defaultMu)
	return &Scorer{lambda: lambda, mu: mu}
}

// Score computes the final score for a memory given its similarity score and metadata.
//
// finalScore = similarityScore
//
//	* exp(-lambda * daysSinceCreated)
//	* log1p(accessCount)
//	* exp(-mu * daysSinceAccessed)
func (s *Scorer) Score(similarityScore float64, m *Memory) float64 {
	now := time.Now().UTC()

	daysSinceCreated := now.Sub(m.CreatedAt).Hours() / 24
	if daysSinceCreated < 0 {
		daysSinceCreated = 0
	}

	daysSinceAccessed := now.Sub(m.LastAccessedAt).Hours() / 24
	if daysSinceAccessed < 0 {
		daysSinceAccessed = 0
	}

	accessCount := float64(m.AccessCount)
	if accessCount < 0 {
		accessCount = 0
	}

	return similarityScore *
		math.Exp(-s.lambda*daysSinceCreated) *
		math.Log1p(accessCount+1) *
		math.Exp(-s.mu*daysSinceAccessed)
}

func getEnvFloat(key string, defaultVal float64) float64 {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return defaultVal
	}
	return f
}
