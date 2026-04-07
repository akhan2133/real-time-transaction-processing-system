//go:build integration

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"real-time-transaction-processing-system/internal/db"
	"real-time-transaction-processing-system/internal/handlers"
	"real-time-transaction-processing-system/internal/logger"
	"real-time-transaction-processing-system/internal/metrics"
	"real-time-transaction-processing-system/internal/models"
	"real-time-transaction-processing-system/internal/queue"
	"real-time-transaction-processing-system/internal/rules"
	"real-time-transaction-processing-system/internal/services"
)

func TestTransactionLifecycleIntegration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	redisAddr := os.Getenv("REDIS_ADDR")
	if databaseURL == "" || redisAddr == "" {
		t.Skip("DATABASE_URL and REDIS_ADDR are required for integration tests")
	}

	ctx := context.Background()
	log := logger.New()

	store, err := db.Open(databaseURL)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.RunMigrations(ctx, log); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	if err := store.ResetForTests(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if err := store.SeedDemoData(ctx, log); err != nil {
		t.Fatalf("seed: %v", err)
	}

	queueClient := queue.NewRedisQueue(redisAddr, "", 0, "integration_transaction_jobs")
	defer queueClient.Close()

	metricsCollector := metrics.NewCollector()
	transactionService := services.NewTransactionService(store, queueClient, metricsCollector, log)
	api := handlers.NewAPI(store, queueClient, transactionService, metricsCollector, log)
	server := httptest.NewServer(api.Router())
	defer server.Close()

	body, _ := json.Marshal(models.CreateTransactionRequest{
		SourceAccountID:      1,
		DestinationAccountID: 2,
		Amount:               6000,
		Currency:             "USD",
	})

	resp, err := http.Post(server.URL+"/transactions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post transaction: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}

	job, err := queueClient.Dequeue(ctx, 0)
	if err != nil {
		t.Fatalf("dequeue job: %v", err)
	}

	processor := services.NewTransactionProcessor(store, rules.NewEngine(5000), metricsCollector, log, true)
	transaction, alerts, err := processor.ProcessTransaction(ctx, job.TransactionID)
	if err != nil {
		t.Fatalf("process transaction: %v", err)
	}

	if transaction.Status != models.TransactionStatusFlagged {
		t.Fatalf("expected flagged transaction, got %s", transaction.Status)
	}
	if len(alerts) == 0 {
		t.Fatalf("expected alerts to be created")
	}
}
