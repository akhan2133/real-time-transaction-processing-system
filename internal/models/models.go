package models

import "time"

const (
	AccountStatusActive   = "active"
	AccountStatusFrozen   = "frozen"
	AccountStatusDisabled = "disabled"

	TransactionStatusPending    = "pending"
	TransactionStatusProcessing = "processing"
	TransactionStatusCompleted  = "completed"
	TransactionStatusFailed     = "failed"
	TransactionStatusFlagged    = "flagged"
)

type Account struct {
	ID        int64     `json:"id"`
	OwnerName string    `json:"owner_name"`
	Balance   float64   `json:"balance"`
	Currency  string    `json:"currency"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Transaction struct {
	ID                   int64      `json:"id"`
	SourceAccountID      int64      `json:"source_account_id"`
	DestinationAccountID int64      `json:"destination_account_id"`
	Amount               float64    `json:"amount"`
	Currency             string     `json:"currency"`
	Status               string     `json:"status"`
	FailureReason        *string    `json:"failure_reason,omitempty"`
	RetryCount           int        `json:"retry_count"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	ProcessedAt          *time.Time `json:"processed_at,omitempty"`
}

type Alert struct {
	ID            int64     `json:"id"`
	TransactionID int64     `json:"transaction_id"`
	RuleTriggered string    `json:"rule_triggered"`
	Severity      string    `json:"severity"`
	Message       string    `json:"message"`
	CreatedAt     time.Time `json:"created_at"`
}

type TransactionEvent struct {
	ID            int64     `json:"id"`
	TransactionID int64     `json:"transaction_id"`
	EventType     string    `json:"event_type"`
	Message       string    `json:"message"`
	CreatedAt     time.Time `json:"created_at"`
}

type CreateTransactionRequest struct {
	SourceAccountID      int64   `json:"source_account_id"`
	DestinationAccountID int64   `json:"destination_account_id"`
	Amount               float64 `json:"amount"`
	Currency             string  `json:"currency"`
}

type CreateTransactionResponse struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

type RuleAlert struct {
	RuleTriggered string
	Severity      string
	Message       string
}
