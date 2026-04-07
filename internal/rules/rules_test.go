package rules

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"real-time-transaction-processing-system/internal/models"
)

func TestLargeTransferRule(t *testing.T) {
	rule := LargeTransferRule{Threshold: 5000}
	alert, err := rule.Evaluate(context.Background(), nil, models.Transaction{Amount: 7000})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alert == nil {
		t.Fatalf("expected alert for large transfer")
	}
}

func TestRapidTransferRule(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(*)
		FROM transactions
		WHERE source_account_id = $1
		  AND created_at >= NOW() - $2::interval`)).
		WithArgs(int64(1), "120 seconds").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	rule := RapidTransferRule{Window: 2 * time.Minute, ThresholdCount: 3}
	alert, err := rule.Evaluate(context.Background(), wrapDB(db), models.Transaction{SourceAccountID: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alert == nil {
		t.Fatalf("expected rapid transfer alert")
	}
}

func TestRepeatedFailuresRule(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(*)
		FROM transactions
		WHERE source_account_id = $1
		  AND status = 'failed'
		  AND created_at >= NOW() - $2::interval`)).
		WithArgs(int64(2), "300 seconds").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))

	rule := RepeatedFailuresRule{Window: 5 * time.Minute, ThresholdCount: 3}
	alert, err := rule.Evaluate(context.Background(), wrapDB(db), models.Transaction{SourceAccountID: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alert == nil {
		t.Fatalf("expected repeated failures alert")
	}
}

func TestNewDestinationHighValueRule(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT COUNT(*)
		FROM transactions
		WHERE source_account_id = $1
		  AND destination_account_id = $2
		  AND id <> $3`)).
		WithArgs(int64(1), int64(8), int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))

	rule := NewDestinationHighValueRule{Threshold: 2000}
	alert, err := rule.Evaluate(context.Background(), wrapDB(db), models.Transaction{
		ID:                   11,
		SourceAccountID:      1,
		DestinationAccountID: 8,
		Amount:               2500,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if alert == nil {
		t.Fatalf("expected new destination alert")
	}
}

type sqlDB struct {
	db *sql.DB
}

func wrapDB(db *sql.DB) sqlDB {
	return sqlDB{db: db}
}

func (d sqlDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return d.db.QueryRowContext(ctx, query, args...)
}
