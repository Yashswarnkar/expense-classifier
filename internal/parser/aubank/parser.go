// Package aubank parses AU Small Finance Bank account statement PDFs.
//
// AU Bank PDFs use an RTL-encoded font where every character is a separate
// text element with X positions running right-to-left within each word.
// We use GroupByRowsNaturalOrder + ReconstructText to restore reading order.
//
// Each transaction is spread across several visual rows:
//
//	pre-row  : "UPI/DR/570161013279/BA  ICIa65a07aa510943"   (desc start + ref start)
//	main row : "01Dec2025 01Dec2025 NGALOREHARTICULTURE b98… 45.00 - 1,25,912.58"
//	cont rows: "FRUITSAND c", "VEGETABLES/YESB/…", "JAGATPURA"
//
// The main row always contains at least one date and two amounts.
// Pre-rows + description portion of main row + continuation rows form the
// full description.
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
	// Handles both "01Dec2025" (no spaces, from ReconstructText) and "01 Dec 2025"
	dateRe = regexp.MustCompile(`\b(\d{2}\s*[A-Za-z]{3}\s*\d{4})\b`)

	// Indian-format amounts: "45.00", "1,25,912.58"
	amountRe = regexp.MustCompile(`\b([\d,]+\.\d{2})\b`)

	// Standalone dash representing an empty debit/credit cell
	dashRe = regexp.MustCompile(`\s+-\s+`)

	stopWords = []string{
		"account statement", "statement date", "opening balance",
		"closing balance", "this is an auto", "call us at",
		"transaction date", "value date", "description/narration",
		"cheque/reference", "debit", "credit", "balance",
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
		rows := pdfread.GroupByRowsNaturalOrder(page.Elements, rowTol)
		txns = append(txns, parsePageRows(rows)...)
	}
	return txns, nil
}

// parsePageRows is the core parser.
//
// Pass 1: reconstruct text for every row.
// Pass 2: identify "main rows" — rows that contain ≥1 date AND ≥2 amounts.
// Pass 3: for each main row at index m, the transaction block spans rows
//
//	[prev_main+1 … next_main-2]
//
// The -2 is because (next_main-1) is always the pre-row of the next
// transaction (containing the start of the next description), which belongs
// to the next transaction, not the current one.
func parsePageRows(rows [][]pdfread.TextElement) []*models.Transaction {
	texts := make([]string, len(rows))
	for i, row := range rows {
		texts[i] = pdfread.ReconstructText(row)
	}

	// Find main row indices.
	var mainIdxs []int
	for i, t := range texts {
		if len(findDates(t)) > 0 && len(findAmounts(t)) >= 2 {
			mainIdxs = append(mainIdxs, i)
		}
	}

	var txns []*models.Transaction

	for i, mainIdx := range mainIdxs {
		// Block start: row after previous main row (or 0).
		blockStart := 0
		if i > 0 {
			blockStart = mainIdxs[i-1] + 1
		}

		// Block end: two rows before next main row (or end of page).
		// next_main-1 is the pre-row of the next transaction.
		blockEnd := len(texts) - 1
		if i+1 < len(mainIdxs) {
			blockEnd = mainIdxs[i+1] - 2
			if blockEnd < mainIdx {
				blockEnd = mainIdx
			}
		}

		// Gather description text from every row in the block.
		var descParts []string
		for j := blockStart; j <= blockEnd; j++ {
			t := strings.TrimSpace(texts[j])
			if t == "" || isNonTxnRow(t) {
				continue
			}
			if j == mainIdx {
				// Strip dates, amounts, and balance from the main row —
				// what remains is the middle portion of the description.
				t = descFromMainRow(t)
			}
			if t != "" {
				descParts = append(descParts, t)
			}
		}

		desc := cleanDesc(strings.Join(descParts, " "))
		dates := findDates(texts[mainIdx])
		amounts := findAmounts(texts[mainIdx])

		txn := buildTxn(dates, amounts, desc)
		if txn != nil {
			txn.Hash = txn.ComputeHash()
			txns = append(txns, txn)
		}
	}

	return txns
}

// buildTxn assembles a Transaction from parsed fields.
func buildTxn(dates []time.Time, amounts []float64, desc string) *models.Transaction {
	if len(dates) == 0 || len(amounts) < 2 {
		return nil
	}

	txnDate := dates[0]
	var valueDate *time.Time
	if len(dates) >= 2 {
		d := dates[1]
		valueDate = &d
	}

	// Layout: [...] debit credit balance — last amount is balance.
	balance := amounts[len(amounts)-1]

	var debit, credit float64
	if len(amounts) >= 3 {
		debit = amounts[len(amounts)-3]
		credit = amounts[len(amounts)-2]
	} else if len(amounts) == 2 {
		// Could be just one side + balance.
		debit = amounts[0]
	}

	amount := debit
	txnType := models.TxnDebit
	if credit > 0 && debit == 0 {
		amount = credit
		txnType = models.TxnCredit
	}

	if amount == 0 || desc == "" {
		return nil
	}

	return &models.Transaction{
		ID:              uuid.New().String(),
		Source:          models.SourceAUBank,
		TransactionDate: txnDate,
		ValueDate:       valueDate,
		Description:     desc,
		Amount:          amount,
		Type:            txnType,
		Balance:         balance,
		Category:        "Uncategorized",
		CreatedAt:       time.Now(),
	}
}

// descFromMainRow strips dates and amounts from a main row leaving just the
// description/reference fragment that sits in the middle.
func descFromMainRow(text string) string {
	// Remove date tokens.
	text = dateRe.ReplaceAllString(text, " ")
	// Remove amount tokens (including the balance).
	text = amountRe.ReplaceAllString(text, " ")
	// Remove standalone dashes (empty cells).
	text = dashRe.ReplaceAllString(text, " ")
	return cleanDesc(text)
}

// findDates extracts and parses all date tokens from text.
// Handles both "01Dec2025" and "01 Dec 2025".
func findDates(text string) []time.Time {
	matches := dateRe.FindAllString(text, -1)
	var dates []time.Time
	seen := map[string]bool{}
	for _, m := range matches {
		m = strings.Join(strings.Fields(m), "") // "01 Dec 2025" → "01Dec2025"
		if seen[m] {
			continue
		}
		seen[m] = true
		// Try compact layout first, then spaced.
		t, err := time.Parse("02Jan2006", m)
		if err != nil {
			spaced := m[:2] + " " + m[2:5] + " " + m[5:]
			t, err = time.Parse("02 Jan 2006", spaced)
		}
		if err == nil {
			dates = append(dates, t)
		}
	}
	return dates
}

// findAmounts extracts all numeric amounts from text.
func findAmounts(text string) []float64 {
	matches := amountRe.FindAllString(text, -1)
	var amounts []float64
	for _, m := range matches {
		v, err := parseIndianAmt(m)
		if err == nil && v > 0 {
			amounts = append(amounts, v)
		}
	}
	return amounts
}

func parseIndianAmt(s string) (float64, error) {
	s = strings.ReplaceAll(s, ",", "")
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

func isNonTxnRow(text string) bool {
	lower := strings.ToLower(text)
	for _, w := range stopWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

func cleanDesc(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}
