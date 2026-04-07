package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"real-time-transaction-processing-system/internal/config"
	"real-time-transaction-processing-system/internal/db"
	"real-time-transaction-processing-system/internal/handlers"
	"real-time-transaction-processing-system/internal/logger"
	"real-time-transaction-processing-system/internal/metrics"
	"real-time-transaction-processing-system/internal/queue"
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
	if err := store.SeedDemoData(ctx, log); err != nil {
		log.Error("seed failed", "error", err)
		os.Exit(1)
	}

	queueClient := queue.NewRedisQueue(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.QueueName)
	defer queueClient.Close()

	metricsCollector := metrics.NewCollector()
	transactionService := services.NewTransactionService(store, queueClient, metricsCollector, log)
	api := handlers.NewAPI(store, queueClient, transactionService, metricsCollector, log)

	server := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      api.Router(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	go func() {
		log.Info("api listening", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("api server failed", "error", err)
			os.Exit(1)
		}
	}()

	waitForShutdown(log, server, cfg.ShutdownGrace)
}

func waitForShutdown(logger *slog.Logger, server *http.Server, timeout time.Duration) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	logger.Info("shutting down api")
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shutdown failed", "error", err)
	}
}
