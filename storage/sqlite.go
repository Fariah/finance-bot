package storage

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type Storage struct {
	db *sql.DB
}

// Transaction описує структуру нашої транзакції в БД
type Transaction struct {
	ID          string
	Amount      int64 // Сума в копійках
	Description string
	MCC         int
	Timestamp   int64
	Type        string // "income" або "expense"
}

// NewStorage ініціалізує підключення та створює таблиці, якщо їх немає
func NewStorage(dbPath string) (*Storage, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("помилка відкриття БД: %w", err)
	}

	s := &Storage{db: db}
	if err := s.init(); err != nil {
		return nil, fmt.Errorf("помилка ініціалізації таблиць: %w", err)
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
	`
	_, err := s.db.Exec(query)
	return err
}

// SaveTransaction зберігає транзакцію. ON CONFLICT DO NOTHING захищає від дублів, 
// якщо Монобанк раптом надішле той самий вебхук двічі.
func (s *Storage) SaveTransaction(tx Transaction) error {
	query := `
	INSERT INTO transactions (id, amount, description, mcc, timestamp, type) 
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO NOTHING;
	`
	_, err := s.db.Exec(query, tx.ID, tx.Amount, tx.Description, tx.MCC, tx.Timestamp, tx.Type)
	return err
}

// GetLatestTransactionTimestamp повертає таймстемп останньої збереженої транзакції.
// Якщо транзакцій немає, повертає 0.
func (s *Storage) GetLatestTransactionTimestamp() (int64, error) {
	var timestamp int64
	query := `SELECT COALESCE(MAX(timestamp), 0) FROM transactions;`
	err := s.db.QueryRow(query).Scan(&timestamp)
	if err != nil {
		return 0, err
	}
	return timestamp, nil
}