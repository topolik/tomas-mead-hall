// Package usage persists one row per LLM request (tokens, cost, latency, which
// impl served it, success/failure) in SQLite and exposes an aggregation for
// /admin/usage.
package usage

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver (pure Go, no cgo)
)

// Store wraps the SQLite handle.
type Store struct{ db *sql.DB }

// Record is one request's accounting row.
type Record struct {
	Agent            string
	RequestedModel   string
	ImplUsed         string
	PromptTokens     int
	CompletionTokens int
	CostUSD          float64
	LatencyMS        int64
	Status           string // "ok" | "error"
	Error            string
	PromptPreview    string // truncated request text (empty when content preview disabled)
	ResponsePreview  string // truncated response text
}

const schema = `
CREATE TABLE IF NOT EXISTS usage (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  ts                TEXT    NOT NULL,
  agent             TEXT    NOT NULL,
  requested_model   TEXT    NOT NULL,
  impl_used         TEXT    NOT NULL,
  prompt_tokens     INTEGER NOT NULL,
  completion_tokens INTEGER NOT NULL,
  cost_usd          REAL    NOT NULL,
  latency_ms        INTEGER NOT NULL,
  status            TEXT    NOT NULL,
  error             TEXT    NOT NULL DEFAULT '',
  prompt_preview    TEXT    NOT NULL DEFAULT '',
  response_preview  TEXT    NOT NULL DEFAULT ''
);`

// addColumnIfMissing migrates an older usage table that predates a column.
// table/col are package constants (never user input), so the concatenation is safe.
func addColumnIfMissing(db *sql.DB, table, col, decl string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == col {
			return nil // already present
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + col + " " + decl)
	return err
}

// Open opens (creating if needed) the SQLite database at path with WAL enabled.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA busy_timeout=5000;",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma: %w", err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	// Migrate DBs created before the preview columns existed.
	for _, col := range [][2]string{
		{"prompt_preview", "TEXT NOT NULL DEFAULT ''"},
		{"response_preview", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := addColumnIfMissing(db, "usage", col[0], col[1]); err != nil {
			db.Close()
			return nil, fmt.Errorf("migrate %s: %w", col[0], err)
		}
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// Record inserts one accounting row, stamping ts with the current UTC time.
func (s *Store) Record(r Record) error {
	_, err := s.db.Exec(
		`INSERT INTO usage
		 (ts, agent, requested_model, impl_used, prompt_tokens, completion_tokens, cost_usd, latency_ms, status, error, prompt_preview, response_preview)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		time.Now().UTC().Format(time.RFC3339), r.Agent, r.RequestedModel, r.ImplUsed,
		r.PromptTokens, r.CompletionTokens, r.CostUSD, r.LatencyMS, r.Status, r.Error,
		r.PromptPreview, r.ResponsePreview,
	)
	return err
}

// AggRow is one grouped (day, agent, impl) usage summary.
type AggRow struct {
	Day              string  `json:"day"`
	Agent            string  `json:"agent"`
	Impl             string  `json:"impl"`
	Requests         int     `json:"requests"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	Errors           int     `json:"errors"`
}

// Aggregate returns usage grouped by day, agent, and impl, newest day first.
func (s *Store) Aggregate() ([]AggRow, error) {
	rows, err := s.db.Query(`
		SELECT substr(ts,1,10) AS day, agent, impl_used,
		       COUNT(*),
		       COALESCE(SUM(prompt_tokens),0),
		       COALESCE(SUM(completion_tokens),0),
		       COALESCE(SUM(cost_usd),0),
		       SUM(CASE WHEN status='error' THEN 1 ELSE 0 END)
		FROM usage
		GROUP BY day, agent, impl_used
		ORDER BY day DESC, agent, impl_used`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AggRow
	for rows.Next() {
		var a AggRow
		if err := rows.Scan(&a.Day, &a.Agent, &a.Impl, &a.Requests, &a.PromptTokens, &a.CompletionTokens, &a.CostUSD, &a.Errors); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RecentRow is one request row for the recent-calls log (newest first).
type RecentRow struct {
	Ts               string  `json:"ts"`
	Agent            string  `json:"agent"`
	RequestedModel   string  `json:"requested_model"`
	ImplUsed         string  `json:"impl_used"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	LatencyMS        int64   `json:"latency_ms"`
	Status           string  `json:"status"`
	Error            string  `json:"error"`
	PromptPreview    string  `json:"prompt_preview"`
	ResponsePreview  string  `json:"response_preview"`
}

// Recent returns the most recent request rows, newest first. limit is clamped
// to [1, 1000] with a default of 50.
func (s *Store) Recent(limit int) ([]RecentRow, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.Query(`
		SELECT ts, agent, requested_model, impl_used, prompt_tokens, completion_tokens, cost_usd, latency_ms, status, error, prompt_preview, response_preview
		FROM usage ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecentRow
	for rows.Next() {
		var r RecentRow
		if err := rows.Scan(&r.Ts, &r.Agent, &r.RequestedModel, &r.ImplUsed, &r.PromptTokens, &r.CompletionTokens, &r.CostUSD, &r.LatencyMS, &r.Status, &r.Error, &r.PromptPreview, &r.ResponsePreview); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Cost computes USD cost given per-1M-token prices. CLI impls price at 0, so
// their cost is always 0 regardless of estimated token counts.
func Cost(promptTokens, completionTokens int, pricePer1MIn, pricePer1MOut float64) float64 {
	return (float64(promptTokens)*pricePer1MIn + float64(completionTokens)*pricePer1MOut) / 1_000_000.0
}
