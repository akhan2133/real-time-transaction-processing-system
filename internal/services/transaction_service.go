package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"real-time-transaction-processing-system/internal/db"
	"real-time-transaction-processing-system/internal/metrics"
	"real-time-transaction-processing-system/internal/models"
	"real-time-transaction-processing-system/internal/queue"
	"real-time-transaction-processing-system/internal/rules"
)

type TransactionStore interface {
	CreateTransaction(ctx context.Context, req models.CreateTransactionRequest) (models.Transaction, error)
}

type JobQueue interface {
	Enqueue(ctx context.Context, job queue.Job) error
}

type TransactionService struct {
	store   TransactionStore
	queue   JobQueue
	metrics *metrics.Collector
	logger  *slog.Logger
}

func NewTransactionService(store TransactionStore, queue JobQueue, metrics *metrics.Collector, logger *slog.Logger) *TransactionService {
	return &TransactionService{store: store, queue: queue, metrics: metrics, logger: logger}
}

func (s *TransactionService) Create(ctx context.Context, req models.CreateTransactionRequest) (models.Transaction, error) {
	if err := ValidateCreateTransactionRequest(req); err != nil {
		return models.Transaction{}, err
	}

	transaction, err := s.store.CreateTransaction(ctx, req)
	if err != nil {
		return models.Transaction{}, fmt.Errorf("create transaction: %w", err)
	}

	if err := s.queue.Enqueue(ctx, queue.Job{TransactionID: transaction.ID, Attempt: 1}); err != nil {
		return models.Transaction{}, fmt.Errorf("enqueue transaction: %w", err)
	}

	s.metrics.IncReceived()
	s.logger.Info("transaction accepted", "transaction_id", transaction.ID, "source_account_id", transaction.SourceAccountID, "destination_account_id", transaction.DestinationAccountID, "amount", transaction.Amount)
	return transaction, nil
}

type TransactionProcessor struct {
	store       *db.Store
	ruleEngine  RuleEngine
	metrics     *metrics.Collector
	logger      *slog.Logger
	flagOnAlert bool
}

type RuleEngine interface {
	Evaluate(ctx context.Context, q rules.Querier, tx models.Transaction) ([]models.RuleAlert, error)
}

func NewTransactionProcessor(store *db.Store, ruleEngine RuleEngine, metrics *metrics.Collector, logger *slog.Logger, flagOnAlert bool) *TransactionProcessor {
	return &TransactionProcessor{
		store:       store,
		ruleEngine:  ruleEngine,
		metrics:     metrics,
		logger:      logger,
		flagOnAlert: flagOnAlert,
	}
}

func (p *TransactionProcessor) ProcessTransaction(ctx context.Context, transactionID int64) (models.Transaction, []models.Alert, error) {
	started := time.Now()
	tx, err := p.store.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return models.Transaction{}, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	transaction, sourceAccount, destinationAccount, alreadyFinalized, err := p.loadForUpdate(ctx, tx, transactionID)
	if err != nil {
		p.metrics.IncFailed(time.Since(started))
		return models.Transaction{}, nil, err
	}
	if alreadyFinalized {
		if err := tx.Commit(); err != nil {
			return models.Transaction{}, nil, fmt.Errorf("commit finalized transaction: %w", err)
		}
		if transaction.Status == models.TransactionStatusFailed {
			p.metrics.IncFailed(time.Since(started))
		}
		p.logger.Info("transaction already finalized", "transaction_id", transaction.ID, "status", transaction.Status)
		return transaction, nil, nil
	}

	if transaction.Status == models.TransactionStatusCompleted || transaction.Status == models.TransactionStatusFailed || transaction.Status == models.TransactionStatusFlagged {
		if err := tx.Commit(); err != nil {
			return models.Transaction{}, nil, fmt.Errorf("commit finalized transaction: %w", err)
		}
		p.logger.Info("transaction already finalized", "transaction_id", transaction.ID, "status", transaction.Status)
		return transaction, nil, nil
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE transactions SET status = $2, updated_at = NOW()
		WHERE id = $1`, transaction.ID, models.TransactionStatusProcessing); err != nil {
		return models.Transaction{}, nil, fmt.Errorf("mark processing: %w", err)
	}

	if err := validateAccountsForProcessing(sourceAccount, destinationAccount, transaction); err != nil {
		failedTransaction, failureErr := markTransactionFailed(ctx, tx, transaction.ID, err.Error())
		if failureErr != nil {
			return models.Transaction{}, nil, failureErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return models.Transaction{}, nil, commitErr
		}
		p.metrics.IncFailed(time.Since(started))
		return failedTransaction, nil, nil
	}

	if sourceAccount.Balance < transaction.Amount {
		failedTransaction, failureErr := markTransactionFailed(ctx, tx, transaction.ID, "insufficient funds")
		if failureErr != nil {
			return models.Transaction{}, nil, failureErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return models.Transaction{}, nil, commitErr
		}
		p.metrics.IncFailed(time.Since(started))
		return failedTransaction, nil, nil
	}

	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET balance = balance - $2, updated_at = NOW() WHERE id = $1`, sourceAccount.ID, transaction.Amount); err != nil {
		return models.Transaction{}, nil, fmt.Errorf("debit source: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET balance = balance + $2, updated_at = NOW() WHERE id = $1`, destinationAccount.ID, transaction.Amount); err != nil {
		return models.Transaction{}, nil, fmt.Errorf("credit destination: %w", err)
	}

	alertDefs, err := p.ruleEngine.Evaluate(ctx, tx, transaction)
	if err != nil {
		return models.Transaction{}, nil, fmt.Errorf("evaluate rules: %w", err)
	}

	status := models.TransactionStatusCompleted
	if len(alertDefs) > 0 && p.flagOnAlert {
		status = models.TransactionStatusFlagged
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE transactions
		SET status = $2, failure_reason = NULL, processed_at = NOW(), updated_at = NOW()
		WHERE id = $1`, transaction.ID, status); err != nil {
		return models.Transaction{}, nil, fmt.Errorf("update success status: %w", err)
	}

	alerts := make([]models.Alert, 0, len(alertDefs))
	for _, def := range alertDefs {
		alert, err := insertAlert(ctx, tx, transaction.ID, def)
		if err != nil {
			return models.Transaction{}, nil, err
		}
		alerts = append(alerts, alert)
	}

	if err := logProcessingEvents(ctx, tx, transaction.ID, status, len(alerts)); err != nil {
		return models.Transaction{}, nil, err
	}

	finalTransaction, err := getTransactionInTx(ctx, tx, transaction.ID)
	if err != nil {
		return models.Transaction{}, nil, err
	}

	if err := tx.Commit(); err != nil {
		return models.Transaction{}, nil, fmt.Errorf("commit processing: %w", err)
	}

	if len(alerts) > 0 {
		p.metrics.IncFlagged()
	}
	p.metrics.IncSucceeded(time.Since(started))
	p.logger.Info("transaction processed", "transaction_id", transaction.ID, "status", finalTransaction.Status, "alerts", len(alerts))
	return finalTransaction, alerts, nil
}

func (p *TransactionProcessor) loadForUpdate(ctx context.Context, tx *sql.Tx, transactionID int64) (models.Transaction, models.Account, models.Account, bool, error) {
	transaction, err := getTransactionForUpdate(ctx, tx, transactionID)
	if err != nil {
		return models.Transaction{}, models.Account{}, models.Account{}, false, fmt.Errorf("load transaction: %w", err)
	}
	sourceAccount, err := getAccountForUpdate(ctx, tx, transaction.SourceAccountID)
	if err != nil {
		failedTransaction, failureErr := markTransactionFailed(ctx, tx, transaction.ID, "source account not found")
		if failureErr != nil {
			return models.Transaction{}, models.Account{}, models.Account{}, false, failureErr
		}
		return failedTransaction, models.Account{}, models.Account{}, true, nil
	}
	destinationAccount, err := getAccountForUpdate(ctx, tx, transaction.DestinationAccountID)
	if err != nil {
		failedTransaction, failureErr := markTransactionFailed(ctx, tx, transaction.ID, "destination account not found")
		if failureErr != nil {
			return models.Transaction{}, models.Account{}, models.Account{}, false, failureErr
		}
		return failedTransaction, models.Account{}, models.Account{}, true, nil
	}
	return transaction, sourceAccount, destinationAccount, false, nil
}

func validateAccountsForProcessing(source, destination models.Account, transaction models.Transaction) error {
	switch {
	case source.ID == 0:
		return errors.New("source account not found")
	case destination.ID == 0:
		return errors.New("destination account not found")
	case source.ID == destination.ID:
		return errors.New("source and destination accounts must differ")
	case source.Status != models.AccountStatusActive:
		return fmt.Errorf("source account status is %s", source.Status)
	case destination.Status != models.AccountStatusActive:
		return fmt.Errorf("destination account status is %s", destination.Status)
	case source.Currency != destination.Currency:
		return errors.New("account currencies do not match")
	case source.Currency != transaction.Currency:
		return errors.New("transaction currency does not match account currency")
	}
	return nil
}

func getTransactionForUpdate(ctx context.Context, tx *sql.Tx, id int64) (models.Transaction, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, source_account_id, destination_account_id, amount, currency, status, failure_reason, retry_count, created_at, updated_at, processed_at
		FROM transactions
		WHERE id = $1
		FOR UPDATE`, id)
	return scanTransactionRow(row)
}

func getTransactionInTx(ctx context.Context, tx *sql.Tx, id int64) (models.Transaction, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, source_account_id, destination_account_id, amount, currency, status, failure_reason, retry_count, created_at, updated_at, processed_at
		FROM transactions WHERE id = $1`, id)
	return scanTransactionRow(row)
}

func getAccountForUpdate(ctx context.Context, tx *sql.Tx, id int64) (models.Account, error) {
	row := tx.QueryRowContext(ctx, `
		SELECT id, owner_name, balance, currency, status, created_at, updated_at
		FROM accounts WHERE id = $1 FOR UPDATE`, id)
	var account models.Account
	err := row.Scan(&account.ID, &account.OwnerName, &account.Balance, &account.Currency, &account.Status, &account.CreatedAt, &account.UpdatedAt)
	return account, err
}

func markTransactionFailed(ctx context.Context, tx *sql.Tx, id int64, reason string) (models.Transaction, error) {
	if _, err := tx.ExecContext(ctx, `
		UPDATE transactions
		SET status = $2, failure_reason = $3, processed_at = NOW(), updated_at = NOW(), retry_count = retry_count + 1
		WHERE id = $1`, id, models.TransactionStatusFailed, reason); err != nil {
		return models.Transaction{}, fmt.Errorf("mark transaction failed: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO transaction_events (transaction_id, event_type, message)
		VALUES ($1, 'failed', $2)`, id, reason); err != nil {
		return models.Transaction{}, fmt.Errorf("log failed event: %w", err)
	}
	return getTransactionInTx(ctx, tx, id)
}

func insertAlert(ctx context.Context, tx *sql.Tx, transactionID int64, def models.RuleAlert) (models.Alert, error) {
	row := tx.QueryRowContext(ctx, `
		INSERT INTO alerts (transaction_id, rule_triggered, severity, message)
		VALUES ($1, $2, $3, $4)
		RETURNING id, transaction_id, rule_triggered, severity, message, created_at`,
		transactionID, def.RuleTriggered, def.Severity, def.Message)
	var alert models.Alert
	if err := row.Scan(&alert.ID, &alert.TransactionID, &alert.RuleTriggered, &alert.Severity, &alert.Message, &alert.CreatedAt); err != nil {
		return models.Alert{}, fmt.Errorf("insert alert: %w", err)
	}
	return alert, nil
}

func logProcessingEvents(ctx context.Context, tx *sql.Tx, transactionID int64, status string, alertCount int) error {
	message := fmt.Sprintf("transaction finished with status %s and %d alerts", status, alertCount)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO transaction_events (transaction_id, event_type, message)
		VALUES ($1, 'processed', $2)`, transactionID, message)
	return err
}

func scanTransactionRow(row interface {
	Scan(dest ...any) error
}) (models.Transaction, error) {
	var txModel models.Transaction
	var failureReason sql.NullString
	var processedAt sql.NullTime
	if err := row.Scan(&txModel.ID, &txModel.SourceAccountID, &txModel.DestinationAccountID, &txModel.Amount, &txModel.Currency, &txModel.Status, &failureReason, &txModel.RetryCount, &txModel.CreatedAt, &txModel.UpdatedAt, &processedAt); err != nil {
		return models.Transaction{}, err
	}
	if failureReason.Valid {
		txModel.FailureReason = &failureReason.String
	}
	if processedAt.Valid {
		txModel.ProcessedAt = &processedAt.Time
	}
	return txModel, nil
}
