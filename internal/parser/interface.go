package parser

import "github.com/Yashswarnkar/expense-classifier/internal/models"

// Parser is the contract every statement parser must satisfy.
// Adding a new bank is as simple as implementing this interface and
// registering it in the factory (cmd/process.go).
type Parser interface {
	// Parse extracts transactions from the given PDF.
	// password may be empty for unprotected files.
	Parse(filePath, password string) ([]*models.Transaction, error)

	// Name returns a human-readable identifier, e.g. "AU Bank Account Statement".
	Name() string

	// Source returns the canonical SourceType this parser produces.
	Source() models.SourceType
}
