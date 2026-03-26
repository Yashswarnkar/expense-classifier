// Package aubank parses AU Small Finance Bank account statement PDFs.
//
// Expected column order (left-to-right):
//   Transaction Date | Value Date | Description/Narration |
//   Cheque/Reference No. | Debit (₹) | Credit (₹) | Balance (₹)
//
// Dates are in "02 Jan 2006" format.
// Amounts use Indian number formatting (1,25,912.58).
// The Description column can span multiple text rows.
package aubank

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/Yashswarnkar/expense-classifier/internal/models"
	pdfread "github.com/Yashswarnkar/expense-classifier/internal/pdf"
)

const (
	dateLayout = "02 Jan 2006"
	rowTol     = 4.0 // Y-position tolerance in PDF points for row grouping
)

var (
	dateRe     = regexp.MustCompile(`^\d{2}\s+[A-Za-z]{3}\s+\d{4}$`)
	amountRe   = regexp.MustCompile(`^[\d,]+\.\d{2}$`)
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

	all := pdfread.AllElements(pages)
	rows := pdfread.GroupByRows(all, rowTol)

	// Find the header row to determine column X-boundaries.
	colBounds, headerIdx, err := detectColumnBounds(rows)
	if err != nil {
		return nil, fmt.Errorf("aubank: %w", err)
	}

	return parseTransactions(rows[headerIdx+1:], colBounds)
}

// columnBounds holds the X-coordinate ranges for the seven columns.
type columnBounds struct {
	txnDate    [2]float64
	valueDate  [2]float64
	desc       [2]float64
	reference  [2]float64
	debit      [2]float64
	credit     [2]float64
	balance    [2]float64
}

func detectColumnBounds(rows [][]pdfread.TextElement) (columnBounds, int, error) {
	for i, row := range rows {
		text := strings.ToLower(pdfread.RowText(row))
		// The header row contains all these keywords.
		if strings.Contains(text, "transaction") &&
			strings.Contains(text, "description") &&
			strings.Contains(text, "debit") &&
			strings.Contains(text, "balance") {

			return buildBounds(row), i, nil
		}
	}
	return columnBounds{}, 0, fmt.Errorf("could not locate table header row — is this an AU Bank statement?")
}

// buildBounds maps header cells to their X midpoints, then derives ranges.
// We look for the 7 known header keywords and assign column indices.
func buildBounds(headerRow []pdfread.TextElement) columnBounds {
	type colInfo struct {
		keyword string
		midX    float64
	}

	// Collect midpoints for cells we recognise.
	var infos []colInfo
	keywords := []string{"transaction", "value", "description", "cheque", "debit", "credit", "balance"}
	for _, el := range headerRow {
		low := strings.ToLower(el.Content)
		for _, kw := range keywords {
			if strings.Contains(low, kw) {
				infos = append(infos, colInfo{keyword: kw, midX: el.X})
				break
			}
		}
	}

	// Build a map keyword→X.
	xOf := map[string]float64{}
	for _, inf := range infos {
		if _, exists := xOf[inf.keyword]; !exists {
			xOf[inf.keyword] = inf.midX
		}
	}

	// Fallback: if we don't have all columns use generous X-splits.
	getX := func(kw string, fallback float64) float64 {
		if v, ok := xOf[kw]; ok {
			return v
		}
		return fallback
	}

	x0 := getX("transaction", 30)
	x1 := getX("value", 100)
	x2 := getX("description", 170)
	x3 := getX("cheque", 330)
	x4 := getX("debit", 410)
	x5 := getX("credit", 470)
	x6 := getX("balance", 530)
	xMax := x6 + 100

	mid := func(a, b float64) float64 { return (a + b) / 2 }

	return columnBounds{
		txnDate:   [2]float64{x0 - 10, mid(x0, x1)},
		valueDate: [2]float64{mid(x0, x1), mid(x1, x2)},
		desc:      [2]float64{mid(x1, x2), mid(x2, x3)},
		reference: [2]float64{mid(x2, x3), mid(x3, x4)},
		debit:     [2]float64{mid(x3, x4), mid(x4, x5)},
		credit:    [2]float64{mid(x4, x5), mid(x5, x6)},
		balance:   [2]float64{mid(x5, x6), xMax},
	}
}

func inRange(x float64, r [2]float64) bool {
	return x >= r[0] && x < r[1]
}

// cellsInRange collects text from elements that fall within a column range.
func cellsInRange(row []pdfread.TextElement, r [2]float64) string {
	var parts []string
	for _, el := range row {
		if inRange(el.X, r) {
			parts = append(parts, el.Content)
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// parseTransactions converts raw rows (below the header) into Transaction objects.
// Multi-line descriptions are handled by folding continuation rows into the
// preceding transaction's description field.
func parseTransactions(rows [][]pdfread.TextElement, cb columnBounds) ([]*models.Transaction, error) {
	var txns []*models.Transaction
	var current *models.Transaction

	finalize := func() {
		if current != nil {
			current.Description = strings.TrimSpace(current.Description)
			current.Hash = current.ComputeHash()
			txns = append(txns, current)
			current = nil
		}
	}

	for _, row := range rows {
		dateStr := strings.TrimSpace(cellsInRange(row, cb.txnDate))

		if isDate(dateStr) {
			finalize()

			txnDate, err := time.Parse(dateLayout, dateStr)
			if err != nil {
				continue
			}

			valDateStr := cellsInRange(row, cb.valueDate)
			var valDate *time.Time
			if d, err := time.Parse(dateLayout, valDateStr); err == nil {
				valDate = &d
			}

			desc := cellsInRange(row, cb.desc)
			ref := cellsInRange(row, cb.reference)
			debitStr := cellsInRange(row, cb.debit)
			creditStr := cellsInRange(row, cb.credit)
			balStr := cellsInRange(row, cb.balance)

			amount, txnType := resolveAmount(debitStr, creditStr)
			balance, _ := parseIndianAmount(balStr)

			current = &models.Transaction{
				ID:              uuid.New().String(),
				Source:          models.SourceAUBank,
				TransactionDate: txnDate,
				ValueDate:       valDate,
				Description:     desc,
				ReferenceNo:     ref,
				Amount:          amount,
				Type:            txnType,
				Balance:         balance,
				Category:        "Uncategorized",
				CreatedAt:       time.Now(),
			}
		} else if current != nil {
			// Continuation: append description text from the desc column.
			extra := cellsInRange(row, cb.desc)
			if extra != "" {
				current.Description += " " + extra
			}
		}
		// Rows before the first date (e.g., account summary) are ignored.
	}
	finalize()
	return txns, nil
}

func isDate(s string) bool {
	return dateRe.MatchString(s)
}

// resolveAmount picks the non-zero side and determines direction.
func resolveAmount(debitStr, creditStr string) (float64, models.TxnType) {
	debit, _ := parseIndianAmount(debitStr)
	credit, _ := parseIndianAmount(creditStr)

	if math.Abs(debit) > 0 {
		return debit, models.TxnDebit
	}
	if math.Abs(credit) > 0 {
		return credit, models.TxnCredit
	}
	return 0, models.TxnDebit
}

// parseIndianAmount handles amounts like "1,25,912.58" and "-".
func parseIndianAmount(s string) (float64, error) {
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, "₹", "")
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parseIndianAmount(%q): %w", s, err)
	}
	return v, nil
}

// Ensure Parser satisfies the parser.Parser interface at compile time.
var _ interface {
	Parse(string, string) ([]*models.Transaction, error)
	Name() string
	Source() models.SourceType
} = (*Parser)(nil)

// amountRe is used externally if needed.
var _ = amountRe
