// Package amazonpay parses Amazon Pay ICICI Bank credit card statement PDFs.
//
// Column order (left-to-right):
//   Date | SerNo. | Transaction Details | Reward Points | Intl.# amount | Amount (in₹)
//
// Date format: "02/01/2006" (DD/MM/YYYY, no time component).
// Amounts: "726.00" = debit, "114.00 CR" = credit/refund.
// Reward point entries are just a numeric column on the same row — they are
// NOT separate line items, so nothing needs to be skipped here.
// A card-number header row ("4315XXXXXXXX3015") appears before the first
// transaction and is ignored.
package amazonpay

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
	// DD/MM/YYYY
	dateRe = regexp.MustCompile(`^\d{2}/\d{2}/\d{4}$`)
	// Serial number: all digits, 10-12 chars
	serNoRe = regexp.MustCompile(`^\d{10,13}$`)
	// Card number masked row e.g. "4315XXXXXXXX3015"
	cardNoRe = regexp.MustCompile(`^\d{4}[Xx*]{4,}`)

	dateLayout = "02/01/2006"
)

// Parser implements parser.Parser for Amazon Pay ICICI credit card statements.
type Parser struct{}

func New() *Parser { return &Parser{} }

func (p *Parser) Name() string              { return "Amazon Pay ICICI Credit Card Statement" }
func (p *Parser) Source() models.SourceType { return models.SourceAmazonPayCC }

func (p *Parser) Parse(filePath, password string) ([]*models.Transaction, error) {
	pages, err := pdfread.DecryptAndExtract(filePath, password)
	if err != nil {
		return nil, fmt.Errorf("amazonpay: extract text: %w", err)
	}

	all := pdfread.AllElements(pages)
	rows := pdfread.GroupByRows(all, rowTol)

	return parseTransactions(rows)
}

func parseTransactions(rows [][]pdfread.TextElement) ([]*models.Transaction, error) {
	var txns []*models.Transaction

	// We process rows only within the transaction table section.
	// Entry: row containing "Transaction Details" header.
	// Exit: blank-ish row or known section headers.
	inTable := false

	stopKeywords := []string{
		"important messages", "safe banking", "earnings", "standing instruction",
		"for any query", "invoice no",
	}

	for _, row := range rows {
		text := strings.ToLower(pdfread.RowText(row))

		// Detect table start.
		if strings.Contains(text, "transaction details") && strings.Contains(text, "serNo") ||
			(strings.Contains(text, "transaction details") && strings.Contains(text, "amount")) {
			inTable = true
			continue
		}

		if inTable {
			// Detect table end.
			for _, stop := range stopKeywords {
				if strings.Contains(text, stop) {
					inTable = false
					break
				}
			}
		}
		if !inTable {
			continue
		}

		// Skip card-number header rows and the column header row itself.
		rawText := pdfread.RowText(row)
		if cardNoRe.MatchString(strings.TrimSpace(rawText)) {
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

// parseRow extracts a single transaction from a table row.
func parseRow(row []pdfread.TextElement) (*models.Transaction, bool, error) {
	if len(row) == 0 {
		return nil, true, nil
	}

	// First cell must be a date.
	firstCell := strings.TrimSpace(row[0].Content)
	if !dateRe.MatchString(firstCell) {
		return nil, true, nil
	}

	txnDate, err := time.Parse(dateLayout, firstCell)
	if err != nil {
		return nil, true, nil
	}

	// Collect all cell texts in order.
	var cells []string
	for _, el := range row {
		c := strings.TrimSpace(el.Content)
		if c != "" {
			cells = append(cells, c)
		}
	}

	// Expected minimum: Date, SerNo, Description, Amount
	// The reward points and Intl.# columns may be absent if empty.
	if len(cells) < 3 {
		return nil, true, nil
	}

	// cells[0] = date (already parsed)
	// cells[1] = serial number (skip if matches serNoRe, else it merged with description)
	idx := 1
	if serNoRe.MatchString(cells[idx]) {
		idx++ // skip SerNo
	}

	// Everything between SerNo and the last numeric token is the description.
	// The last cell that looks like an amount (optionally suffixed with CR) is the amount.
	amountCell, amountIdx := findAmountCell(cells)
	if amountIdx < 0 {
		return nil, true, nil
	}

	// Description spans cells[idx .. amountIdx-1]
	descParts := cells[idx:amountIdx]
	// Drop trailing reward-point tokens (pure integers) from the description.
	for len(descParts) > 0 && isRewardPointToken(descParts[len(descParts)-1]) {
		descParts = descParts[:len(descParts)-1]
	}
	desc := strings.TrimSpace(strings.Join(descParts, " "))
	if desc == "" {
		return nil, true, nil
	}

	amount, isCredit := parseAmount(amountCell)
	if amount == 0 {
		return nil, true, nil
	}

	txnType := models.TxnDebit
	if isCredit {
		txnType = models.TxnCredit
	}

	txn := &models.Transaction{
		ID:              uuid.New().String(),
		Source:          models.SourceAmazonPayCC,
		TransactionDate: txnDate,
		Description:     desc,
		Amount:          amount,
		Type:            txnType,
		Category:        "Uncategorized",
		CreatedAt:       time.Now(),
	}
	txn.Hash = txn.ComputeHash()

	return txn, false, nil
}

// findAmountCell locates the rightmost cell that looks like a monetary amount.
// Returns the cell string and its index in cells.
func findAmountCell(cells []string) (string, int) {
	for i := len(cells) - 1; i >= 0; i-- {
		c := cells[i]
		// Strip CR suffix and ₹ for testing.
		stripped := strings.TrimSuffix(strings.TrimSpace(c), " CR")
		stripped = strings.TrimSuffix(stripped, "CR")
		stripped = strings.ReplaceAll(stripped, "₹", "")
		stripped = strings.ReplaceAll(stripped, ",", "")
		stripped = strings.TrimSpace(stripped)
		if _, err := strconv.ParseFloat(stripped, 64); err == nil {
			return c, i
		}
	}
	return "", -1
}

// parseAmount parses "726.00" → (726.0, false) and "114.00 CR" → (114.0, true).
func parseAmount(s string) (float64, bool) {
	isCredit := strings.Contains(strings.ToUpper(s), "CR")
	s = strings.ToUpper(s)
	s = strings.ReplaceAll(s, "CR", "")
	s = strings.ReplaceAll(s, "₹", "")
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSpace(s)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, isCredit
}

// isRewardPointToken returns true for small plain integers that look like
// reward point values (no decimal, no CR).
func isRewardPointToken(s string) bool {
	if strings.Contains(s, ".") || strings.Contains(strings.ToUpper(s), "CR") {
		return false
	}
	_, err := strconv.Atoi(strings.TrimSpace(s))
	return err == nil
}
