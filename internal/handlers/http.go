package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"real-time-transaction-processing-system/internal/db"
	"real-time-transaction-processing-system/internal/metrics"
	"real-time-transaction-processing-system/internal/models"
	"real-time-transaction-processing-system/internal/queue"
	"real-time-transaction-processing-system/internal/services"
)

type API struct {
	store      *db.Store
	queue      *queue.RedisQueue
	service    *services.TransactionService
	metrics    *metrics.Collector
	logger     *slog.Logger
	httpServer *http.Server
}

func NewAPI(store *db.Store, queue *queue.RedisQueue, service *services.TransactionService, metrics *metrics.Collector, logger *slog.Logger) *API {
	return &API{
		store:   store,
		queue:   queue,
		service: service,
		metrics: metrics,
		logger:  logger,
	}
}

func (a *API) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transactions", a.handleCreateTransaction)
	mux.HandleFunc("GET /transactions", a.handleListTransactions)
	mux.HandleFunc("GET /transactions/", a.handleGetTransaction)
	mux.HandleFunc("GET /accounts", a.handleListAccounts)
	mux.HandleFunc("GET /accounts/", a.handleGetAccount)
	mux.HandleFunc("GET /alerts", a.handleListAlerts)
	mux.HandleFunc("GET /alerts/", a.handleGetAlert)
	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("GET /ready", a.handleReady)
	mux.HandleFunc("GET /metrics", a.handleMetrics)

	return a.loggingMiddleware(mux)
}

func (a *API) handleCreateTransaction(w http.ResponseWriter, r *http.Request) {
	var req models.CreateTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}
	transaction, err := a.service.Create(r.Context(), req)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, models.CreateTransactionResponse{ID: transaction.ID, Status: transaction.Status})
}

func (a *API) handleGetTransaction(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.URL.Path, "/transactions/")
	if !ok {
		return
	}
	transaction, err := a.store.GetTransaction(r.Context(), id)
	if err != nil {
		writeFetchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, transaction)
}

func (a *API) handleListTransactions(w http.ResponseWriter, r *http.Request) {
	limit := parseIntWithDefault(r.URL.Query().Get("limit"), 20)
	offset := parseIntWithDefault(r.URL.Query().Get("offset"), 0)
	transactions, err := a.store.ListTransactions(r.Context(), limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list transactions"})
		return
	}
	writeJSON(w, http.StatusOK, transactions)
}

func (a *API) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	accounts, err := a.store.ListAccounts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list accounts"})
		return
	}
	writeJSON(w, http.StatusOK, accounts)
}

func (a *API) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.URL.Path, "/accounts/")
	if !ok {
		return
	}
	account, err := a.store.GetAccount(r.Context(), id)
	if err != nil {
		writeFetchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, account)
}

func (a *API) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	alerts, err := a.store.ListAlerts(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list alerts"})
		return
	}
	writeJSON(w, http.StatusOK, alerts)
}

func (a *API) handleGetAlert(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(w, r.URL.Path, "/alerts/")
	if !ok {
		return
	}
	alert, err := a.store.GetAlert(r.Context(), id)
	if err != nil {
		writeFetchError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, alert)
}

func (a *API) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := a.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "database": "down"})
		return
	}
	if err := a.queue.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "degraded", "redis": "down"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (a *API) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(a.metrics.RenderPrometheus()))
}

func (a *API) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		a.logger.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"duration_ms", time.Since(started).Milliseconds(),
		)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "resource not found"})
	default:
		if strings.Contains(err.Error(), "must") || strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "invalid") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

func writeFetchError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "resource not found"})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "query failed"})
}

func parseID(w http.ResponseWriter, path, prefix string) (int64, bool) {
	raw := strings.TrimPrefix(path, prefix)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return 0, false
	}
	return id, true
}

func parseIntWithDefault(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
