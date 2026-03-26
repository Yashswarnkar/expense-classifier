package export

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Yashswarnkar/expense-classifier/internal/models"
)

// WriteJSON writes transactions to an indented JSON file at outPath.
func WriteJSON(txns []*models.Transaction, outPath string) error {
	return writeJSON(txns, outPath)
}

// WriteSummaryJSON writes category summaries to a JSON file.
func WriteSummaryJSON(summaries []*models.Summary, outPath string) error {
	return writeJSON(summaries, outPath)
}

func writeJSON(v interface{}, outPath string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return fmt.Errorf("json: mkdir: %w", err)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("json: create %q: %w", outPath, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("json: encode: %w", err)
	}
	return nil
}
