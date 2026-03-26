package config

import (
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Ollama     OllamaConfig     `mapstructure:"ollama"`
	Categories CategoriesConfig `mapstructure:"categories"`
	Storage    StorageConfig    `mapstructure:"storage"`
	Processing ProcessingConfig `mapstructure:"processing"`
}

type OllamaConfig struct {
	BaseURL string        `mapstructure:"base_url"`
	Model   string        `mapstructure:"model"`
	Timeout time.Duration `mapstructure:"timeout"`
}

type CategoriesConfig struct {
	FilePath string `mapstructure:"file_path"`
}

type StorageConfig struct {
	SQLitePath string `mapstructure:"sqlite_path"`
	ExportDir  string `mapstructure:"export_dir"`
}

type ProcessingConfig struct {
	Interactive bool `mapstructure:"interactive"`
	SkipLLM     bool `mapstructure:"skip_llm"`
	// DeduplicateAcrossSources controls whether transactions from different
	// sources with the same hash are considered duplicates.  Defaults to true
	// because the same UPI payment can surface in both a bank statement and a
	// credit-card statement.
	DeduplicateAcrossSources bool `mapstructure:"deduplicate_across_sources"`
}

// Load reads config from file (if found) and applies sensible defaults.
// configPath may be empty — the loader will look for config.yaml in CWD and
// $HOME/.expense-classifier.
func Load(configPath string) (*Config, error) {
	v := viper.New()

	v.SetDefault("ollama.base_url", "http://localhost:11434")
	v.SetDefault("ollama.model", "mistral")
	v.SetDefault("ollama.timeout", "60s")
	v.SetDefault("categories.file_path", "categories.txt")
	v.SetDefault("storage.sqlite_path", "./expenses.db")
	v.SetDefault("storage.export_dir", "./exports")
	v.SetDefault("processing.interactive", false)
	v.SetDefault("processing.skip_llm", false)
	v.SetDefault("processing.deduplicate_across_sources", true)

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("$HOME/.expense-classifier")
	}

	v.SetEnvPrefix("EXPENSE")
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
		// Config file is optional — defaults are fine.
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
