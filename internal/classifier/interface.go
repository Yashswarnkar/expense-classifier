package classifier

import (
	"context"

	"github.com/Yashswarnkar/expense-classifier/internal/categories"
	"github.com/Yashswarnkar/expense-classifier/internal/models"
)

// Classifier assigns a category to a transaction.
// The interface is intentionally simple so it can be backed by:
//   - A local Ollama model (current implementation)
//   - Any remote LLM API
//   - A rule-based engine for testing
type Classifier interface {
	// Classify returns the most appropriate category name from cats for the
	// given transaction.  It must always return a non-empty string; the
	// fallback is "Uncategorized".
	Classify(ctx context.Context, txn *models.Transaction, cats []categories.Category) (string, error)

	// ModelName returns a human-readable identifier of the underlying model.
	ModelName() string
}
