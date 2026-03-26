package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/Yashswarnkar/expense-classifier/internal/config"
)

var (
	cfgFile string
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:   "expense-classifier",
	Short: "Classify bank and credit card transactions using a local LLM",
	Long: `expense-classifier parses AU Bank account statements and HDFC credit card
statements (password-protected PDFs), deduplicates transactions, calls a local
Ollama model to assign spending categories, and stores results in SQLite.

Exports to CSV and JSON are available for downstream tooling or a future UI.`,
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./config.yaml)")
}

func initConfig() {
	var err error
	cfg, err = config.Load(cfgFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
}
