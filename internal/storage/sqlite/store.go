package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, no CGO required

	"github.com/Yashswarnkar/expense-classifier/internal/models"
)

const schema = `
CREATE TABLE IF NOT EXISTS transactions (
    id               TEXT PRIMARY KEY,
    source           TEXT    NOT NULL,
    transaction_date TEXT    NOT NULL,
    value_date       TEXT,
    description      TEXT    NOT NULL,
    reference_no     TEXT    NOT NULL DEFAULT '',
    amount           REAL    NOT NULL,
    txn_type         TEXT    NOT NULL,
    balance          REAL    NOT NULL DEFAULT 0,
    category         TEXT    NOT NULL DEFAULT 'Uncategorized',
    hash             TEXT    NOT NULL UNIQUE,
    created_at       TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_txn_date     ON transactions(transaction_date);
CREATE INDEX IF NOT EXISTS idx_txn_category ON transactions(category);
CREATE INDEX IF NOT EXISTS idx_txn_source   ON transactions(source);
`

// Store wraps a SQLite database and satisfies storage.Store.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %q: %w", path, err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("sqlite: apply schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Save inserts transactions; rows with a duplicate hash are skipped silently.
func (s *Store) Save(ctx context.Context, txns []*models.Transaction) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO transactions
		  (id, source, transaction_date, value_date, description, reference_no,
		   amount, txn_type, balance, category, hash, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, t := range txns {
		var valDate interface{}
		if t.ValueDate != nil {
			valDate = t.ValueDate.Format("2006-01-02")
		}
		_, err := stmt.ExecContext(ctx,
			t.ID,
			string(t.Source),
			t.TransactionDate.Format("2006-01-02"),
			valDate,
			t.Description,
			t.ReferenceNo,
			t.Amount,
			string(t.Type),
			t.Balance,
			t.Category,
			t.Hash,
			t.CreatedAt.Format(time.RFC3339),
		)
		if err != nil {
			return fmt.Errorf("sqlite: insert %s: %w", t.ID, err)
		}
	}
	return tx.Commit()
}

func (s *Store) UpdateCategory(ctx context.Context, id, category string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE transactions SET category = ? WHERE id = ?`, category, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("sqlite: transaction %q not found", id)
	}
	return nil
}

func (s *Store) GetAll(ctx context.Context) ([]*models.Transaction, error) {
	return s.query(ctx, `SELECT * FROM transactions ORDER BY transaction_date DESC, id`)
}

func (s *Store) GetBySource(ctx context.Context, source models.SourceType) ([]*models.Transaction, error) {
	return s.query(ctx,
		`SELECT * FROM transactions WHERE source = ? ORDER BY transaction_date DESC`, string(source))
}

func (s *Store) GetByDateRange(ctx context.Context, from, to time.Time) ([]*models.Transaction, error) {
	return s.query(ctx,
		`SELECT * FROM transactions WHERE transaction_date BETWEEN ? AND ? ORDER BY transaction_date DESC`,
		from.Format("2006-01-02"), to.Format("2006-01-02"))
}

func (s *Store) GetByCategory(ctx context.Context, category string) ([]*models.Transaction, error) {
	return s.query(ctx,
		`SELECT * FROM transactions WHERE LOWER(category) = LOWER(?) ORDER BY transaction_date DESC`, category)
}

func (s *Store) ExistingHashes(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT hash FROM transactions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hashes []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		hashes = append(hashes, h)
	}
	return hashes, rows.Err()
}

func (s *Store) Summary(ctx context.Context) ([]*models.Summary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
		    category,
		    SUM(CASE WHEN txn_type = 'debit'  THEN amount ELSE 0 END) AS total_debit,
		    SUM(CASE WHEN txn_type = 'credit' THEN amount ELSE 0 END) AS total_credit,
		    COUNT(*) AS cnt
		FROM transactions
		GROUP BY category
		ORDER BY total_debit DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []*models.Summary
	for rows.Next() {
		var sum models.Summary
		if err := rows.Scan(&sum.Category, &sum.TotalDebit, &sum.TotalCredit, &sum.Count); err != nil {
			return nil, err
		}
		sum.NetSpend = sum.TotalDebit - sum.TotalCredit
		summaries = append(summaries, &sum)
	}
	return summaries, rows.Err()
}

// query is a shared helper that scans rows into Transaction slices.
func (s *Store) query(ctx context.Context, q string, args ...interface{}) ([]*models.Transaction, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []*models.Transaction
	for rows.Next() {
		t, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		txns = append(txns, t)
	}
	return txns, rows.Err()
}

func scanTransaction(rows *sql.Rows) (*models.Transaction, error) {
	var t models.Transaction
	var valDateStr sql.NullString
	var txnDateStr, createdAtStr string

	err := rows.Scan(
		&t.ID, &t.Source, &txnDateStr, &valDateStr,
		&t.Description, &t.ReferenceNo,
		&t.Amount, &t.Type, &t.Balance,
		&t.Category, &t.Hash, &createdAtStr,
	)
	if err != nil {
		return nil, err
	}

	t.TransactionDate, _ = time.Parse("2006-01-02", txnDateStr)
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAtStr)
	if valDateStr.Valid {
		d, err := time.Parse("2006-01-02", valDateStr.String)
		if err == nil {
			t.ValueDate = &d
		}
	}
	return &t, nil
}
