package ollama

import (
	"context"
	"strings"
	"time"

	"github.com/Yashswarnkar/expense-classifier/internal/categories"
	"github.com/Yashswarnkar/expense-classifier/internal/models"
)

// Classifier implements classifier.Classifier using a local Ollama model.
type Classifier struct {
	client *Client
	cfg    ModelConfig
}

// NewClassifier wires up the HTTP client and model configuration.
func NewClassifier(baseURL string, timeout time.Duration, modelCfg ModelConfig) *Classifier {
	return &Classifier{
		client: NewClient(baseURL, timeout),
		cfg:    modelCfg,
	}
}

func (c *Classifier) ModelName() string { return c.cfg.Model }

// Classify sends the transaction to Ollama and matches the response to one
// of the known category names.  It always returns a usable string — on any
// error or ambiguous response it falls back to "Uncategorized".
func (c *Classifier) Classify(ctx context.Context, txn *models.Transaction, cats []categories.Category) (string, error) {
	prompt, err := BuildPrompt(txn, cats)
	if err != nil {
		return "Uncategorized", err
	}

	raw, err := c.client.Generate(ctx, c.cfg, prompt)
	if err != nil {
		return "Uncategorized", err
	}

	return matchCategory(raw, cats), nil
}

// matchCategory finds the closest valid category for the LLM's response.
// Matching is case-insensitive and trims whitespace / punctuation.
func matchCategory(response string, cats []categories.Category) string {
	candidate := strings.TrimSpace(response)
	candidate = strings.Trim(candidate, ".,!:\"'")

	// Exact match (case-insensitive).
	for _, c := range cats {
		if strings.EqualFold(c.Name, candidate) {
			return c.Name
		}
	}

	// Substring match — the model sometimes includes extra words.
	lower := strings.ToLower(candidate)
	for _, c := range cats {
		if strings.Contains(lower, strings.ToLower(c.Name)) {
			return c.Name
		}
	}

	return "Uncategorized"
}

// Ping delegates to the HTTP client's health check.
func (c *Classifier) Ping(ctx context.Context) error {
	return c.client.Ping(ctx, c.cfg.Model)
}
