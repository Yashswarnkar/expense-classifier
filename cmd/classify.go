package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/Yashswarnkar/expense-classifier/internal/categories"
	"github.com/Yashswarnkar/expense-classifier/internal/classifier/ollama"
	"github.com/Yashswarnkar/expense-classifier/internal/storage/sqlite"
)

var (
	flagClassifyID       string
	flagClassifyCategory string
	flagReclassifyAll    bool
)

var classifyCmd = &cobra.Command{
	Use:   "classify",
	Short: "Manually override a category, or re-run LLM on all Uncategorized transactions",
	Example: `  # Override a single transaction
  expense-classifier classify --id <uuid> --category "Dining Out"

  # Re-run Ollama on all transactions still marked Uncategorized
  expense-classifier classify --reclassify-all`,
	RunE: runClassify,
}

func init() {
	classifyCmd.Flags().StringVar(&flagClassifyID, "id", "", "Transaction ID to update")
	classifyCmd.Flags().StringVar(&flagClassifyCategory, "category", "", "New category name")
	classifyCmd.Flags().BoolVar(&flagReclassifyAll, "reclassify-all", false, "Re-run LLM on all Uncategorized transactions")
	rootCmd.AddCommand(classifyCmd)
}

func runClassify(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	store, err := sqlite.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	defer store.Close()

	// Manual single-transaction override.
	if flagClassifyID != "" && flagClassifyCategory != "" {
		if err := store.UpdateCategory(ctx, flagClassifyID, flagClassifyCategory); err != nil {
			return err
		}
		fmt.Printf("Updated transaction %s → %s\n", flagClassifyID, flagClassifyCategory)
		return nil
	}

	// Re-classify all Uncategorized.
	if flagReclassifyAll {
		return reclassifyAll(ctx, store)
	}

	return fmt.Errorf("provide --id + --category for a manual override, or --reclassify-all")
}

func reclassifyAll(ctx context.Context, store *sqlite.Store) error {
	cats, err := categories.Load(cfg.Categories.FilePath)
	if err != nil {
		return fmt.Errorf("load categories: %w", err)
	}

	clf := ollama.NewClassifier(
		cfg.Ollama.BaseURL,
		cfg.Ollama.Timeout,
		ollama.DefaultModelConfig(cfg.Ollama.Model),
	)
	if err := clf.Ping(ctx); err != nil {
		return err
	}

	txns, err := store.GetAll(ctx)
	if err != nil {
		return err
	}

	var uncategorized = txns[:0]
	for _, t := range txns {
		if strings.EqualFold(t.Category, "Uncategorized") {
			uncategorized = append(uncategorized, t)
		}
	}

	if len(uncategorized) == 0 {
		fmt.Println("All transactions are already categorized.")
		return nil
	}

	fmt.Printf("Re-classifying %d uncategorized transactions...\n", len(uncategorized))
	for i, t := range uncategorized {
		cat, err := clf.Classify(ctx, t, cats)
		if err != nil {
			fmt.Printf("  [%d/%d] error: %v\n", i+1, len(uncategorized), err)
			continue
		}
		if err := store.UpdateCategory(ctx, t.ID, cat); err != nil {
			return err
		}
		fmt.Printf("  [%d/%d] %-45s → %s\n", i+1, len(uncategorized),
			truncate(t.Description, 45), cat)
	}
	return nil
}
