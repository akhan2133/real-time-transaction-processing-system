package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Store) RunMigrations(ctx context.Context, logger *slog.Logger) error {
	if _, err := s.DB.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	migrationDir := migrationsDir()
	entries, err := os.ReadDir(migrationDir)
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var applied bool
		if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied {
			continue
		}

		content, err := os.ReadFile(filepath.Join(migrationDir, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration tx %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
		logger.Info("migration applied", "version", name)
	}

	return nil
}

func migrationsDir() string {
	if dir := os.Getenv("MIGRATIONS_DIR"); dir != "" {
		return dir
	}
	return "migrations"
}

func (s *Store) SeedDemoData(ctx context.Context, logger *slog.Logger) error {
	seedAccounts := []struct {
		name     string
		balance  float64
		currency string
		status   string
	}{
		{name: "Alice", balance: 12000, currency: "USD", status: "active"},
		{name: "Bob", balance: 6500, currency: "USD", status: "active"},
		{name: "Carol", balance: 4000, currency: "USD", status: "active"},
		{name: "Dave", balance: 1500, currency: "USD", status: "active"},
	}

	for _, account := range seedAccounts {
		if _, err := s.DB.ExecContext(ctx, `
			INSERT INTO accounts (owner_name, balance, currency, status)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (owner_name) DO NOTHING`,
			account.name, account.balance, strings.ToUpper(account.currency), account.status); err != nil {
			return fmt.Errorf("seed account %s: %w", account.name, err)
		}
	}

	logger.Info("seed data ensured")
	return nil
}

func (s *Store) ResetForTests(ctx context.Context) error {
	statements := []string{
		`TRUNCATE alerts, transaction_events, transactions, accounts RESTART IDENTITY CASCADE`,
	}
	for _, stmt := range statements {
		if _, err := s.DB.ExecContext(ctx, stmt); err != nil && err != sql.ErrNoRows {
			return err
		}
	}
	return nil
}
