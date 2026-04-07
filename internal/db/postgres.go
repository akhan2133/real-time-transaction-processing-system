package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"real-time-transaction-processing-system/internal/models"
)

type Store struct {
	DB *sql.DB
}

func Open(databaseURL string) (*Store, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	return &Store{DB: db}, nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.DB.PingContext(ctx)
}

func (s *Store) Close() error {
	return s.DB.Close()
}

func (s *Store) CreateTransaction(ctx context.Context, req models.CreateTransactionRequest) (models.Transaction, error) {
	query := `
		INSERT INTO transactions (source_account_id, destination_account_id, amount, currency, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, source_account_id, destination_account_id, amount, currency, status, failure_reason, retry_count, created_at, updated_at, processed_at`
	row := s.DB.QueryRowContext(ctx, query, req.SourceAccountID, req.DestinationAccountID, req.Amount, strings.ToUpper(req.Currency), models.TransactionStatusPending)
	return scanTransaction(row)
}

func (s *Store) GetTransaction(ctx context.Context, id int64) (models.Transaction, error) {
	query := `
		SELECT id, source_account_id, destination_account_id, amount, currency, status, failure_reason, retry_count, created_at, updated_at, processed_at
		FROM transactions WHERE id = $1`
	row := s.DB.QueryRowContext(ctx, query, id)
	return scanTransaction(row)
}

func (s *Store) ListTransactions(ctx context.Context, limit, offset int) ([]models.Transaction, error) {
	query := `
		SELECT id, source_account_id, destination_account_id, amount, currency, status, failure_reason, retry_count, created_at, updated_at, processed_at
		FROM transactions
		ORDER BY id DESC
		LIMIT $1 OFFSET $2`
	rows, err := s.DB.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list transactions: %w", err)
	}
	defer rows.Close()

	var transactions []models.Transaction
	for rows.Next() {
		tx, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		transactions = append(transactions, tx)
	}
	return transactions, rows.Err()
}

func (s *Store) ListAccounts(ctx context.Context) ([]models.Account, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, owner_name, balance, currency, status, created_at, updated_at
		FROM accounts ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close()

	var accounts []models.Account
	for rows.Next() {
		var account models.Account
		if err := rows.Scan(&account.ID, &account.OwnerName, &account.Balance, &account.Currency, &account.Status, &account.CreatedAt, &account.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func (s *Store) GetAccount(ctx context.Context, id int64) (models.Account, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, owner_name, balance, currency, status, created_at, updated_at
		FROM accounts WHERE id = $1`, id)

	var account models.Account
	if err := row.Scan(&account.ID, &account.OwnerName, &account.Balance, &account.Currency, &account.Status, &account.CreatedAt, &account.UpdatedAt); err != nil {
		return models.Account{}, err
	}
	return account, nil
}

func (s *Store) ListAlerts(ctx context.Context) ([]models.Alert, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, transaction_id, rule_triggered, severity, message, created_at
		FROM alerts ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	defer rows.Close()

	var alerts []models.Alert
	for rows.Next() {
		var alert models.Alert
		if err := rows.Scan(&alert.ID, &alert.TransactionID, &alert.RuleTriggered, &alert.Severity, &alert.Message, &alert.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan alert: %w", err)
		}
		alerts = append(alerts, alert)
	}
	return alerts, rows.Err()
}

func (s *Store) GetAlert(ctx context.Context, id int64) (models.Alert, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, transaction_id, rule_triggered, severity, message, created_at
		FROM alerts WHERE id = $1`, id)
	var alert models.Alert
	if err := row.Scan(&alert.ID, &alert.TransactionID, &alert.RuleTriggered, &alert.Severity, &alert.Message, &alert.CreatedAt); err != nil {
		return models.Alert{}, err
	}
	return alert, nil
}

func (s *Store) LogEvent(ctx context.Context, transactionID int64, eventType, message string) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO transaction_events (transaction_id, event_type, message)
		VALUES ($1, $2, $3)`, transactionID, eventType, message)
	return err
}

type Processor interface {
	ProcessTransaction(ctx context.Context, transactionID int64) (models.Transaction, []models.Alert, error)
}

func scanTransaction(scanner interface {
	Scan(dest ...any) error
}) (models.Transaction, error) {
	var tx models.Transaction
	var failureReason sql.NullString
	var processedAt sql.NullTime
	if err := scanner.Scan(
		&tx.ID,
		&tx.SourceAccountID,
		&tx.DestinationAccountID,
		&tx.Amount,
		&tx.Currency,
		&tx.Status,
		&failureReason,
		&tx.RetryCount,
		&tx.CreatedAt,
		&tx.UpdatedAt,
		&processedAt,
	); err != nil {
		return models.Transaction{}, err
	}
	if failureReason.Valid {
		tx.FailureReason = &failureReason.String
	}
	if processedAt.Valid {
		tx.ProcessedAt = &processedAt.Time
	}
	return tx, nil
}

var ErrNotFound = errors.New("not found")
