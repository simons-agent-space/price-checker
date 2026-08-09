package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"time"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type Search struct {
	ID            int64
	Name          string
	Query         string
	CheckInterval time.Duration
	NextCheckAt   time.Time
	CreatedAt     time.Time
}

type Product struct {
	ID            int64
	SearchID      int64
	URL           string
	LastCheckedAt *time.Time
	CreatedAt     time.Time
}

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func Migrate(db *sql.DB) error {
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	return goose.Up(db, "migrations")
}

type scanner interface {
	Scan(dest ...any) error
}
