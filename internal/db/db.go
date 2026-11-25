package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

// Store wraps SQLite connections, Goose migrations, and serialized write access.
type Store struct {
	sql     *sql.DB
	writeMu sync.Mutex

	cacheMu       sync.RWMutex
	snapshotCache map[int64]any // caches decoded CanonicalState by snapshot ID
}

// Open opens (or creates) the SQLite database at path, applies WAL/FK pragmas,
// and runs embedded Goose migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	if path != ":memory:" && path != "" {
		dir := filepath.Dir(path)
		if dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create db dir: %w", err)
			}
		}
	}
	if path == "" {
		path = ":memory:"
	}

	dsn := path
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	} else {
		dsn = fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Single connection for :memory: shared stability, small pool for file WAL.
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(8)
		db.SetMaxIdleConns(4)
	}

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	if err := Migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}

	return &Store{
		sql:           db,
		snapshotCache: make(map[int64]any),
	}, nil
}

// Migrate runs embedded Goose migrations up to the latest version.
func Migrate(ctx context.Context, db *sql.DB) error {
	goose.SetBaseFS(embedMigrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	return goose.UpContext(ctx, db, "migrations")
}

func (s *Store) Close() error {
	if s == nil || s.sql == nil {
		return nil
	}
	return s.sql.Close()
}

func (s *Store) DB() *sql.DB {
	return s.sql
}
