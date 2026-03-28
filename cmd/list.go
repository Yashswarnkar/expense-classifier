package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/Yashswarnkar/expense-classifier/internal/categories"
	"github.com/Yashswarnkar/expense-classifier/internal/models"
	"github.com/Yashswarnkar/expense-classifier/internal/storage/sqlite"
)

var (
	flagListCategory    string
	flagListSource      string
	flagListFrom        string
	flagListTo          string
	flagListInteractive bool
	flagListLimit       int
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List classified transactions with optional filters",
	Example: `  # Show all transactions
  expense-classifier list

  # Filter by category
  expense-classifier list --category "Dining Out"

  # Show only Uncategorized and fix them interactively
  expense-classifier list --category Uncategorized --interactive

  # Filter by source and date range
  expense-classifier list --source hdfc_cc --from 2025-12-01 --to 2025-12-31

  # Limit output
  expense-classifier list --limit 20`,
	RunE: runList,
}

func init() {
	listCmd.Flags().StringVar(&flagListCategory, "category", "", "Filter by category name (case-insensitive)")
	listCmd.Flags().StringVar(&flagListSource, "source", "", "Filter by source: aubank | hdfc_cc | amazon_pay_cc")
	listCmd.Flags().StringVar(&flagListFrom, "from", "", "Start date filter YYYY-MM-DD")
	listCmd.Flags().StringVar(&flagListTo, "to", "", "End date filter YYYY-MM-DD")
	listCmd.Flags().BoolVarP(&flagListInteractive, "interactive", "i", false, "Review and update each transaction's category interactively")
	listCmd.Flags().IntVar(&flagListLimit, "limit", 0, "Maximum number of transactions to show (0 = all)")
	rootCmd.AddCommand(listCmd)
}

func runList(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	store, err := sqlite.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	defer store.Close()

	txns, err := fetchListTransactions(ctx, store)
	if err != nil {
		return err
	}
	if len(txns) == 0 {
		fmt.Println("No transactions found for the given filters.")
		return nil
	}

	if flagListLimit > 0 && len(txns) > flagListLimit {
		txns = txns[:flagListLimit]
	}

	printTransactionTable(txns)

	if flagListInteractive {
		cats, err := categories.Load(cfg.Categories.FilePath)
		if err != nil {
			return fmt.Errorf("load categories: %w", err)
		}
		if err := interactiveListReview(ctx, store, txns, cats); err != nil {
			return err
		}
	}

	return nil
}

func fetchListTransactions(ctx context.Context, store *sqlite.Store) ([]*models.Transaction, error) {
	if flagListCategory != "" {
		return store.GetByCategory(ctx, flagListCategory)
	}
	if flagListFrom != "" || flagListTo != "" {
		from, to, err := parseDateRange(flagListFrom, flagListTo)
		if err != nil {
			return nil, err
		}
		return store.GetByDateRange(ctx, from, to)
	}
	if flagListSource != "" {
		return store.GetBySource(ctx, models.SourceType(flagListSource))
	}
	return store.GetAll(ctx)
}

func printTransactionTable(txns []*models.Transaction) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "DATE\tAMOUNT (₹)\tTYPE\tCATEGORY\tSOURCE\tDESCRIPTION")
	fmt.Fprintln(w, "----\t----------\t----\t--------\t------\t-----------")
	for _, t := range txns {
		fmt.Fprintf(w, "%s\t%.2f\t%s\t%s\t%s\t%s\n",
			t.TransactionDate.Format("02 Jan 2006"),
			t.Amount,
			t.Type,
			t.Category,
			t.Source,
			truncate(t.Description, 60),
		)
	}
	w.Flush()
	fmt.Printf("\n%d transaction(s)\n", len(txns))
}

func interactiveListReview(ctx context.Context, store *sqlite.Store, txns []*models.Transaction, cats []categories.Category) error {
	scanner := bufio.NewScanner(os.Stdin)
	catNames := categories.Names(cats)

	fmt.Println("\n--- Interactive Review ---")
	fmt.Println("Press Enter to keep the current category, or type a new one.")
	fmt.Printf("Available: %s\n\n", strings.Join(catNames, ", "))

	for _, t := range txns {
		fmt.Printf("Date: %s | Amount: ₹%.2f (%s) | Source: %s\n",
			t.TransactionDate.Format("02 Jan 2006"), t.Amount, t.Type, t.Source)
		fmt.Printf("Desc: %s\n", t.Description)
		fmt.Printf("Category [%s]: ", t.Category)

		if scanner.Scan() {
			input := strings.TrimSpace(scanner.Text())
			if input != "" && !strings.EqualFold(input, t.Category) {
				if err := store.UpdateCategory(ctx, t.ID, input); err != nil {
					fmt.Printf("  error updating: %v\n", err)
				} else {
					fmt.Printf("  updated → %s\n", input)
				}
			}
		}
		fmt.Println()
	}
	return nil
}
