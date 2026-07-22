package codexstate

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Reader struct {
	db *sql.DB
}

type ListOptions struct {
	Archived bool
}

type Thread struct {
	ID          string
	RolloutPath string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CWD         string
	Title       string
	Archived    bool
	CLIVersion  string
}

func Open(path string) (*Reader, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve state database path: %w", err)
	}

	info, err := os.Stat(absolutePath)
	if err != nil {
		return nil, fmt.Errorf("inspect state database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("state database is not a regular file")
	}

	dsn := (&url.URL{
		Scheme: "file",
		Path:   absolutePath,
		RawQuery: url.Values{
			"mode":    []string{"ro"},
			"_pragma": []string{"query_only(1)", "busy_timeout(1000)"},
		}.Encode(),
	}).String()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("initialize read-only state database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open read-only state database: %w", err)
	}

	return &Reader{db: db}, nil
}

func (reader *Reader) Close() error {
	if reader == nil || reader.db == nil {
		return nil
	}
	return reader.db.Close()
}

func (reader *Reader) ListThreads(ctx context.Context, options ListOptions) ([]Thread, error) {
	archived := 0
	if options.Archived {
		archived = 1
	}

	rows, err := reader.db.QueryContext(ctx, `
		SELECT id, rollout_path, created_at, updated_at, cwd, title, archived, cli_version
		FROM threads
		WHERE archived = ?
		ORDER BY updated_at DESC, id ASC
	`, archived)
	if err != nil {
		return nil, fmt.Errorf("query threads: %w", err)
	}
	defer rows.Close()

	var threads []Thread
	for rows.Next() {
		var (
			thread      Thread
			createdAt   int64
			updatedAt   int64
			archivedInt int
		)
		if err := rows.Scan(
			&thread.ID,
			&thread.RolloutPath,
			&createdAt,
			&updatedAt,
			&thread.CWD,
			&thread.Title,
			&archivedInt,
			&thread.CLIVersion,
		); err != nil {
			return nil, fmt.Errorf("scan thread metadata: %w", err)
		}

		thread.CreatedAt = time.Unix(createdAt, 0).UTC()
		thread.UpdatedAt = time.Unix(updatedAt, 0).UTC()
		thread.Archived = archivedInt != 0
		threads = append(threads, thread)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate thread metadata: %w", err)
	}

	return threads, nil
}
