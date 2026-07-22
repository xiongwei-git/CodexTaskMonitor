package codexstate

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestReaderListsThreadsWithoutWriting(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "state.sqlite")
	seedDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open seed database: %v", err)
	}

	_, err = seedDB.Exec(`
		CREATE TABLE threads (
			id TEXT PRIMARY KEY,
			rollout_path TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			cwd TEXT NOT NULL,
			title TEXT NOT NULL,
			archived INTEGER NOT NULL,
			cli_version TEXT NOT NULL
		);
		INSERT INTO threads VALUES
			('thread-active', '/sessions/active.jsonl', 1784600000, 1784600100, '/Users/example/CodeX/Alpha', 'Active task', 0, '0.145.0'),
			('thread-archived', '/sessions/archived.jsonl', 1784500000, 1784500100, '/Users/example/CodeX/Beta', 'Archived task', 1, '0.144.0');
	`)
	if err != nil {
		seedDB.Close()
		t.Fatalf("seed database: %v", err)
	}
	if err := seedDB.Close(); err != nil {
		t.Fatalf("close seed database: %v", err)
	}

	reader, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reader.Close()

	threads, err := reader.ListThreads(context.Background(), ListOptions{Archived: false})
	if err != nil {
		t.Fatalf("ListThreads() error = %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("len(threads) = %d, want 1", len(threads))
	}

	got := threads[0]
	if got.ID != "thread-active" || got.CWD != "/Users/example/CodeX/Alpha" {
		t.Fatalf("thread = %#v, want active Alpha thread", got)
	}
	wantUpdated := time.Unix(1784600100, 0).UTC()
	if !got.UpdatedAt.Equal(wantUpdated) {
		t.Fatalf("UpdatedAt = %v, want %v", got.UpdatedAt, wantUpdated)
	}

	if _, err := reader.db.ExecContext(context.Background(), `CREATE TABLE forbidden (id INTEGER)`); err == nil {
		t.Fatal("read-only database unexpectedly allowed CREATE TABLE")
	}
}

func TestOpenRejectsMissingDatabase(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "missing.sqlite"))
	if err == nil {
		t.Fatal("Open() error = nil, want missing database error")
	}
}
