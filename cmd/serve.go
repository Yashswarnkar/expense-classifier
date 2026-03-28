package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/Yashswarnkar/expense-classifier/internal/api"
	"github.com/Yashswarnkar/expense-classifier/internal/storage/sqlite"
)

var flagServePort int

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP API server for the web frontend",
	Example: `  # Start on the default port (8080)
  expense-classifier serve

  # Use a custom port
  expense-classifier serve --port 9090`,
	RunE: runServe,
}

func init() {
	serveCmd.Flags().IntVar(&flagServePort, "port", 8080, "Port to listen on")
	rootCmd.AddCommand(serveCmd)
}

func runServe(_ *cobra.Command, _ []string) error {
	store, err := sqlite.Open(cfg.Storage.SQLitePath)
	if err != nil {
		return fmt.Errorf("storage: %w", err)
	}
	defer store.Close()

	mux := http.NewServeMux()
	handler := api.New(store, cfg.Categories.FilePath)
	handler.Register(mux)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", flagServePort),
		Handler:      api.CORS(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		fmt.Println("\nShutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx) //nolint:errcheck
	}()

	fmt.Printf("API server listening on http://localhost:%d\n", flagServePort)
	fmt.Println("Press Ctrl+C to stop.")

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}
