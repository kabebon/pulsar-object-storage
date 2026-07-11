package repository

import (
	"errors"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // postgres driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/golang-migrate/migrate/v4/source/file" // file source (fallback)

	"pulsar/internal/config"
	"pulsar/migrations"
)

// Migrate runs database migrations. It prefers embedded migrations (so the
// binary is self-contained); if migrationsDir points to an existing directory
// on disk, those files take precedence (useful for development overrides).
func Migrate(cfg config.DBConfig, migrationsDir string) error {
	if migrationsDir != "" {
		if _, err := os.Stat(migrationsDir); err == nil {
			m, err := migrate.New("file://"+migrationsDir, cfg.DSN)
			if err == nil {
				defer m.Close()
				return applyMigrations(m)
			}
		}
	}
	// Fall back to embedded migrations.
	srcFS, err := iofs.New(migrations.SQL(), ".")
	if err != nil {
		return fmt.Errorf("create embedded migrate source: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", srcFS, cfg.DSN)
	if err != nil {
		return fmt.Errorf("create migrate instance: %w", err)
	}
	defer m.Close()
	return applyMigrations(m)
}

// applyMigrations runs Up, treating "no change" as success.
func applyMigrations(m *migrate.Migrate) error {
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}
