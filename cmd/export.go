package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/Yashswarnkar/expense-classifier/internal/models"
	"github.com/Yashswarnkar/expense-classifier/internal/storage/export"
	"github.com/Yashswarnkar/expense-classifier/internal/storage/sqlite"
)

var (
	flagExportFormat string
	flagExportOutput string
	flagExportSource string
	flagDateFrom     string
	flagDateTo       string
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export transactions to CSV and/or JSON",
	Example: `  expense-classifier export --format csv
  expense-classifier export --format all --output ./my-exports
  expense-classifier export --source aubank --from 2025-12-01 --to 2025-12-31`,
	RunE: runExport,
}

func init() {
	exportCmd.Flags().StringVar(&flagExportFormat, "format", "all", "Export format: csv | json | all")
	exportCmd.Flags().StringVarP(&flagExportOutput, "output", "o", "", "Output directory (defaults to storage.export_dir in config)")
	exportCmd.Flags().StringVar(&flagExportSource, "source", "", "Filter by source: aubank | hdfc")
	exportCmd.Flags().StringVar(&flagDateFrom, "from", "", "Start date filter YYYY-MM-DD")
	exportCmd.Flags().StringVar(&flagDateTo, "to", "", "End date filter YYYY-MM-DD")
	rootCmd.AddCommand(exportCmd)
}

func runExport(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	store, err := sqlite.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	defer store.Close()

	txns, err := fetchTransactions(ctx, store)
	if err != nil {
		return err
	}
	if len(txns) == 0 {
		fmt.Println("No transactions found for the given filters.")
		return nil
	}

	outDir := flagExportOutput
	if outDir == "" {
		outDir = cfg.Storage.ExportDir
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}

	stamp := time.Now().Format("20060102-150405")
	fmt.Printf("Exporting %d transactions...\n", len(txns))

	switch flagExportFormat {
	case "csv":
		return writeCSV(txns, outDir, stamp)
	case "json":
		return writeJSON(txns, outDir, stamp)
	default: // "all"
		if err := writeCSV(txns, outDir, stamp); err != nil {
			return err
		}
		return writeJSON(txns, outDir, stamp)
	}
}

func fetchTransactions(ctx context.Context, store *sqlite.Store) ([]*models.Transaction, error) {
	if flagDateFrom != "" || flagDateTo != "" {
		from, to, err := parseDateRange(flagDateFrom, flagDateTo)
		if err != nil {
			return nil, err
		}
		return store.GetByDateRange(ctx, from, to)
	}
	if flagExportSource != "" {
		src := models.SourceType(flagExportSource)
		return store.GetBySource(ctx, src)
	}
	return store.GetAll(ctx)
}

func writeCSV(txns []*models.Transaction, dir, stamp string) error {
	path := filepath.Join(dir, fmt.Sprintf("transactions-%s.csv", stamp))
	if err := export.WriteCSV(txns, path); err != nil {
		return err
	}
	fmt.Printf("CSV  → %s\n", path)
	return nil
}

func writeJSON(txns []*models.Transaction, dir, stamp string) error {
	path := filepath.Join(dir, fmt.Sprintf("transactions-%s.json", stamp))
	if err := export.WriteJSON(txns, path); err != nil {
		return err
	}
	fmt.Printf("JSON → %s\n", path)
	return nil
}

func parseDateRange(fromStr, toStr string) (time.Time, time.Time, error) {
	layout := "2006-01-02"
	var from, to time.Time
	var err error
	if fromStr != "" {
		from, err = time.Parse(layout, fromStr)
		if err != nil {
			return from, to, fmt.Errorf("invalid --from date: %w", err)
		}
	} else {
		from = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if toStr != "" {
		to, err = time.Parse(layout, toStr)
		if err != nil {
			return from, to, fmt.Errorf("invalid --to date: %w", err)
		}
	} else {
		to = time.Now()
	}
	return from, to, nil
}
