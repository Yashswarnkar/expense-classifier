package export

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Yashswarnkar/expense-classifier/internal/models"
)

// WriteCSV writes transactions to a CSV file at outPath.
func WriteCSV(txns []*models.Transaction, outPath string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return fmt.Errorf("csv: mkdir: %w", err)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("csv: create %q: %w", outPath, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"ID", "Source", "TransactionDate", "ValueDate", "Description",
		"ReferenceNo", "Amount", "Type", "Balance", "Category",
	}
	if err := w.Write(header); err != nil {
		return err
	}

	for _, t := range txns {
		valDate := ""
		if t.ValueDate != nil {
			valDate = t.ValueDate.Format("2006-01-02")
		}
		row := []string{
			t.ID,
			string(t.Source),
			t.TransactionDate.Format("2006-01-02"),
			valDate,
			t.Description,
			t.ReferenceNo,
			fmt.Sprintf("%.2f", t.Amount),
			string(t.Type),
			fmt.Sprintf("%.2f", t.Balance),
			t.Category,
		}
		if err := w.Write(row); err != nil {
			return err
		}
	}
	return w.Error()
}

// WriteSummaryCSV writes category summaries to a CSV file.
func WriteSummaryCSV(summaries []*models.Summary, outPath string) error {
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		return fmt.Errorf("csv: mkdir: %w", err)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("csv: create %q: %w", outPath, err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	_ = w.Write([]string{"Category", "TotalDebit", "TotalCredit", "NetSpend", "Count"})
	for _, s := range summaries {
		_ = w.Write([]string{
			s.Category,
			fmt.Sprintf("%.2f", s.TotalDebit),
			fmt.Sprintf("%.2f", s.TotalCredit),
			fmt.Sprintf("%.2f", s.NetSpend),
			fmt.Sprintf("%d", s.Count),
		})
	}
	return w.Error()
}
