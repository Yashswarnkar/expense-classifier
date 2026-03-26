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
	flagDebugNatural  bool
)

var debugCmd = &cobra.Command{
	Use:   "debug-pdf",
	Short: "Dump raw text rows extracted from a PDF (for parser tuning)",
	Example: `  expense-classifier debug-pdf --file statement.pdf --password secret
  expense-classifier debug-pdf --file statement.pdf --natural   # show reconstructed text`,
	RunE: runDebugPDF,
}

func init() {
	debugCmd.Flags().StringVarP(&flagDebugFile, "file", "f", "", "Path to PDF (required)")
	debugCmd.Flags().StringVarP(&flagDebugPassword, "password", "p", "", "PDF password")
	debugCmd.Flags().IntVar(&flagDebugMaxRows, "max-rows", 60, "Max rows to print per page")
	debugCmd.Flags().BoolVar(&flagDebugNatural, "natural", false, "Use natural stream order + ReconstructText (for AU Bank style PDFs)")
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

		var rows [][]pdfread.TextElement
		if flagDebugNatural {
			rows = pdfread.GroupByRowsNaturalOrder(page.Elements, 4.0)
			fmt.Printf("Mode: natural stream order + ReconstructText\n")
		} else {
			rows = pdfread.GroupByRows(page.Elements, 4.0)
			fmt.Printf("Mode: X-sorted (standard)\n")
		}
		fmt.Printf("Rows: %d\n\n", len(rows))

		printed := 0
		for i, row := range rows {
			if printed >= flagDebugMaxRows {
				fmt.Printf("  ... (%d more rows)\n", len(rows)-i)
				break
			}
			var text string
			if flagDebugNatural {
				text = pdfread.ReconstructText(row)
			} else {
				text = pdfread.RowText(row)
			}
			if text == "" {
				continue
			}
			fmt.Printf("  [row %03d | y=%.1f] %s\n", i, row[0].Y, text)
			printed++
		}
	}
	return nil
}
