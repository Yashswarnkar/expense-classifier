// Package deduplicator filters out transactions that have already been seen.
//
// Deduplication key: SHA-256 of (date + amount_paise + normalised_description).
// Source is excluded from the key so that the same real-world payment that
// appears in both a bank statement (as an outflow) and a credit card statement
// (as a charge) is recognised as one record.
package deduplicator

import "github.com/Yashswarnkar/expense-classifier/internal/models"

// Deduplicator tracks seen transaction hashes in memory and against an
// optional pre-loaded set of hashes from the database.
type Deduplicator struct {
	seen map[string]struct{}
}

// New creates a Deduplicator pre-seeded with hashes that already exist in
// persistent storage.  Pass nil or an empty slice if starting fresh.
func New(existingHashes []string) *Deduplicator {
	d := &Deduplicator{seen: make(map[string]struct{}, len(existingHashes))}
	for _, h := range existingHashes {
		d.seen[h] = struct{}{}
	}
	return d
}

// Filter returns only transactions whose hashes have not been seen before,
// and registers those hashes so subsequent calls to Filter won't re-add them.
func (d *Deduplicator) Filter(txns []*models.Transaction) []*models.Transaction {
	var unique []*models.Transaction
	for _, t := range txns {
		if t.Hash == "" {
			t.Hash = t.ComputeHash()
		}
		if _, exists := d.seen[t.Hash]; !exists {
			d.seen[t.Hash] = struct{}{}
			unique = append(unique, t)
		}
	}
	return unique
}

// IsDuplicate reports whether a single transaction is a duplicate.
func (d *Deduplicator) IsDuplicate(t *models.Transaction) bool {
	if t.Hash == "" {
		t.Hash = t.ComputeHash()
	}
	_, exists := d.seen[t.Hash]
	return exists
}
