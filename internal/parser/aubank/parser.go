// Package aubank parses AU Small Finance Bank account statement PDFs.
//
// AU Bank PDFs use an RTL-encoded font where every character is a separate
// text element with X positions running right-to-left within each word.
// Sorting by X (the normal approach) scrambles text. Instead we use the
// natural PDF content stream order which preserves correct reading order,
// then apply ReconstructText to restore word boundaries.
//
// Expected columns (left-to-right):
//
//	Transaction Date | Value Date | Description/Narration |
//	Cheque/Reference No. | Debit (₹) | Credit (₹) | Balance (₹)
//
// Date format: "02 Jan 2006"   Amounts: Indian format "1,25,912.58"
package aubank

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
	// "01 Dec 2025" — spaces may be missing after reconstruction so we allow \s*
	dateRe = regexp.MustCompile(`\b(\d{2}\s*[A-Za-z]{3}\s*\d{4})\b`)

	// Rightmost amount in a row: digits/commas then dot then 2 digits, optionally preceded by -
	amountRe = regexp.MustCompile(`([\d,]+\.\d{2})`)

	// Reference numbers: long alphanumeric tokens (≥10 chars, no spaces)
	refRe = regexp.MustCompile(`\b([A-Za-z0-9]{10,})\b`)

	dateLayout = "02 Jan 2006"

	// Stop words that appear in non-transaction rows (page headers/footers)
	stopWords = []string{
		"account statement", "statement date", "opening balance",
		"closing balance", "page", "this is an auto", "call us at",
	}
)

// Parser implements parser.Parser for AU Bank account statements.
type Parser struct{}

func New() *Parser { return &Parser{} }

func (p *Parser) Name() string              { return "AU Bank Account Statement" }
func (p *Parser) Source() models.SourceType { return models.SourceAUBank }

func (p *Parser) Parse(filePath, password string) ([]*models.Transaction, error) {
	pages, err := pdfread.DecryptAndExtract(filePath, password)
	if err != nil {
		return nil, fmt.Errorf("aubank: extract text: %w", err)
	}

	var txns []*models.Transaction
	for _, page := range pages {
		// Use natural stream order — critical for AU Bank's RTL font encoding.
		rows := pdfread.GroupByRowsNaturalOrder(page.Elements, rowTol)
		pageTxns := parsePageRows(rows)
		txns = append(txns, pageTxns...)
	}
	return txns, nil
}

// parsePageRows converts reconstructed text rows into transactions.
// A row that contains two dates and at least one amount is a transaction row.
// Rows without dates are description continuations of the previous transaction.
func parsePageRows(rows [][]pdfread.TextElement) []*models.Transaction {
	var txns []*models.Transaction
	var current *models.Transaction

	finalize := func() {
		if current != nil {
			current.Description = cleanDescription(current.Description)
			current.Hash = current.ComputeHash()
			txns = append(txns, current)
			current = nil
		}
	}

	for _, row := range rows {
		text := pdfread.ReconstructText(row)

		if isNonTransactionRow(text) {
			continue
		}

		dates := extractDates(text)
		if len(dates) >= 1 {
			// Looks like a transaction row.
			txn := buildTransaction(text, dates)
			if txn != nil {
				finalize()
				current = txn
				continue
			}
		}

		// Continuation: append to current transaction description.
		if current != nil {
			trimmed := strings.TrimSpace(text)
			if trimmed != "" && !looksLikePageHeader(trimmed) {
				current.Description += " " + trimmed
			}
		}
	}
	finalize()
	return txns
}

// buildTransaction extracts fields from a reconstructed row string.
//
// Row shape (after ReconstructText):
//
//	"01 Dec 2025 01 Dec 2025 UPI/DR/.../DESCRIPTION RefNumber 45.00 - 1,25,912.58"
//
// Strategy: anchor on dates (left) and amounts (right), everything in between
// is description + reference.
func buildTransaction(text string, dates []time.Time) *models.Transaction {
	// We need at least one amount (debit or credit) and a balance.
	amounts := extractAmounts(text)
	if len(amounts) < 2 {
		return nil
	}

	txnDate := dates[0]
	var valueDate *time.Time
	if len(dates) >= 2 {
		d := dates[1]
		valueDate = &d
	}

	// Balance is the last amount. Debit/credit are the two before it.
	balance := amounts[len(amounts)-1]
	var debit, credit float64
	if len(amounts) >= 3 {
		// Check which of the two preceding amounts is non-zero.
		a1 := amounts[len(amounts)-3]
		a2 := amounts[len(amounts)-2]
		if a1 > 0 {
			debit = a1
		}
		if a2 > 0 {
			credit = a2
		}
	} else {
		// Only balance found — likely a continuation row; skip.
		return nil
	}

	amount := debit
	txnType := models.TxnDebit
	if credit > 0 && debit == 0 {
		amount = credit
		txnType = models.TxnCredit
	}

	if amount == 0 {
		return nil
	}

	// Description: everything between the last date match and the first amount.
	desc := extractDescription(text)

	return &models.Transaction{
		ID:              uuid.New().String(),
		Source:          models.SourceAUBank,
		TransactionDate: txnDate,
		ValueDate:       valueDate,
		Description:     desc,
		ReferenceNo:     extractReference(text),
		Amount:          amount,
		Type:            txnType,
		Balance:         balance,
		Category:        "Uncategorized",
		CreatedAt:       time.Now(),
	}
}

// extractDates finds all "DD Mon YYYY" patterns in text and parses them.
func extractDates(text string) []time.Time {
	matches := dateRe.FindAllString(text, -1)
	var dates []time.Time
	for _, m := range matches {
		// Normalise: collapse any internal whitespace
		m = strings.Join(strings.Fields(m), " ")
		t, err := time.Parse(dateLayout, m)
		if err == nil {
			dates = append(dates, t)
		}
	}
	return dates
}

// extractAmounts finds all numeric amounts in the row (right side of the row).
func extractAmounts(text string) []float64 {
	matches := amountRe.FindAllString(text, -1)
	var amounts []float64
	for _, m := range matches {
		v, err := parseIndianAmount(m)
		if err == nil && v > 0 {
			amounts = append(amounts, v)
		}
	}
	return amounts
}

// extractDescription pulls the middle portion of the row as description text.
// It strips leading dates and trailing amounts/reference numbers.
func extractDescription(text string) string {
	// Remove all date occurrences from the left.
	result := dateRe.ReplaceAllString(text, " ")
	// Remove amounts (numbers with commas and decimals).
	result = amountRe.ReplaceAllString(result, " ")
	// Remove standalone "-" that represent empty debit/credit cells.
	result = regexp.MustCompile(`\s+-\s+`).ReplaceAllString(result, " ")
	// Remove long reference-looking tokens (ICIxxxxxx, alphanumeric ≥15 chars).
	result = regexp.MustCompile(`\b[A-Za-z0-9]{15,}\b`).ReplaceAllString(result, " ")
	return cleanDescription(result)
}

// extractReference tries to find the cheque/reference number — a long
// alphanumeric token that isn't part of UPI strings.
func extractReference(text string) string {
	// Look for tokens that look like ICICI reference numbers: start with IC or
	// are purely hex-like, length 20-40.
	refPattern := regexp.MustCompile(`\b(IC[A-Za-z0-9]{18,}|[0-9]{10,15})\b`)
	m := refPattern.FindString(text)
	return m
}

func cleanDescription(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSpace(s)
	return s
}

func isNonTransactionRow(text string) bool {
	lower := strings.ToLower(text)
	for _, w := range stopWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

func looksLikePageHeader(text string) bool {
	lower := strings.ToLower(text)
	return strings.Contains(lower, "page") ||
		strings.Contains(lower, "account") ||
		strings.Contains(lower, "narration")
}

// parseIndianAmount handles "1,25,912.58" and plain "45.00".
func parseIndianAmount(s string) (float64, error) {
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "₹", "")
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}
