// Package repository provides a thin DB access layer built on pgx.
// Queries are written by hand in the sqlc spirit (one method per query,
// explicit args/structs) but require no code-generation step.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"pulsar/internal/config"
)

// DB is the shared *pgxpool.Pool wrapper. Methods hang off Querier so the same
// type can be used for transactions (pgx.Tx) and pool (pgxpool.Pool).
type DB struct {
	Pool *pgxpool.Pool
}

// New opens a connection pool and validates connectivity. It applies sensible
// pool sizing derived from config.
func New(ctx context.Context, cfg config.DBConfig) (*DB, error) {
	pcfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse db dsn: %w", err)
	}
	pcfg.MaxConns = cfg.MaxOpenConns
	pcfg.MinConns = cfg.MaxIdleConns
	pcfg.MaxConnLifetime = cfg.ConnMaxLifetime

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return nil, fmt.Errorf("open db pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &DB{Pool: pool}, nil
}

// Close releases all pool resources.
func (d *DB) Close() {
	if d != nil && d.Pool != nil {
		d.Pool.Close()
	}
}

// IsUniqueViolation reports whether err is a Postgres unique-constraint violation.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// IsForeignKeyViolation reports whether err is a Postgres foreign-key violation.
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// Wrap returns an error that reports both sentinel and a human message via
// errors.Is/errors.As. Used to attach a domain sentinel to a descriptive text.
func Wrap(sentinel error, msg string) error {
	if msg == "" {
		return sentinel
	}
	return &wrappedErr{sentinel: sentinel, msg: msg}
}

type wrappedErr struct {
	sentinel error
	msg      string
}

func (e *wrappedErr) Error() string { return e.msg }
func (e *wrappedErr) Is(target error) bool {
	return errors.Is(e.sentinel, target)
}
func (e *wrappedErr) Unwrap() error { return e.sentinel }
