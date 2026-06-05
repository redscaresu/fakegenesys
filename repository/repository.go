// Package repository is the SQLite-backed state engine for fakegenesys.
//
// One *Repository owns one database/sql handle (modernc.org/sqlite,
// pure Go, no CGO). FK constraints are declared at the schema level
// for hierarchical resources. Resource bodies live in a JSON `data`
// column; FK-bearing identity columns are extracted as proper SQL
// columns so cross-resource FK validation works.
//
// Standing rules carried over from fakeaws / fakegcp:
//   - SetMaxOpenConns(1) — mandatory for FK enforcement and :memory:
//     isolation across goroutines (otherwise FKs silently drop).
//   - PRAGMA foreign_keys = ON — per-connection, applied at Open.
//   - Reset/Snapshot/Restore lifecycle covers the SQLite file AND any
//     in-process cache registered via RegisterCache (the OAuth token
//     store registers here so /mock/reset purges tokens too).
//
// In S108 the repository ships universal bookkeeping (operations log)
// plus the OAuth token table. Per-resource schemas land per slice:
// identity resources in S109, routing in S110, architect in S111.
package repository

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"

	"github.com/redscaresu/fakegenesys/models"
)

// Repository wraps the SQLite handle plus the in-process cache hooks.
type Repository struct {
	db     *sql.DB
	dbPath string

	cachesMu sync.Mutex
	caches   []Cache
}

// Cache is the interface in-process state must satisfy to ride the
// repository's reset/snapshot/restore lifecycle. The OAuth token store
// (handlers/oauth.go) is the first user of this in S108.
type Cache interface {
	Name() string
	Reset() error
	Snapshot(dbPath string) error
	Restore(dbPath string) error
}

// New opens the SQLite database, applies migrations, and returns the
// Repository. dbPath of ":memory:" is supported but snapshot/restore
// become no-ops since there's no on-disk file to VACUUM INTO.
func New(dbPath string) (*Repository, error) {
	db, err := openDB(dbPath)
	if err != nil {
		return nil, err
	}
	r := &Repository{db: db, dbPath: dbPath}
	if err := r.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return r, nil
}

func openDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign_keys: %w", err)
	}
	return db, nil
}

// DB exposes the underlying handle for per-resource queries. Handlers
// call this in their CRUD paths; tests use it directly to seed fixtures.
func (r *Repository) DB() *sql.DB { return r.db }

// Close releases the SQLite handle. Idempotent.
func (r *Repository) Close() error {
	if r.db == nil {
		return nil
	}
	err := r.db.Close()
	r.db = nil
	return err
}

// RegisterCache adds an in-process cache to the lifecycle hooks. The
// OAuth token store (handlers/oauth.go) calls this in NewApplication
// so /mock/reset purges tokens too.
func (r *Repository) RegisterCache(c Cache) {
	r.cachesMu.Lock()
	defer r.cachesMu.Unlock()
	r.caches = append(r.caches, c)
}

// Reset truncates every table managed by fakegenesys and resets every
// registered cache. Used by POST /mock/reset.
func (r *Repository) Reset() error {
	if _, err := r.db.Exec(`DELETE FROM operations`); err != nil {
		return fmt.Errorf("reset operations: %w", err)
	}
	// Per-resource truncations land as resources arrive. Each handler
	// file owns its own DROP/RECREATE inside this Reset path.
	for _, table := range managedTables() {
		if _, err := r.db.Exec("DELETE FROM " + table); err != nil {
			return fmt.Errorf("reset %s: %w", table, err)
		}
	}
	r.cachesMu.Lock()
	defer r.cachesMu.Unlock()
	for _, c := range r.caches {
		if err := c.Reset(); err != nil {
			return fmt.Errorf("reset cache %s: %w", c.Name(), err)
		}
	}
	return nil
}

// Snapshot copies the current SQLite file to <dbPath>.snapshot. Returns
// ErrConflict when running on :memory: (no on-disk file).
func (r *Repository) Snapshot() error {
	if r.dbPath == ":memory:" || r.dbPath == "" {
		return fmt.Errorf("snapshot on :memory:: %w", models.ErrConflict)
	}
	if _, err := r.db.Exec("VACUUM INTO ?", r.dbPath+".snapshot"); err != nil {
		return fmt.Errorf("vacuum into snapshot: %w", err)
	}
	r.cachesMu.Lock()
	defer r.cachesMu.Unlock()
	for _, c := range r.caches {
		if err := c.Snapshot(r.dbPath); err != nil {
			return fmt.Errorf("snapshot cache %s: %w", c.Name(), err)
		}
	}
	return nil
}

// Restore reverses a Snapshot. Returns ErrNotFound if no snapshot
// baseline exists, ErrConflict on :memory:.
func (r *Repository) Restore() error {
	if r.dbPath == ":memory:" || r.dbPath == "" {
		return fmt.Errorf("restore on :memory:: %w", models.ErrConflict)
	}
	snapPath := r.dbPath + ".snapshot"
	if _, err := os.Stat(snapPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("no snapshot at %s: %w", snapPath, models.ErrNotFound)
		}
		return fmt.Errorf("stat snapshot: %w", err)
	}
	if err := r.db.Close(); err != nil {
		return fmt.Errorf("close db: %w", err)
	}
	if err := copyFile(snapPath, r.dbPath); err != nil {
		return fmt.Errorf("copy snapshot: %w", err)
	}
	db, err := openDB(r.dbPath)
	if err != nil {
		return fmt.Errorf("reopen db: %w", err)
	}
	r.db = db
	r.cachesMu.Lock()
	defer r.cachesMu.Unlock()
	for _, c := range r.caches {
		if err := c.Restore(r.dbPath); err != nil {
			return fmt.Errorf("restore cache %s: %w", c.Name(), err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, in, 0o600)
}

// managedTables returns the per-resource tables Reset should truncate.
// Per-slice tickets append to this list when adding a new resource.
func managedTables() []string {
	return []string{
		// S109 identity:
		"users", "groups", "locations", "auth_roles", "oauth_clients",
		// S110 routing:
		"routing_queues", "routing_skills", "routing_wrapupcodes",
		"routing_languages", "routing_utilization", "routing_queue_members",
		// S111 architect / responsemanagement / IDP:
		"architect_datatables", "architect_datatable_rows",
		"architect_user_prompts", "flows",
		"responsemanagement_responses", "idp_generic",
	}
}

func (r *Repository) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS operations (
			id TEXT PRIMARY KEY,
			service TEXT NOT NULL,
			method TEXT NOT NULL,
			path TEXT NOT NULL,
			status_code INTEGER NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		// S109 identity tables. Body is the opaque JSON-serialized
		// representation the API returns on GET. Indexed identity
		// columns (email for users, name for auth_roles) carry
		// uniqueness constraints the spec declares.
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			state TEXT NOT NULL DEFAULT 'active',
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS groups (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'official',
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS locations (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS auth_roles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			body TEXT NOT NULL,
			default_role INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS oauth_clients (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			secret TEXT NOT NULL,
			grant_type TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		// S110 routing tables. routing_queue_members carries an FK
		// reference to routing_queues so cascade-delete fires.
		`CREATE TABLE IF NOT EXISTS routing_queues (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS routing_skills (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS routing_wrapupcodes (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS routing_languages (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		// routing_utilization is a singleton — one row with id='_'.
		`CREATE TABLE IF NOT EXISTS routing_utilization (
			id TEXT PRIMARY KEY,
			body TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS routing_queue_members (
			queue_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			ring_number INTEGER NOT NULL DEFAULT 1,
			joined INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (queue_id, user_id),
			FOREIGN KEY (queue_id) REFERENCES routing_queues(id) ON DELETE CASCADE
		)`,
		// S111 architect / responsemanagement / IDP.
		`CREATE TABLE IF NOT EXISTS architect_datatables (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS architect_datatable_rows (
			datatable_id TEXT NOT NULL,
			row_id TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (datatable_id, row_id),
			FOREIGN KEY (datatable_id) REFERENCES architect_datatables(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS architect_user_prompts (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS flows (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			type TEXT NOT NULL DEFAULT 'inboundcall',
			state TEXT NOT NULL DEFAULT 'unpublished',
			locked_user_id TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS responsemanagement_responses (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			body TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		// idp_generic is a singleton — one row keyed by '_'.
		`CREATE TABLE IF NOT EXISTS idp_generic (
			id TEXT PRIMARY KEY,
			body TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	}
	for _, stmt := range stmts {
		if _, err := r.db.Exec(stmt); err != nil {
			return fmt.Errorf("migrate: %s: %w", firstLine(stmt), err)
		}
	}
	return nil
}

func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' {
			return s[:i]
		}
	}
	return s
}
