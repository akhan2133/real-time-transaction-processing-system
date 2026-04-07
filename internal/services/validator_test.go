package services

import (
	"testing"

	"real-time-transaction-processing-system/internal/models"
)

func TestValidateCreateTransactionRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     models.CreateTransactionRequest
		wantErr bool
	}{
		{
			name: "valid request",
			req: models.CreateTransactionRequest{
				SourceAccountID:      1,
				DestinationAccountID: 2,
				Amount:               100,
				Currency:             "USD",
			},
		},
		{
			name: "same account",
			req: models.CreateTransactionRequest{
				SourceAccountID:      1,
				DestinationAccountID: 1,
				Amount:               100,
				Currency:             "USD",
			},
			wantErr: true,
		},
		{
			name: "missing currency",
			req: models.CreateTransactionRequest{
				SourceAccountID:      1,
				DestinationAccountID: 2,
				Amount:               100,
			},
			wantErr: true,
		},
		{
			name: "negative amount",
			req: models.CreateTransactionRequest{
				SourceAccountID:      1,
				DestinationAccountID: 2,
				Amount:               -10,
				Currency:             "USD",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCreateTransactionRequest(tt.req)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}
