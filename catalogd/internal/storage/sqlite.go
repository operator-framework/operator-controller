package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	"github.com/operator-framework/operator-registry/alpha/declcfg"
	_ "modernc.org/sqlite"
)

type SQLiteV1 struct {
	db      *sql.DB
	RootDir string
	RootURL *url.URL
	mu      sync.RWMutex
}

func NewSQLiteV1(rootDir string, rootURL *url.URL) (*SQLiteV1, error) {
	if err := os.MkdirAll(rootDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create root directory: %w", err)
	}

	dbPath := filepath.Join(rootDir, "storage.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := initializeDB(db); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteV1{
		db:      db,
		RootDir: rootDir,
		RootURL: rootURL,
	}, nil
}

func initializeDB(db *sql.DB) error {
	_, err := db.Exec(`
        CREATE TABLE IF NOT EXISTS metas (
            id TEXT PRIMARY KEY,
            catalog_name TEXT NOT NULL,
            schema TEXT NOT NULL,
            package TEXT,
            name TEXT NOT NULL,
            blob TEXT NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP
        );

        -- Index for catalog lookups
        CREATE INDEX IF NOT EXISTS idx_metas_catalog 
        ON metas(catalog_name);
    `)
	return err
}

func (s *SQLiteV1) Store(ctx context.Context, catalog string, fsys fs.FS) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete existing entries for this catalog
	if _, err := tx.ExecContext(ctx, "DELETE FROM metas WHERE catalog_name = ?", catalog); err != nil {
		return fmt.Errorf("failed to delete existing catalog entries: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
        INSERT INTO metas (id, catalog_name, schema, package, name, blob) 
        VALUES (?, ?, ?, ?, ?, ?)
    `)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	err = declcfg.WalkMetasFS(ctx, fsys, func(path string, meta *declcfg.Meta, err error) error {
		if err != nil {
			return err
		}

		// Generate a new UUID for each meta entry
		id := uuid.New().String()

		// Handle empty package as NULL
		// since schema=olm.package blobs don't have a `package`
		// value and the value is instead in `name`
		var pkgValue interface{}
		if meta.Package != "" {
			pkgValue = meta.Package
		}

		_, err = stmt.ExecContext(ctx,
			id,
			catalog,
			meta.Schema,
			pkgValue,
			meta.Name,
			string(meta.Blob),
		)
		if err != nil {
			return fmt.Errorf("failed to insert meta: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("error walking FBC root: %w", err)
	}

	return tx.Commit()
}

func (s *SQLiteV1) Delete(catalog string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.db.Exec("DELETE FROM metas WHERE catalog_name = ?", catalog)
	if err != nil {
		return fmt.Errorf("failed to delete catalog: %w", err)
	}
	return nil
}

func (s *SQLiteV1) BaseURL(catalog string) string {
	return s.RootURL.JoinPath(catalog).String()
}

func (s *SQLiteV1) handleV1All(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	catalog := r.PathValue("catalog")
	if catalog == "" {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.Query(`
        SELECT schema, package, name, blob 
        FROM metas 
        WHERE catalog_name = ? 
        ORDER BY schema, 
                 CASE WHEN package IS NULL THEN 1 ELSE 0 END, 
                 package, 
                 name
    `, catalog)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "application/jsonl")
	encoder := json.NewEncoder(w)

	for rows.Next() {
		var meta declcfg.Meta
		var packageVal sql.NullString
		var blobStr string

		if err := rows.Scan(&meta.Schema, &packageVal, &meta.Name, &blobStr); err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		// Convert NULL package to empty string
		if packageVal.Valid {
			meta.Package = packageVal.String
		}

		meta.Blob = json.RawMessage(blobStr)

		if err := encoder.Encode(meta); err != nil {
			return
		}
	}
}

func (s *SQLiteV1) StorageServerHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(s.RootURL.JoinPath("{catalog}", "api", "v1", "all").Path, s.handleV1All)
	return mux
}

func (s *SQLiteV1) ContentExists(catalog string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var exists bool
	err := s.db.QueryRow(`
        SELECT EXISTS(
            SELECT 1 FROM metas WHERE catalog_name = ? LIMIT 1
        )
    `, catalog).Scan(&exists)
	if err != nil {
		return false
	}
	return exists
}

func (s *SQLiteV1) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db.Close()
}
