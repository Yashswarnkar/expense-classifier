// Package ollama contains all Ollama-specific implementation details.
// Prompt templates and model configuration are isolated here so they can
// be swapped or tuned without touching business logic.
package ollama

import (
	"bytes"
	"strings"
	"text/template"

	"github.com/Yashswarnkar/expense-classifier/internal/categories"
	"github.com/Yashswarnkar/expense-classifier/internal/models"
)

// PromptData is the data bag fed into the prompt template.
type PromptData struct {
	Description string
	Amount      float64
	Type        string // "debit" or "credit"
	Categories  []categories.Category
}

// ClassificationPromptTmpl is the prompt template sent to the LLM.
// It is defined as a package-level variable so it can be overridden in
// tests or by advanced users who embed this library.
var ClassificationPromptTmpl = template.Must(template.New("classify").Funcs(template.FuncMap{
	"join": strings.Join,
	"kwList": func(cat categories.Category) string {
		if len(cat.Keywords) == 0 {
			return ""
		}
		return " (hints: " + strings.Join(cat.Keywords, ", ") + ")"
	},
}).Parse(`You are a financial transaction classifier for Indian bank statements.

Available categories:
{{- range .Categories}}
- {{.Name}}{{kwList .}}
{{- end}}

Transaction details:
  Description : {{.Description}}
  Amount (₹)  : {{printf "%.2f" .Amount}}
  Direction   : {{.Type}}

Rules:
1. Reply with ONLY the exact category name from the list above.
2. Do NOT include any explanation, punctuation, or extra words.
3. If the transaction does not clearly fit any category, reply: Uncategorized

Category:`))

// BuildPrompt renders the classification prompt for the given transaction
// and category list.
func BuildPrompt(txn *models.Transaction, cats []categories.Category) (string, error) {
	data := PromptData{
		Description: txn.Description,
		Amount:      txn.Amount,
		Type:        string(txn.Type),
		Categories:  cats,
	}
	var buf bytes.Buffer
	if err := ClassificationPromptTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ModelConfig holds Ollama-specific model settings, kept separate from the
// HTTP client so they can be versioned or swapped independently.
type ModelConfig struct {
	// Model is the Ollama model tag, e.g. "mistral", "llama3", "gemma3".
	Model string
	// Temperature controls creativity; lower = more deterministic.
	// For classification tasks 0 is ideal.
	Temperature float64
	// NumPredict caps the response length in tokens.
	// Category names are short so 20 is generous.
	NumPredict int
}

// DefaultModelConfig returns production-ready defaults for classification.
func DefaultModelConfig(modelName string) ModelConfig {
	return ModelConfig{
		Model:       modelName,
		Temperature: 0,
		NumPredict:  20,
	}
}
