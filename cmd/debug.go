package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	pdfread "github.com/Yashswarnkar/expense-classifier/internal/pdf"
)

var (
	flagDebugFile     string
	flagDebugPassword string
	flagDebugMaxRows  int
)

var debugCmd = &cobra.Command{
	Use:   "debug-pdf",
	Short: "Dump raw text rows extracted from a PDF (for parser tuning)",
	Example: `  expense-classifier debug-pdf --file statement.pdf --password secret`,
	RunE: runDebugPDF,
}

func init() {
	debugCmd.Flags().StringVarP(&flagDebugFile, "file", "f", "", "Path to PDF (required)")
	debugCmd.Flags().StringVarP(&flagDebugPassword, "password", "p", "", "PDF password")
	debugCmd.Flags().IntVar(&flagDebugMaxRows, "max-rows", 80, "Max rows to print per page")
	_ = debugCmd.MarkFlagRequired("file")
	rootCmd.AddCommand(debugCmd)
}

func runDebugPDF(_ *cobra.Command, _ []string) error {
	pages, err := pdfread.DecryptAndExtract(flagDebugFile, flagDebugPassword)
	if err != nil {
		return fmt.Errorf("extract: %w", err)
	}

	for _, page := range pages {
		fmt.Printf("\n========== PAGE %d (%d elements) ==========\n", page.PageNum, len(page.Elements))
		rows := pdfread.GroupByRows(page.Elements, 4.0)
		fmt.Printf("Grouped into %d rows\n\n", len(rows))

		printed := 0
		for i, row := range rows {
			if printed >= flagDebugMaxRows {
				fmt.Printf("  ... (%d more rows not shown)\n", len(rows)-i)
				break
			}
			rowText := pdfread.RowText(row)
			if rowText == "" {
				continue
			}
			// Print Y position of first element + full row text
			fmt.Printf("  [row %03d | y=%.1f] %s\n", i, row[0].Y, rowText)
			printed++
		}
	}
	return nil
}
