package services

import (
	"errors"
	"strings"

	"real-time-transaction-processing-system/internal/models"
)

func ValidateCreateTransactionRequest(req models.CreateTransactionRequest) error {
	switch {
	case req.SourceAccountID <= 0:
		return errors.New("source_account_id must be positive")
	case req.DestinationAccountID <= 0:
		return errors.New("destination_account_id must be positive")
	case req.SourceAccountID == req.DestinationAccountID:
		return errors.New("source and destination accounts must differ")
	case req.Amount <= 0:
		return errors.New("amount must be greater than zero")
	case strings.TrimSpace(req.Currency) == "":
		return errors.New("currency is required")
	}
	return nil
}
