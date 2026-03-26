package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/Yashswarnkar/expense-classifier/internal/storage/export"
	"github.com/Yashswarnkar/expense-classifier/internal/storage/sqlite"
)

var (
	flagSummaryExport bool
)

var summaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Print a spending summary grouped by category",
	RunE:  runSummary,
}

func init() {
	summaryCmd.Flags().BoolVar(&flagSummaryExport, "export", false, "Also export summary to CSV+JSON in the export directory")
	rootCmd.AddCommand(summaryCmd)
}

func runSummary(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	store, err := sqlite.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	defer store.Close()

	summaries, err := store.Summary(ctx)
	if err != nil {
		return err
	}
	if len(summaries) == 0 {
		fmt.Println("No transactions found. Run `process` first.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "CATEGORY\tDEBIT (₹)\tCREDIT (₹)\tNET SPEND (₹)\tCOUNT")
	fmt.Fprintln(w, "--------\t---------\t----------\t-------------\t-----")

	var totalDebit, totalCredit float64
	for _, s := range summaries {
		fmt.Fprintf(w, "%s\t%.2f\t%.2f\t%.2f\t%d\n",
			s.Category, s.TotalDebit, s.TotalCredit, s.NetSpend, s.Count)
		totalDebit += s.TotalDebit
		totalCredit += s.TotalCredit
	}
	fmt.Fprintln(w, "--------\t---------\t----------\t-------------\t-----")
	fmt.Fprintf(w, "TOTAL\t%.2f\t%.2f\t%.2f\t\n",
		totalDebit, totalCredit, totalDebit-totalCredit)
	w.Flush()

	if flagSummaryExport {
		stamp := time.Now().Format("20060102-150405")
		csvPath := filepath.Join(cfg.Storage.ExportDir, fmt.Sprintf("summary-%s.csv", stamp))
		jsonPath := filepath.Join(cfg.Storage.ExportDir, fmt.Sprintf("summary-%s.json", stamp))
		_ = export.WriteSummaryCSV(summaries, csvPath)
		_ = export.WriteSummaryJSON(summaries, jsonPath)
		fmt.Printf("\nExported: %s\n         %s\n", csvPath, jsonPath)
	}
	return nil
}
