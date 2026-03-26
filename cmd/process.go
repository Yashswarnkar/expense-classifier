package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/Yashswarnkar/expense-classifier/internal/categories"
	"github.com/Yashswarnkar/expense-classifier/internal/classifier/ollama"
	"github.com/Yashswarnkar/expense-classifier/internal/deduplicator"
	"github.com/Yashswarnkar/expense-classifier/internal/models"
	"github.com/Yashswarnkar/expense-classifier/internal/parser"
	aubankparser    "github.com/Yashswarnkar/expense-classifier/internal/parser/aubank"
	amazonpayparser "github.com/Yashswarnkar/expense-classifier/internal/parser/amazonpay"
	hdfcparser      "github.com/Yashswarnkar/expense-classifier/internal/parser/hdfc"
	"github.com/Yashswarnkar/expense-classifier/internal/storage/sqlite"
)

var (
	flagFile        string
	flagPassword    string
	flagType        string
	flagInteractive bool
	flagSkipLLM     bool
)

var processCmd = &cobra.Command{
	Use:   "process",
	Short: "Parse a statement PDF and classify its transactions",
	Example: `  # AU Bank statement with password
  expense-classifier process --file statement.pdf --password secret --type aubank

  # HDFC CC statement, interactive review
  expense-classifier process --file cc.pdf --password secret --type hdfc --interactive

  # Skip LLM, just store (classify later)
  expense-classifier process --file statement.pdf --skip-llm`,
	RunE: runProcess,
}

func init() {
	processCmd.Flags().StringVarP(&flagFile, "file", "f", "", "Path to the statement PDF (required)")
	processCmd.Flags().StringVarP(&flagPassword, "password", "p", "", "PDF password (leave empty if not protected)")
	processCmd.Flags().StringVarP(&flagType, "type", "t", "", "Statement type: aubank | hdfc (auto-detected if omitted)")
	processCmd.Flags().BoolVarP(&flagInteractive, "interactive", "i", false, "Review and override each classification interactively")
	processCmd.Flags().BoolVar(&flagSkipLLM, "skip-llm", false, "Store transactions without classifying (classify later)")
	_ = processCmd.MarkFlagRequired("file")
	rootCmd.AddCommand(processCmd)
}

func runProcess(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()

	// --- 1. Select parser ---
	p, err := selectParser(flagType, flagFile)
	if err != nil {
		return err
	}
	fmt.Printf("Using parser: %s\n", p.Name())

	// --- 2. Parse PDF ---
	fmt.Printf("Parsing %s ...\n", flagFile)
	txns, err := p.Parse(flagFile, flagPassword)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	fmt.Printf("Found %d transactions\n", len(txns))

	// --- 3. Open storage & deduplicate ---
	store, err := sqlite.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	defer store.Close()

	existingHashes, err := store.ExistingHashes(ctx)
	if err != nil {
		return fmt.Errorf("fetch hashes: %w", err)
	}

	dedup := deduplicator.New(existingHashes)
	unique := dedup.Filter(txns)
	fmt.Printf("%d new (non-duplicate) transactions\n", len(unique))

	if len(unique) == 0 {
		fmt.Println("Nothing new to process.")
		return nil
	}

	// --- 4. Load categories ---
	cats, err := categories.Load(cfg.Categories.FilePath)
	if err != nil {
		return fmt.Errorf("load categories from %q: %w", cfg.Categories.FilePath, err)
	}

	// --- 5. Classify ---
	skipLLM := flagSkipLLM || cfg.Processing.SkipLLM
	if !skipLLM {
		clf := ollama.NewClassifier(
			cfg.Ollama.BaseURL,
			cfg.Ollama.Timeout,
			ollama.DefaultModelConfig(cfg.Ollama.Model),
		)
		if err := clf.Ping(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %v\n  Falling back to 'Uncategorized'\n", err)
			skipLLM = true
		} else {
			fmt.Printf("Classifying with model: %s\n", clf.ModelName())
			if err := classifyBatch(ctx, clf, unique, cats); err != nil {
				return err
			}
		}
	}

	// --- 6. Interactive override ---
	interactive := flagInteractive || cfg.Processing.Interactive
	if interactive && !skipLLM {
		if err := interactiveReview(unique, cats); err != nil {
			return err
		}
	}

	// --- 7. Save ---
	if err := store.Save(ctx, unique); err != nil {
		return fmt.Errorf("save: %w", err)
	}
	fmt.Printf("Saved %d transactions to %s\n", len(unique), cfg.Storage.SQLitePath)
	return nil
}

// selectParser picks the right parser: explicit flag > filename heuristic.
func selectParser(typFlag, filePath string) (parser.Parser, error) {
	switch strings.ToLower(typFlag) {
	case "aubank", "au", "au_bank":
		return aubankparser.New(), nil
	case "hdfc", "hdfc_cc":
		return hdfcparser.New(), nil
	case "amazon", "amazonpay", "amazon_pay", "amazon_pay_cc", "icici":
		return amazonpayparser.New(), nil
	case "":
		// Auto-detect from file name.
		lower := strings.ToLower(filePath)
		if strings.Contains(lower, "amazon") || strings.Contains(lower, "amazonpay") {
			return amazonpayparser.New(), nil
		}
		if strings.Contains(lower, "aubank") || strings.Contains(lower, "au_bank") {
			return aubankparser.New(), nil
		}
		if strings.Contains(lower, "hdfc") || strings.Contains(lower, "diners") {
			return hdfcparser.New(), nil
		}
		return nil, fmt.Errorf("could not auto-detect statement type; use --type aubank|hdfc|amazon")
	default:
		return nil, fmt.Errorf("unknown statement type %q; use aubank, hdfc, or amazon", typFlag)
	}
}

func classifyBatch(ctx context.Context, clf *ollama.Classifier, txns []*models.Transaction, cats []categories.Category) error {
	for i, t := range txns {
		cat, err := clf.Classify(ctx, t, cats)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [%d/%d] classify error (%s): %v — using Uncategorized\n",
				i+1, len(txns), t.Description[:min(40, len(t.Description))], err)
			cat = "Uncategorized"
		}
		t.Category = cat
		fmt.Printf("  [%d/%d] %-45s → %s\n",
			i+1, len(txns),
			truncate(t.Description, 45),
			t.Category,
		)
		// Brief pause to avoid hammering local Ollama.
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

func interactiveReview(txns []*models.Transaction, cats []categories.Category) error {
	scanner := bufio.NewScanner(os.Stdin)
	catNames := categories.Names(cats)

	fmt.Println("\n--- Interactive Review ---")
	fmt.Println("Press Enter to accept the suggested category, or type a new one.")
	fmt.Printf("Available categories: %s\n\n", strings.Join(catNames, ", "))

	for _, t := range txns {
		fmt.Printf("Date: %s | Amount: ₹%.2f (%s)\n",
			t.TransactionDate.Format("02 Jan 2006"), t.Amount, t.Type)
		fmt.Printf("Desc: %s\n", t.Description)
		fmt.Printf("Suggested category [%s]: ", t.Category)

		if scanner.Scan() {
			input := strings.TrimSpace(scanner.Text())
			if input != "" {
				t.Category = input
			}
		}
		fmt.Println()
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
