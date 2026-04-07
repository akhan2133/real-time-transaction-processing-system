package rules

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"real-time-transaction-processing-system/internal/models"
)

type Rule interface {
	Name() string
	Evaluate(ctx context.Context, q Querier, tx models.Transaction) (*models.RuleAlert, error)
}

type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Engine struct {
	rules []Rule
}

func NewEngine(largeTransferThreshold float64) *Engine {
	return &Engine{
		rules: []Rule{
			LargeTransferRule{Threshold: largeTransferThreshold},
			RapidTransferRule{Window: 2 * time.Minute, ThresholdCount: 3},
			RepeatedFailuresRule{Window: 5 * time.Minute, ThresholdCount: 3},
			NewDestinationHighValueRule{Threshold: largeTransferThreshold / 2},
		},
	}
}

func (e *Engine) Evaluate(ctx context.Context, q Querier, tx models.Transaction) ([]models.RuleAlert, error) {
	alerts := make([]models.RuleAlert, 0)
	for _, rule := range e.rules {
		alert, err := rule.Evaluate(ctx, q, tx)
		if err != nil {
			return nil, fmt.Errorf("evaluate %s: %w", rule.Name(), err)
		}
		if alert != nil {
			alerts = append(alerts, *alert)
		}
	}
	return alerts, nil
}

type LargeTransferRule struct {
	Threshold float64
}

func (r LargeTransferRule) Name() string { return "large_transfer_threshold" }

func (r LargeTransferRule) Evaluate(_ context.Context, _ Querier, tx models.Transaction) (*models.RuleAlert, error) {
	if tx.Amount < r.Threshold {
		return nil, nil
	}
	return &models.RuleAlert{
		RuleTriggered: r.Name(),
		Severity:      "high",
		Message:       fmt.Sprintf("transaction amount %.2f exceeded threshold %.2f", tx.Amount, r.Threshold),
	}, nil
}

type RapidTransferRule struct {
	Window         time.Duration
	ThresholdCount int
}

func (r RapidTransferRule) Name() string { return "rapid_repeated_transfers" }

func (r RapidTransferRule) Evaluate(ctx context.Context, q Querier, tx models.Transaction) (*models.RuleAlert, error) {
	var count int
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM transactions
		WHERE source_account_id = $1
		  AND created_at >= NOW() - $2::interval`,
		tx.SourceAccountID, durationToInterval(r.Window)).Scan(&count)
	if err != nil {
		return nil, err
	}
	if count < r.ThresholdCount {
		return nil, nil
	}
	return &models.RuleAlert{
		RuleTriggered: r.Name(),
		Severity:      "medium",
		Message:       fmt.Sprintf("account %d submitted %d transfers within %s", tx.SourceAccountID, count, r.Window),
	}, nil
}

type RepeatedFailuresRule struct {
	Window         time.Duration
	ThresholdCount int
}

func (r RepeatedFailuresRule) Name() string { return "repeated_failed_transactions" }

func (r RepeatedFailuresRule) Evaluate(ctx context.Context, q Querier, tx models.Transaction) (*models.RuleAlert, error) {
	var count int
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM transactions
		WHERE source_account_id = $1
		  AND status = 'failed'
		  AND created_at >= NOW() - $2::interval`,
		tx.SourceAccountID, durationToInterval(r.Window)).Scan(&count)
	if err != nil {
		return nil, err
	}
	if count < r.ThresholdCount {
		return nil, nil
	}
	return &models.RuleAlert{
		RuleTriggered: r.Name(),
		Severity:      "medium",
		Message:       fmt.Sprintf("account %d had %d failed transactions within %s", tx.SourceAccountID, count, r.Window),
	}, nil
}

type NewDestinationHighValueRule struct {
	Threshold float64
}

func (r NewDestinationHighValueRule) Name() string { return "new_destination_high_value" }

func (r NewDestinationHighValueRule) Evaluate(ctx context.Context, q Querier, tx models.Transaction) (*models.RuleAlert, error) {
	if tx.Amount < r.Threshold {
		return nil, nil
	}
	var count int
	err := q.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM transactions
		WHERE source_account_id = $1
		  AND destination_account_id = $2
		  AND id <> $3`,
		tx.SourceAccountID, tx.DestinationAccountID, tx.ID).Scan(&count)
	if err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, nil
	}
	return &models.RuleAlert{
		RuleTriggered: r.Name(),
		Severity:      "high",
		Message:       fmt.Sprintf("first-time destination %d for source account %d on amount %.2f", tx.DestinationAccountID, tx.SourceAccountID, tx.Amount),
	}, nil
}

func durationToInterval(d time.Duration) string {
	seconds := int(d.Seconds())
	return fmt.Sprintf("%d seconds", seconds)
}
