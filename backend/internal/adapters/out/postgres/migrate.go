// Package postgres implementa ports.CreditLineRepository sobre PostgreSQL.
package postgres

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // driver pgx5://
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"leancore-fintech/backend/migrations"
)

// Migrate aplica todas las migraciones pendientes.
func Migrate(dsn string) error {
	m, err := newMigrate(dsn)
	if err != nil {
		return err
	}
	defer m.Close()
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// MigrateDown revierte todas las migraciones (usado en tests).
func MigrateDown(dsn string) error {
	m, err := newMigrate(dsn)
	if err != nil {
		return err
	}
	defer m.Close()
	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate down: %w", err)
	}
	return nil
}

func newMigrate(dsn string) (*migrate.Migrate, error) {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, err
	}
	url := dsn
	for _, prefix := range []string{"postgres://", "postgresql://"} {
		if strings.HasPrefix(url, prefix) {
			url = "pgx5://" + strings.TrimPrefix(url, prefix)
		}
	}
	return migrate.NewWithSourceInstance("iofs", src, url)
}
