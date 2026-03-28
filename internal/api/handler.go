package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Yashswarnkar/expense-classifier/internal/categories"
	"github.com/Yashswarnkar/expense-classifier/internal/models"
	"github.com/Yashswarnkar/expense-classifier/internal/storage/sqlite"
)

const ccPaymentCategory = "Credit Card Payment"

// Handler holds dependencies for all API endpoints.
type Handler struct {
	store          *sqlite.Store
	categoriesFile string
}

// New creates a Handler.
func New(store *sqlite.Store, categoriesFile string) *Handler {
	return &Handler{store: store, categoriesFile: categoriesFile}
}

// Register wires all routes onto mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/transactions", h.listTransactions)
	mux.HandleFunc("PATCH /api/transactions/{id}", h.updateTransaction)
	mux.HandleFunc("GET /api/summary", h.getSummary)
	mux.HandleFunc("GET /api/categories", h.getCategories)
}

// listTransactions handles GET /api/transactions
// Query params: category, source, from, to, search, limit
func (h *Handler) listTransactions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opts := sqlite.ListOptions{
		Category: q.Get("category"),
		Source:   q.Get("source"),
		Search:   q.Get("search"),
	}

	if s := q.Get("from"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			jsonError(w, "invalid 'from' date, use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		opts.From = &t
	}
	if s := q.Get("to"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			jsonError(w, "invalid 'to' date, use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		opts.To = &t
	}
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			jsonError(w, "invalid 'limit'", http.StatusBadRequest)
			return
		}
		opts.Limit = n
	}
	if s := q.Get("offset"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			jsonError(w, "invalid 'offset'", http.StatusBadRequest)
			return
		}
		opts.Offset = n
	}

	total, err := h.store.Count(r.Context(), opts)
	if err != nil {
		jsonError(w, "failed to count transactions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	txns, err := h.store.List(r.Context(), opts)
	if err != nil {
		jsonError(w, "failed to fetch transactions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	out := make([]txnJSON, 0, len(txns))
	for _, t := range txns {
		out = append(out, txnToJSON(t))
	}

	jsonOK(w, map[string]interface{}{
		"transactions": out,
		"total":        total,
	})
}

// updateTransaction handles PATCH /api/transactions/{id}
func (h *Handler) updateTransaction(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		jsonError(w, "missing transaction id", http.StatusBadRequest)
		return
	}

	var body struct {
		Category string `json:"category"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	body.Category = strings.TrimSpace(body.Category)
	if body.Category == "" {
		jsonError(w, "category must not be empty", http.StatusBadRequest)
		return
	}

	if err := h.store.UpdateCategory(r.Context(), id, body.Category); err != nil {
		if strings.Contains(err.Error(), "not found") {
			jsonError(w, err.Error(), http.StatusNotFound)
		} else {
			jsonError(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	jsonOK(w, map[string]string{"id": id, "category": body.Category})
}

// getSummary handles GET /api/summary
func (h *Handler) getSummary(w http.ResponseWriter, r *http.Request) {
	summaries, err := h.store.Summary(r.Context())
	if err != nil {
		jsonError(w, "failed to fetch summary: "+err.Error(), http.StatusInternalServerError)
		return
	}

	type summaryRow struct {
		Category    string  `json:"category"`
		TotalDebit  float64 `json:"total_debit"`
		TotalCredit float64 `json:"total_credit"`
		NetSpend    float64 `json:"net_spend"`
		Count       int     `json:"count"`
	}

	spending := make([]summaryRow, 0)
	ccPayments := make([]summaryRow, 0)

	for _, s := range summaries {
		row := summaryRow{
			Category:    s.Category,
			TotalDebit:  s.TotalDebit,
			TotalCredit: s.TotalCredit,
			NetSpend:    s.NetSpend,
			Count:       s.Count,
		}
		if s.Category == ccPaymentCategory {
			ccPayments = append(ccPayments, row)
		} else {
			spending = append(spending, row)
		}
	}

	jsonOK(w, map[string]interface{}{
		"spending":    spending,
		"cc_payments": ccPayments,
	})
}

// getCategories handles GET /api/categories
func (h *Handler) getCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := categories.Load(h.categoriesFile)
	if err != nil {
		jsonError(w, "failed to load categories: "+err.Error(), http.StatusInternalServerError)
		return
	}
	jsonOK(w, categories.Names(cats))
}

// CORS wraps a handler to allow cross-origin requests from the dev frontend.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// txnJSON is the wire format sent to the frontend.
type txnJSON struct {
	ID              string  `json:"id"`
	Source          string  `json:"source"`
	TransactionDate string  `json:"transaction_date"`
	ValueDate       *string `json:"value_date,omitempty"`
	Description     string  `json:"description"`
	ReferenceNo     string  `json:"reference_no"`
	Amount          float64 `json:"amount"`
	Type            string  `json:"type"`
	Balance         float64 `json:"balance"`
	Category        string  `json:"category"`
	Hash            string  `json:"hash"`
	CreatedAt       string  `json:"created_at"`
}

func txnToJSON(t *models.Transaction) txnJSON {
	j := txnJSON{
		ID:              t.ID,
		Source:          string(t.Source),
		TransactionDate: t.TransactionDate.Format("2006-01-02"),
		Description:     t.Description,
		ReferenceNo:     t.ReferenceNo,
		Amount:          t.Amount,
		Type:            string(t.Type),
		Balance:         t.Balance,
		Category:        t.Category,
		Hash:            t.Hash,
		CreatedAt:       t.CreatedAt.Format(time.RFC3339),
	}
	if t.ValueDate != nil {
		s := t.ValueDate.Format("2006-01-02")
		j.ValueDate = &s
	}
	return j
}

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}
