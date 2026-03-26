package models

import (
	"crypto/sha256"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
)

// SourceType identifies which bank/card the transaction came from.
type SourceType string

const (
	SourceAUBank       SourceType = "au_bank"
	SourceHDFCCC       SourceType = "hdfc_cc"
	SourceAmazonPayCC  SourceType = "amazon_pay_cc"
)

// TxnType indicates whether money went out or came in.
type TxnType string

const (
	TxnDebit  TxnType = "debit"
	TxnCredit TxnType = "credit"
)

// Transaction is the canonical representation of a single financial transaction
// regardless of which bank or card it came from.
type Transaction struct {
	ID              string     `json:"id"               db:"id"`
	Source          SourceType `json:"source"           db:"source"`
	TransactionDate time.Time  `json:"transaction_date" db:"transaction_date"`
	ValueDate       *time.Time `json:"value_date,omitempty" db:"value_date"`
	Description     string     `json:"description"      db:"description"`
	ReferenceNo     string     `json:"reference_no,omitempty" db:"reference_no"`
	// Amount is always positive; use Type to determine direction.
	Amount   float64 `json:"amount"    db:"amount"`
	Type     TxnType `json:"type"      db:"type"`
	Balance  float64 `json:"balance,omitempty" db:"balance"`
	Category string  `json:"category"  db:"category"`
	// Hash is used for deduplication — see ComputeHash.
	Hash      string    `json:"hash"       db:"hash"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

var multiSpace = regexp.MustCompile(`\s+`)

// normalizeDescription produces a stable, lowercase, whitespace-collapsed string
// suitable for hashing.
func normalizeDescription(s string) string {
	s = strings.ToLower(s)
	s = multiSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// ComputeHash derives a short deduplication hash from the transaction's date,
// amount (in paise), and normalised description.  Source is intentionally
// excluded so that the same real-world payment appearing in both a bank
// statement and a credit card statement is treated as one record.
func (t *Transaction) ComputeHash() string {
	amountPaise := int64(math.Round(t.Amount * 100))
	raw := fmt.Sprintf("%s|%d|%s",
		t.TransactionDate.Format("2006-01-02"),
		amountPaise,
		normalizeDescription(t.Description),
	)
	h := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", h[:8]) // 16 hex chars — plenty for dedup
}

// Summary is an aggregated spending view per category, useful for reports
// and the future UI layer.
type Summary struct {
	Category    string  `json:"category"`
	TotalDebit  float64 `json:"total_debit"`
	TotalCredit float64 `json:"total_credit"`
	NetSpend    float64 `json:"net_spend"` // TotalDebit - TotalCredit
	Count       int     `json:"count"`
}
