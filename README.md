# expense-classifier

A CLI tool that parses **AU Bank account statements**, **HDFC Diners credit card statements**, and **Amazon Pay ICICI credit card statements** (password-protected PDFs), deduplicates transactions across all sources, and uses a **local Ollama LLM** to classify each transaction into your own spending categories.

All data stays on your machine — no external API calls, no cloud storage.

---

## Features

- Parse AU Bank account statement PDFs (password-protected)
- Parse HDFC Diners credit card statement PDFs (password-protected)
- Parse Amazon Pay ICICI credit card statement PDFs (password-protected)
- Automatic deduplication — the same payment will not be counted twice even if it appears in multiple statements
- Local LLM classification via [Ollama](https://ollama.com) (default: `mistral`)
- User-defined categories with optional hint keywords (`categories.txt`)
- SQLite storage for all transactions — new statements never overwrite existing data
- Export to CSV and/or JSON
- Smart credit card payment handling — CC bill payments (e.g. via CRED, autopay) are classified separately and excluded from the spending total to avoid double-counting with credit card statement transactions
- Spending summary by category with a dedicated CC payment section showing payment count and total amount
- Interactive mode: review and override each LLM suggestion at the terminal
- `classify` command: manually fix a single transaction or re-run the LLM on everything still marked *Uncategorized*
- `debug-pdf` command: dump raw extracted text rows to diagnose parser issues

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
git clone https://github.com/Yashswarnkar/expense-classifier
cd expense-classifier
go build -o expense-classifier .
```

Or install directly:

```bash
go install github.com/Yashswarnkar/expense-classifier@latest
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

# 3. Process an HDFC Diners CC statement
./expense-classifier process \
  --file path/to/hdfc-statement.pdf \
  --password "your-pdf-password" \
  --type hdfc

# 4. Process an Amazon Pay ICICI CC statement
./expense-classifier process \
  --file path/to/amazon-statement.pdf \
  --password "your-pdf-password" \
  --type amazon

# 5. View spending summary
./expense-classifier summary

# 6. Export everything
./expense-classifier export --format all
```

---

## Supported Statement Formats

| Bank / Card | Type flag | Auto-detected from filename |
|---|---|---|
| AU Small Finance Bank | `aubank` | filename contains `aubank` or `au_bank` |
| HDFC Diners Credit Card | `hdfc` | filename contains `hdfc` or `diners` |
| Amazon Pay ICICI Credit Card | `amazon` | filename contains `amazon` or `amazonpay` |

---

## Configuration

Copy `config.yaml` and edit as needed. The tool looks for `config.yaml` in the current directory, or pass `--config path/to/config.yaml`.

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

### Credit Card Payment category

The `Credit Card Payment` category is built-in and handles CC bill payments made from your bank account — via CRED, bank autopay, or direct transfer to any card issuer:

```text
Credit Card Payment: cred, credit card payment, cc bill, cc autopay,
                     bill payment hdfc, bill payment sbi, bill payment axis,
                     bill payment icici, bill payment amex, creditcard, credit card bill
```

When you import both an AU Bank statement and an HDFC CC statement, the AU Bank debit for paying the CC bill and the individual HDFC transactions both represent the same spending. Classifying the bank-side payment as `Credit Card Payment` and excluding it from totals prevents double-counting while still giving you visibility into how much you paid and when.

---

## Commands

### `process`

Parse a statement PDF and store classified transactions.

```
Flags:
  -f, --file string       Path to statement PDF (required)
  -p, --password string   PDF password
  -t, --type string       aubank | hdfc | amazon  (auto-detected from filename if omitted)
  -i, --interactive       Review each classification at the terminal
      --skip-llm          Store without classifying (classify later with `classify`)
```

### `export`

Export stored transactions to files.

```
Flags:
      --format string   csv | json | all  (default "all")
  -o, --output string   Output directory  (default: export_dir in config)
      --source string   Filter by source: aubank | hdfc_cc | amazon_pay_cc
      --from string     Start date YYYY-MM-DD
      --to string       End date YYYY-MM-DD
```

### `summary`

Print a per-category spending table to stdout.

```
Flags:
      --export   Also write summary CSV+JSON to the export directory
```

The output is split into two sections:

```
CATEGORY        DEBIT (₹)   CREDIT (₹)   NET SPEND (₹)   COUNT
--------        ---------   ----------   -------------   -----
Groceries       3500.00     0.00         3500.00          12
Dining Out      2100.00     0.00         2100.00           8
...
TOTAL           18000.00    500.00       17500.00

── Credit Card Payments (excluded from total to avoid double-counting) ──
CATEGORY               DEBIT (₹)    CREDIT (₹)   NET SPEND (₹)   COUNT
--------               ---------    ----------   -------------   -----
Credit Card Payment    25000.00     0.00         25000.00         3
TOTAL CC PAID          25000.00     0.00         25000.00
```

`Credit Card Payment` transactions are shown separately so you can see your total CC outflow, but they are excluded from `TOTAL` to avoid double-counting with the detailed per-transaction breakdown from your credit card statements.

### `serve`

Start the HTTP API server that powers the [expense-frontend](https://github.com/Yashswarnkar/expense-frontend) web UI.

```
Flags:
      --port int   Port to listen on (default 8080)
```

```bash
./expense-classifier serve           # http://localhost:8080
./expense-classifier serve --port 9090
```

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/transactions` | List transactions — filters: `category`, `source`, `from`, `to`, `search`, `limit` |
| `PATCH` | `/api/transactions/:id` | Update a transaction's category |
| `GET` | `/api/summary` | Spending totals by category (CC payments split into separate array) |
| `GET` | `/api/categories` | Available categories from `categories.txt` |

CORS is enabled so the Vite dev server (`:5173`) can call the API without a proxy.

### `list`

List transactions in the terminal with optional filters. This is the primary way to browse classified transactions and fix categories without touching an export file.

```
Flags:
      --category string   Filter by category name (case-insensitive)
      --source string     Filter by source: aubank | hdfc_cc | amazon_pay_cc
      --from string       Start date YYYY-MM-DD
      --to string         End date YYYY-MM-DD
  -i, --interactive       Review and update each transaction's category interactively
      --limit int         Maximum number of transactions to show (0 = all)
```

```bash
# Show all transactions
./expense-classifier list

# Browse a specific category
./expense-classifier list --category "Dining Out"

# Find and fix Uncategorized transactions interactively
./expense-classifier list --category Uncategorized --interactive

# Filter by source and date range
./expense-classifier list --source hdfc_cc --from 2025-12-01 --to 2025-12-31

# Show only the 20 most recent
./expense-classifier list --limit 20
```

Example output:
```
DATE          AMOUNT (₹)   TYPE    CATEGORY              SOURCE     DESCRIPTION
----          ----------   ----    --------              ------     -----------
01 Dec 2025   25000.00     debit   Credit Card Payment   au_bank    CRED/HDFC CC BILL PAYMENT
03 Dec 2025   450.00       debit   Dining Out            hdfc_cc    SWIGGY ORDER #123456
...

12 transaction(s)
```

In `--interactive` mode, each transaction is shown one by one and you can type a new category or press Enter to keep the existing one. Changes are saved immediately to the database.

### `classify`

Manually override a category or re-run the LLM.

```bash
# Fix a single transaction (get the ID from the export)
./expense-classifier classify --id <uuid> --category "Dining Out"

# Re-run Ollama on everything still marked Uncategorized
./expense-classifier classify --reclassify-all
```

### `debug-pdf`

Dump raw extracted text rows from a PDF with Y-positions. Useful for diagnosing why a parser cannot find the table header.

```bash
./expense-classifier debug-pdf \
  --file statement.pdf \
  --password "your-password" \
  --max-rows 100
```

---

## How Data Is Stored

- All transactions go into a single SQLite database (`expenses.db` by default)
- Re-processing the same statement is safe — duplicates are detected by a hash of `(date + amount + normalised description)` and silently skipped
- Source is excluded from the hash, so the same UPI payment appearing in both a bank statement and a credit card statement is stored only once
- Categories can be updated at any time without re-parsing

---

## Project Structure

```
expense-classifier/
├── cmd/                          # Cobra CLI commands
│   ├── root.go
│   ├── process.go                # Main pipeline: parse → dedup → classify → save
│   ├── export.go
│   ├── summary.go
│   ├── classify.go
│   └── debug.go                  # debug-pdf command
├── internal/
│   ├── config/                   # Viper config loading
│   ├── models/                   # Transaction struct + dedup hash logic
│   ├── pdf/                      # PDF decryption (pdfcpu) + text extraction
│   ├── parser/
│   │   ├── interface.go          # Parser interface — add new banks here
│   │   ├── aubank/               # AU Small Finance Bank account statement
│   │   ├── hdfc/                 # HDFC Diners credit card statement
│   │   └── amazonpay/            # Amazon Pay ICICI credit card statement
│   ├── classifier/
│   │   ├── interface.go          # Classifier interface
│   │   └── ollama/
│   │       ├── prompt.go         # Prompt template (edit to tune LLM behaviour)
│   │       ├── client.go         # Ollama HTTP client
│   │       └── classifier.go     # Wires prompt + client, matches response to category
│   ├── categories/               # categories.txt loader
│   ├── deduplicator/             # Hash-based dedup
│   └── storage/
│       ├── interface.go          # Store interface (UI layer talks to this)
│       ├── sqlite/               # SQLite implementation
│       └── export/               # CSV + JSON writers
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

Source (bank vs credit card) is **excluded** from the hash. This means if the same UPI payment appears in your AU Bank outflow and your Amazon Pay statement, only one copy is kept.

---

## License

MIT
