# Real-Time Transaction Processing System

Production-style backend portfolio built in Go that simulates asynchronous financial transaction processing with PostgreSQL, Redis, Docker Compose, structured logging, fraud/risk alerting, health checks, and metrics.

## Overview

This project models a small transaction-processing platform with an HTTP API and a background worker. Transactions are accepted synchronously, persisted as `pending`, pushed onto a Redis-backed queue, and finalized asynchronously by a worker that performs atomic balance updates inside PostgreSQL transactions.

The design intentionally focuses on backend engineering concerns that are valuable for backend/distributed systems:

- asynchronous job processing
- transactional consistency
- duplicate-processing protection
- modular fraud/risk detection
- observability through logs, readiness checks, and metrics
- local reproducibility with Docker Compose

## Architecture

```text
Client
  |
  v
REST API (Go)
  | \
  |  \-> PostgreSQL (transactions, accounts, alerts, events)
  |
  \----> Redis queue (transaction_jobs)
              |
              v
         Worker (Go)
              |
              v
        PostgreSQL updates balances, statuses, alerts, events
              |
              v
        Metrics / Health / Readiness / Structured Logs
```

## Design Choices

- The API returns `202 Accepted` after persisting and queueing a transaction.
- The worker is responsible for final validation that depends on current account state.
- Fraud/risk alerts are rule-based and modular.
- If a transaction triggers fraud/risk rules after a successful funds transfer, the system marks it as `flagged`.
  The transfer still completes atomically, but the status highlights that review is needed.
- PostgreSQL row-level locks (`FOR UPDATE`) prevent partial balance updates and reduce duplicate-processing risk.
- The worker retries unexpected processing errors by re-enqueueing the job with an incremented attempt count.

## Tech Stack

- Go
- PostgreSQL
- Redis
- Docker Compose
- REST API with `net/http`
- JSON structured logging with `log/slog`
- Unit tests and integration-style tests

## Project Structure

```text
cmd/api
cmd/worker
internal/config
internal/db
internal/handlers
internal/logger
internal/metrics
internal/models
internal/queue
internal/rules
internal/services
internal/tests
migrations
scripts
```

## Running Locally

### Prerequisites

- Docker
- Docker Compose

### Start the full system

```bash
docker compose up --build
```

The API will be available at `http://localhost:8080`.

Seed accounts are created automatically on startup:

- Alice: 12000 USD
- Bob: 6500 USD
- Carol: 4000 USD
- Dave: 1500 USD

## API Endpoints

### Transactions

- `POST /transactions`
- `GET /transactions`
- `GET /transactions/{id}`

### Accounts

- `GET /accounts`
- `GET /accounts/{id}`

### Alerts

- `GET /alerts`
- `GET /alerts/{id}`

### Health and Metrics

- `GET /health`
- `GET /ready`
- `GET /metrics`

## Example Requests

### Create a transaction

```bash
curl -X POST http://localhost:8080/transactions \
  -H "Content-Type: application/json" \
  -d '{
    "source_account_id": 1,
    "destination_account_id": 2,
    "amount": 250.00,
    "currency": "USD"
  }'
```

Example response:

```json
{
  "id": 1,
  "status": "pending"
}
```

### Check a transaction

```bash
curl http://localhost:8080/transactions/1
```

### List accounts

```bash
curl http://localhost:8080/accounts
```

### List alerts

```bash
curl http://localhost:8080/alerts
```

### Readiness

```bash
curl http://localhost:8080/ready
```

### Metrics

```bash
curl http://localhost:8080/metrics
```

## Async Processing Flow

1. Client sends `POST /transactions`.
2. API validates the payload shape and inserts a `pending` transaction row.
3. API pushes the transaction ID into Redis.
4. Worker pops the job from Redis.
5. Worker locks the transaction and related accounts inside a PostgreSQL transaction.
6. Worker validates balances, statuses, and currencies.
7. Worker updates balances atomically or marks the transaction as failed with a reason.
8. Fraud/risk rules run against recent transaction history.
9. Alerts are written when rules trigger.
10. Metrics and structured logs capture processing outcomes.

## Fraud / Risk Rules

The rule engine is designed to be easy to extend. Current rules:

- Large transfer threshold
- Rapid repeated transfers from the same source account within a short time window
- Repeated failed transactions from the same source account within a time window
- First-time high-value transfer to a new destination

Each triggered rule creates an alert row linked to the transaction.

## Demo Scenarios

The included [demo.sh](/home/asfand-khan/Documents/real-time-transaction-processing-system/scripts/demo.sh) script exercises:

- successful transfer
- insufficient funds
- suspicious large transfer
- repeated rapid transfers

Run it with:

```bash
sh scripts/demo.sh
```

## Testing

Run unit and package tests:

```bash
GOCACHE=/tmp/gocache go test ./...
```

Run integration-style tests against a live PostgreSQL and Redis instance:

```bash
DATABASE_URL=postgres://postgres:postgres@localhost:5432/transactions?sslmode=disable \
REDIS_ADDR=localhost:6379 \
GOCACHE=/tmp/gocache \
go test ./internal/tests -tags=integration
```

## Highlights

- Built a Dockerized transaction processing backend in Go with PostgreSQL and Redis-backed asynchronous workers.
- Implemented atomic balance updates, failure handling, and rule-based fraud alerting for real-time transaction workflows.
- Added structured logging, health checks, and metrics to monitor processing latency, failures, and suspicious activity.

## Future Improvements

- Prometheus client integration and richer histograms
- Dead-letter queue support
- Account-level rate limiting
- Idempotency keys for client requests
- Horizontal worker scaling benchmarks
- OpenTelemetry tracing
- Admin review workflow for flagged transfers

## License
This project is licensed under the [MIT License](LICENSE)
