package services

import (
	"testing"

	"real-time-transaction-processing-system/internal/models"
)

func TestValidateAccountsForProcessing(t *testing.T) {
	source := models.Account{ID: 1, Status: models.AccountStatusActive, Currency: "USD", Balance: 100}
	destination := models.Account{ID: 2, Status: models.AccountStatusActive, Currency: "USD", Balance: 50}
	transaction := models.Transaction{SourceAccountID: 1, DestinationAccountID: 2, Currency: "USD", Amount: 40}

	if err := validateAccountsForProcessing(source, destination, transaction); err != nil {
		t.Fatalf("expected valid accounts, got %v", err)
	}
}

func TestValidateAccountsForProcessingCurrencyMismatch(t *testing.T) {
	source := models.Account{ID: 1, Status: models.AccountStatusActive, Currency: "USD"}
	destination := models.Account{ID: 2, Status: models.AccountStatusActive, Currency: "EUR"}
	transaction := models.Transaction{SourceAccountID: 1, DestinationAccountID: 2, Currency: "USD", Amount: 40}

	if err := validateAccountsForProcessing(source, destination, transaction); err == nil {
		t.Fatalf("expected currency mismatch error")
	}
}
