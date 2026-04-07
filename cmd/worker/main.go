package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"real-time-transaction-processing-system/internal/config"
	"real-time-transaction-processing-system/internal/db"
	"real-time-transaction-processing-system/internal/logger"
	"real-time-transaction-processing-system/internal/metrics"
	"real-time-transaction-processing-system/internal/models"
	"real-time-transaction-processing-system/internal/queue"
	"real-time-transaction-processing-system/internal/rules"
	"real-time-transaction-processing-system/internal/services"
)

func main() {
	cfg := config.Load()
	log := logger.New()

	store, err := db.Open(cfg.DatabaseURL)
	if err != nil {
		log.Error("database setup failed", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.RunMigrations(ctx, log); err != nil {
		log.Error("migrations failed", "error", err)
		os.Exit(1)
	}

	queueClient := queue.NewRedisQueue(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.QueueName)
	defer queueClient.Close()

	processor := services.NewTransactionProcessor(store, rules.NewEngine(cfg.LargeTransfer), metrics.NewCollector(), log, true)

	workerCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log.Info("worker started", "queue", cfg.QueueName)
	runWorker(workerCtx, queueClient, processor, cfg.RetryLimit, log)
}

func runWorker(ctx context.Context, q *queue.RedisQueue, processor *services.TransactionProcessor, retryLimit int, log *slog.Logger) {
	for {
		select {
		case <-ctx.Done():
			log.Info("worker shutting down")
			return
		default:
		}

		job, err := q.Dequeue(ctx, 2*time.Second)
		if err != nil {
			if errors.Is(err, redis.Nil) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				continue
			}
			log.Error("queue pop failed", "error", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		transaction, _, err := processor.ProcessTransaction(ctx, job.TransactionID)
		if err != nil {
			log.Error("processing failed", "transaction_id", job.TransactionID, "attempt", job.Attempt, "error", err)
			if job.Attempt < retryLimit {
				_ = q.Enqueue(ctx, queue.Job{TransactionID: job.TransactionID, Attempt: job.Attempt + 1})
			}
			continue
		}

		log.Info("job complete", "transaction_id", transaction.ID, "status", transaction.Status)
		if transaction.Status == models.TransactionStatusPending || transaction.Status == models.TransactionStatusProcessing {
			_ = q.Enqueue(ctx, queue.Job{TransactionID: job.TransactionID, Attempt: job.Attempt + 1})
		}
	}
}
