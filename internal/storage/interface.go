package storage

import (
	"context"
	"time"

	"github.com/Yashswarnkar/expense-classifier/internal/models"
)

// Store is the persistence interface.  Both the SQLite implementation and any
// future mock/in-memory test store implement this.
type Store interface {
	// Save persists a batch of transactions.  Duplicates (same hash) are
	// silently ignored — they will not cause an error.
	Save(ctx context.Context, txns []*models.Transaction) error

	// UpdateCategory changes the category of a single transaction by ID.
	UpdateCategory(ctx context.Context, id, category string) error

	// GetAll returns all transactions, newest first.
	GetAll(ctx context.Context) ([]*models.Transaction, error)

	// GetBySource returns transactions filtered by source.
	GetBySource(ctx context.Context, source models.SourceType) ([]*models.Transaction, error)

	// GetByDateRange returns transactions in [from, to] inclusive.
	GetByDateRange(ctx context.Context, from, to time.Time) ([]*models.Transaction, error)

	// ExistingHashes returns all hash values currently in the store.
	// Used to seed the Deduplicator before a processing run.
	ExistingHashes(ctx context.Context) ([]string, error)

	// Summary returns per-category aggregates.
	Summary(ctx context.Context) ([]*models.Summary, error)

	// Close releases the underlying connection.
	Close() error
}
