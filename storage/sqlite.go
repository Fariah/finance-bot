package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Storage struct {
	db *sql.DB
}

// Transaction describes the structure of our transaction in the database
type Transaction struct {
	ID          string
	Amount      int64 // Amount in kopiykas
	Description string
	MCC         int
	Timestamp   int64
	Type        string // "income" or "expense"
}

// NewStorage initializes connection and creates tables if they don't exist
func NewStorage(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("error opening database: %w", err)
	}

	s := &Storage{db: db}
	if err := s.init(); err != nil {
		return nil, fmt.Errorf("error initializing tables: %w", err)
	}

	return s, nil
}

func (s *Storage) init() error {
	query := `
	CREATE TABLE IF NOT EXISTS transactions (
		id TEXT PRIMARY KEY,
		amount INTEGER,
		description TEXT,
		mcc INTEGER,
		timestamp INTEGER,
		type TEXT
	);
	
	CREATE TABLE IF NOT EXISTS monthly_stats (
		month TEXT PRIMARY KEY,
		total_income INTEGER DEFAULT 0,
		total_expense INTEGER DEFAULT 0,
		fop_tax_reserved INTEGER DEFAULT 0
	);

	CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS obligations (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		amount INTEGER NOT NULL,
		interval_months INTEGER NOT NULL DEFAULT 1,
		next_due_date TEXT NOT NULL,
		active INTEGER NOT NULL DEFAULT 1
	);

	CREATE TABLE IF NOT EXISTS planned_expenses (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		amount INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		note TEXT,
		created_at INTEGER NOT NULL
	);
	`
	_, err := s.db.Exec(query)
	return err
}

// SetSetting saves an arbitrary setting as a key-value pair
func (s *Storage) SetSetting(key, value string) error {
	query := `
	INSERT INTO settings (key, value) VALUES (?, ?)
	ON CONFLICT(key) DO UPDATE SET value = excluded.value;
	`
	_, err := s.db.Exec(query, key, value)
	return err
}

// GetSetting returns the setting value and a flag indicating whether it exists
func (s *Storage) GetSetting(key string) (string, bool, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?;`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// Obligation describes a recurring mandatory payment (transfers, securities, taxes, installments)
type Obligation struct {
	ID             int64
	Name           string
	Amount         int64 // Amount in kopiykas
	IntervalMonths int   // 1 = monthly, 3 = every 3 months, etc.
	NextDueDate    string
	Active         bool
}

// AddObligation adds a new recurring mandatory payment
func (s *Storage) AddObligation(name string, amount int64, intervalMonths int, nextDueDate string) (int64, error) {
	query := `
	INSERT INTO obligations (name, amount, interval_months, next_due_date, active)
	VALUES (?, ?, ?, ?, 1);
	`
	res, err := s.db.Exec(query, name, amount, intervalMonths, nextDueDate)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListObligations returns mandatory payments. If activeOnly=true, returns only active ones
func (s *Storage) ListObligations(activeOnly bool) ([]Obligation, error) {
	query := `SELECT id, name, amount, interval_months, next_due_date, active FROM obligations`
	if activeOnly {
		query += ` WHERE active = 1`
	}
	query += ` ORDER BY next_due_date ASC;`

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Obligation
	for rows.Next() {
		var o Obligation
		var active int
		if err := rows.Scan(&o.ID, &o.Name, &o.Amount, &o.IntervalMonths, &o.NextDueDate, &active); err != nil {
			return nil, err
		}
		o.Active = active == 1
		result = append(result, o)
	}
	return result, rows.Err()
}

// DeleteObligation permanently deletes a mandatory payment by ID
func (s *Storage) DeleteObligation(id int64) error {
	_, err := s.db.Exec(`DELETE FROM obligations WHERE id = ?;`, id)
	return err
}

// PlannedExpense describes a one-time planned expense (e.g. gift) that the AI analyzes
type PlannedExpense struct {
	ID        int64
	Name      string
	Amount    int64 // Amount in kopiykas
	Status    string
	Note      string
	CreatedAt int64
}

// AddPlannedExpense registers a new planned one-time expense with "pending" status
func (s *Storage) AddPlannedExpense(name string, amount int64, note string) (int64, error) {
	query := `
	INSERT INTO planned_expenses (name, amount, status, note, created_at)
	VALUES (?, ?, 'pending', ?, ?);
	`
	res, err := s.db.Exec(query, name, amount, note, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListPlannedExpenses returns planned expenses with the specified status.
// Empty status returns all planned expenses.
func (s *Storage) ListPlannedExpenses(status string) ([]PlannedExpense, error) {
	query := `SELECT id, name, amount, status, COALESCE(note, ''), created_at FROM planned_expenses`
	args := []any{}
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC;`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []PlannedExpense
	for rows.Next() {
		var p PlannedExpense
		if err := rows.Scan(&p.ID, &p.Name, &p.Amount, &p.Status, &p.Note, &p.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// UpdatePlannedExpenseStatus changes the status of a planned expense (pending/approved/rejected/spent)
func (s *Storage) UpdatePlannedExpenseStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE planned_expenses SET status = ? WHERE id = ?;`, status, id)
	return err
}

// rollForwardObligations advances the next_due_date of each active obligation forward,
// if it falls in a past month relative to now. This avoids having to manually update the date each month.
func (s *Storage) rollForwardObligations(now time.Time) error {
	obligations, err := s.ListObligations(true)
	if err != nil {
		return err
	}

	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	for _, o := range obligations {
		due, err := time.Parse("2006-01-02", o.NextDueDate)
		if err != nil {
			continue
		}

		changed := false
		for due.Before(monthStart) {
			due = due.AddDate(0, o.IntervalMonths, 0)
			changed = true
		}

		if changed {
			if _, err := s.db.Exec(`UPDATE obligations SET next_due_date = ? WHERE id = ?;`, due.Format("2006-01-02"), o.ID); err != nil {
				return err
			}
		}
	}

	return nil
}

// ObligationsDueThisMonth returns active obligations whose due date falls in the current month (relative to now).
// Before checking, automatically advances overdue dates forward by the required number of intervals.
func (s *Storage) ObligationsDueThisMonth(now time.Time) ([]Obligation, error) {
	if err := s.rollForwardObligations(now); err != nil {
		return nil, err
	}

	obligations, err := s.ListObligations(true)
	if err != nil {
		return nil, err
	}

	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	monthEnd := monthStart.AddDate(0, 1, 0)

	var due []Obligation
	for _, o := range obligations {
		d, err := time.Parse("2006-01-02", o.NextDueDate)
		if err != nil {
			continue
		}
		if !d.Before(monthStart) && d.Before(monthEnd) {
			due = append(due, o)
		}
	}

	return due, nil
}

// SumTransactionsInRange sums the amounts of transactions of the specified type ("income"/"expense") in the range [from, to)
func (s *Storage) SumTransactionsInRange(from, to int64, txType string) (int64, error) {
	var sum int64
	query := `SELECT COALESCE(SUM(amount), 0) FROM transactions WHERE timestamp >= ? AND timestamp < ? AND type = ?;`
	err := s.db.QueryRow(query, from, to, txType).Scan(&sum)
	return sum, err
}

// SumPlannedExpensesByStatus sums the amounts of planned expenses with the given statuses
func (s *Storage) SumPlannedExpensesByStatus(statuses ...string) (int64, error) {
	if len(statuses) == 0 {
		return 0, nil
	}

	placeholders := make([]string, len(statuses))
	args := make([]any, len(statuses))
	for i, st := range statuses {
		placeholders[i] = "?"
		args[i] = st
	}

	query := fmt.Sprintf(`SELECT COALESCE(SUM(amount), 0) FROM planned_expenses WHERE status IN (%s);`, strings.Join(placeholders, ","))

	var sum int64
	err := s.db.QueryRow(query, args...).Scan(&sum)
	return sum, err
}

// SaveTransaction saves a transaction. ON CONFLICT DO NOTHING protects against duplicates,
// in case Monobank accidentally sends the same webhook twice.
func (s *Storage) SaveTransaction(tx Transaction) error {
	query := `
	INSERT INTO transactions (id, amount, description, mcc, timestamp, type)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO NOTHING;
	`
	_, err := s.db.Exec(query, tx.ID, tx.Amount, tx.Description, tx.MCC, tx.Timestamp, tx.Type)
	return err
}

// GetLatestTransactionTimestamp returns the timestamp of the last saved transaction.
// If there are no transactions, returns 0.
func (s *Storage) GetLatestTransactionTimestamp() (int64, error) {
	var timestamp int64
	query := `SELECT COALESCE(MAX(timestamp), 0) FROM transactions;`
	err := s.db.QueryRow(query).Scan(&timestamp)
	if err != nil {
		return 0, err
	}
	return timestamp, nil
}