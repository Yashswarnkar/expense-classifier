// Package hdfc parses HDFC Diners / credit card statement PDFs.
//
// Relevant sections detected:
//   "Domestic Transactions"
//   "International Transactions"
//
// Column order (left-to-right):
//   DATE & TIME | TRANSACTION DESCRIPTION | REWARDS | AMOUNT | PI
//
// Date format: "02/01/2006 | 15:04"  (DD/MM/YYYY | HH:MM)
// Amounts: "₹ X.XX" (debit) — rows where the amount cell starts with "+"
// are reward/cashback credits and are SKIPPED.
package hdfc

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/Yashswarnkar/expense-classifier/internal/models"
	pdfread "github.com/Yashswarnkar/expense-classifier/internal/pdf"
)

const rowTol = 4.0

var (
	// e.g. "13/03/2026 | 00:00"  or  "13/03/2026| 00:00"
	dateTimeRe = regexp.MustCompile(`(\d{2}/\d{2}/\d{4})\s*\|\s*(\d{2}:\d{2})`)
	dateLayout = "02/01/2006 15:04"
)

// Parser implements parser.Parser for HDFC credit card statements.
type Parser struct{}

func New() *Parser { return &Parser{} }

func (p *Parser) Name() string              { return "HDFC Credit Card Statement" }
func (p *Parser) Source() models.SourceType { return models.SourceHDFCCC }

func (p *Parser) Parse(filePath, password string) ([]*models.Transaction, error) {
	pages, err := pdfread.DecryptAndExtract(filePath, password)
	if err != nil {
		return nil, fmt.Errorf("hdfc: extract text: %w", err)
	}

	all := pdfread.AllElements(pages)
	rows := pdfread.GroupByRows(all, rowTol)

	return parseTransactions(rows)
}

func parseTransactions(rows [][]pdfread.TextElement) ([]*models.Transaction, error) {
	var txns []*models.Transaction

	// We only process rows that belong to transaction table sections.
	// We enter a section when we see a row containing "Domestic Transactions"
	// or "International Transactions" and stop when we see an unrelated section
	// header (e.g., "Rewards Program", "Important Information").
	inSection := false

	stopKeywords := []string{
		"rewards program", "important information", "gst entry",
		"minimum amount", "available credit", "opening balance",
	}

	for _, row := range rows {
		text := strings.ToLower(pdfread.RowText(row))

		// Section boundary detection.
		if strings.Contains(text, "domestic transactions") ||
			strings.Contains(text, "international transactions") {
			inSection = true
			continue
		}
		if inSection {
			for _, stop := range stopKeywords {
				if strings.Contains(text, stop) {
					inSection = false
					break
				}
			}
		}
		if !inSection {
			continue
		}

		// Skip the column header row.
		if strings.Contains(text, "date & time") || strings.Contains(text, "transaction description") {
			continue
		}

		txn, skip, err := parseRow(row)
		if err != nil || skip {
			continue
		}
		txns = append(txns, txn)
	}

	return txns, nil
}

// parseRow attempts to extract a transaction from a single row.
// skip=true means the row should be silently ignored (e.g. rewards credit).
func parseRow(row []pdfread.TextElement) (*models.Transaction, bool, error) {
	full := pdfread.RowText(row)

	// Extract date+time.
	m := dateTimeRe.FindStringSubmatch(full)
	if m == nil {
		return nil, true, nil // not a transaction row
	}
	txnTime, err := time.Parse(dateLayout, m[1]+" "+m[2])
	if err != nil {
		return nil, true, nil
	}

	// Everything after the date is description + amount.
	afterDate := strings.TrimSpace(full[len(m[0]):])

	// Extract and validate amount — it's the last token(s).
	amount, isCredit, err := extractAmount(row)
	if err != nil {
		return nil, true, nil
	}

	// Skip reward/cashback credits (rows where amount is a credit entry
	// produced by the rewards program).
	if isCredit {
		return nil, true, nil
	}

	// Description is everything that isn't the date/time or amount.
	desc := cleanDescription(afterDate, amount)
	if desc == "" {
		return nil, true, nil
	}

	txn := &models.Transaction{
		ID:              uuid.New().String(),
		Source:          models.SourceHDFCCC,
		TransactionDate: txnTime,
		Description:     desc,
		Amount:          amount,
		Type:            models.TxnDebit,
		Category:        "Uncategorized",
		CreatedAt:       time.Now(),
	}
	txn.Hash = txn.ComputeHash()

	return txn, false, nil
}

// extractAmount finds the amount cell — rightmost "₹ X.XX" in the row.
// Returns (amount, isCredit, error).
// isCredit=true means the row starts with "+" which indicates a reward/refund.
func extractAmount(row []pdfread.TextElement) (float64, bool, error) {
	// Amount column is the rightmost element(s).
	// Walk right-to-left looking for ₹ symbol.
	for i := len(row) - 1; i >= 0; i-- {
		cell := strings.TrimSpace(row[i].Content)

		// Detect "+" prefix = credit/rewards — skip.
		isCredit := strings.HasPrefix(cell, "+")
		cell = strings.TrimPrefix(cell, "+")
		cell = strings.ReplaceAll(cell, "₹", "")
		cell = strings.ReplaceAll(cell, "Rs.", "")
		cell = strings.ReplaceAll(cell, ",", "")
		cell = strings.TrimSpace(cell)

		if v, err := strconv.ParseFloat(cell, 64); err == nil && v > 0 {
			return v, isCredit, nil
		}
	}
	return 0, false, fmt.Errorf("no amount found")
}

// cleanDescription strips the amount string from the tail of the afterDate
// text to leave just the merchant name.
func cleanDescription(afterDate string, amount float64) string {
	// Remove ₹ and numeric-looking suffix.
	amtStr := fmt.Sprintf("%.2f", amount)
	// Try to strip the amount from the end.
	idx := strings.LastIndex(afterDate, amtStr)
	if idx != -1 {
		afterDate = strings.TrimSpace(afterDate[:idx])
	}
	afterDate = strings.TrimRight(afterDate, "₹+Rs., ")
	afterDate = strings.TrimSpace(afterDate)
	return afterDate
}
