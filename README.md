# expense-classifier

A CLI tool that parses **AU Bank account statements** and **HDFC credit card statements** (password-protected PDFs), deduplicates transactions across sources, and uses a **local Ollama LLM** to classify each transaction into your own spending categories.

All data stays on your machine — no external API calls, no cloud storage.

---

## Features

- Parse AU Bank account statement PDFs (password-protected)
- Parse HDFC Diners/credit card statement PDFs (password-protected)
- Automatic deduplication — the same payment will not be counted twice even if it appears in both a bank statement and a credit card statement
- Local LLM classification via [Ollama](https://ollama.com) (default: `mistral`)
- User-defined categories with optional hint keywords (`categories.txt`)
- SQLite storage for all transactions
- Export to CSV and/or JSON
- Spending summary by category (ready for a future UI)
- Interactive mode: review and override each LLM suggestion at the terminal
- `classify` command: manually fix a single transaction or re-run the LLM on everything still marked *Uncategorized*

---

## Prerequisites

| Tool | Version |
|------|---------|
| Go   | 1.22+   |
| [Ollama](https://ollama.com) | any recent |
| mistral model | `ollama pull mistral` |

---

## Installation

```bash
git clone https://github.com/yourusername/expense-classifier
cd expense-classifier
go build -o expense-classifier .
```

Or install directly:

```bash
go install github.com/yourusername/expense-classifier@latest
```

---

## Quick Start

```bash
# 1. Pull the model (one-time)
ollama pull mistral

# 2. Process an AU Bank statement
./expense-classifier process \
  --file path/to/au-statement.pdf \
  --password "your-pdf-password" \
  --type aubank

# 3. Process an HDFC CC statement
./expense-classifier process \
  --file path/to/hdfc-statement.pdf \
  --password "your-pdf-password" \
  --type hdfc

# 4. View spending summary
./expense-classifier summary

# 5. Export everything
./expense-classifier export --format all
```

---

## Configuration

Copy `config.yaml` and edit as needed. The tool looks for `config.yaml` in the current directory, or you can pass `--config path/to/config.yaml`.

```yaml
ollama:
  base_url: "http://localhost:11434"
  model: "mistral"       # any model you have pulled in Ollama
  timeout: "60s"

categories:
  file_path: "categories.txt"

storage:
  sqlite_path: "./expenses.db"
  export_dir:  "./exports"

processing:
  interactive: false              # prompt for each transaction by default?
  skip_llm: false                 # store without classifying?
  deduplicate_across_sources: true
```

Environment variable overrides use the `EXPENSE_` prefix:

```bash
EXPENSE_OLLAMA_MODEL=llama3 ./expense-classifier process --file stmt.pdf
```

---

## Categories

Edit `categories.txt` — one category per line. Keyword hints are optional but improve accuracy for ambiguous merchant names.

```text
# comment
Groceries: bigbasket, zepto, kirana, vegetables
Dining Out: swiggy, zomato, restaurant
Transport: uber, ola, rapido, petrol
Utilities: jio, airtel, electricity
Uncategorized
```

The LLM uses the full transaction description regardless of keywords — keywords are just extra context in the prompt.

---

## Commands

### `process`

Parse a statement PDF and store classified transactions.

```
Flags:
  -f, --file string       Path to statement PDF (required)
  -p, --password string   PDF password
  -t, --type string       aubank | hdfc  (auto-detected from filename if omitted)
  -i, --interactive       Review each classification at the terminal
      --skip-llm          Store without classifying (classify later with `classify`)
```

### `export`

Export stored transactions to files.

```
Flags:
      --format string   csv | json | all  (default "all")
  -o, --output string   Output directory  (default: export_dir in config)
      --source string   Filter: aubank | hdfc
      --from string     Start date YYYY-MM-DD
      --to string       End date YYYY-MM-DD
```

### `summary`

Print a per-category spending table to stdout.

```
Flags:
      --export   Also write summary CSV+JSON to the export directory
```

### `classify`

Manually override a category or re-run the LLM.

```
# Fix a single transaction (get the ID from the export)
./expense-classifier classify --id <uuid> --category "Dining Out"

# Re-run Ollama on everything still marked Uncategorized
./expense-classifier classify --reclassify-all
```

---

## Project Structure

```
expense-classifier/
├── cmd/                        # Cobra CLI commands
│   ├── root.go
│   ├── process.go              # Main pipeline: parse → dedup → classify → save
│   ├── export.go
│   ├── summary.go
│   └── classify.go
├── internal/
│   ├── config/                 # Viper config loading
│   ├── models/                 # Transaction struct + dedup hash logic
│   ├── pdf/                    # PDF decryption (pdfcpu) + text extraction
│   ├── parser/
│   │   ├── interface.go        # Parser interface — add new banks here
│   │   ├── aubank/             # AU Bank account statement parser
│   │   └── hdfc/               # HDFC credit card statement parser
│   ├── classifier/
│   │   ├── interface.go        # Classifier interface
│   │   └── ollama/
│   │       ├── prompt.go       # Prompt template (edit to tune LLM behaviour)
│   │       ├── client.go       # Ollama HTTP client
│   │       └── classifier.go   # Wires prompt + client, matches response to category
│   ├── categories/             # categories.txt loader
│   ├── deduplicator/           # Hash-based dedup
│   └── storage/
│       ├── interface.go        # Store interface (UI layer talks to this)
│       ├── sqlite/             # SQLite implementation
│       └── export/             # CSV + JSON writers
├── config.yaml
├── categories.txt
└── main.go
```

---

## Adding a New Bank

1. Create `internal/parser/newbank/parser.go` and implement `parser.Parser`.
2. Register it in `cmd/process.go → selectParser()`.

---

## Tuning the LLM Prompt

Edit `internal/classifier/ollama/prompt.go` — the `ClassificationPromptTmpl` variable. You can also swap the model by changing `ollama.model` in `config.yaml` without touching any code.

---

## Deduplication

The deduplication hash is computed from:
- Transaction date (YYYY-MM-DD)
- Amount in paise (integer, avoids float rounding)
- Lowercased, whitespace-normalised description

Source (bank vs credit card) is **excluded** from the hash. This means if the same UPI payment appears in your AU Bank outflow and your HDFC statement, only one copy is kept.

---

## License

MIT
