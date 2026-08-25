package store

import (
	"testing"
)

// TestMigration17RemovesMultiProvider covers the qobuz-only migration: a
// custom qobuz-direct endpoint survives as a setting, the providers table and
// the provider selection settings go, and queued sync_hifi jobs are renamed.
func TestMigration17RemovesMultiProvider(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	// setupTestDB runs every migration, so rebuild the pre-17 state and run
	// migration 17 over it directly.
	seedLegacyProviderState(t, db)
	runMigration(t, db, 17)

	t.Run("carries a custom endpoint into settings", func(t *testing.T) {
		settings := NewSettingsRepo(db)
		val, err := settings.Get(SettingQobuzBaseURL)
		if err != nil {
			t.Fatalf("Get(%q) failed: %v", SettingQobuzBaseURL, err)
		}
		if val != "https://qobuz.example.test/api" {
			t.Errorf("%s = %q, want the custom endpoint", SettingQobuzBaseURL, val)
		}
	})

	t.Run("drops the providers table", func(t *testing.T) {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name='providers'`).Scan(&name)
		if err == nil {
			t.Fatal("providers table still exists")
		}
	})

	t.Run("removes provider selection settings", func(t *testing.T) {
		for _, key := range []string{
			"active_provider", "active_metadata_provider",
			"active_download_provider", "active_streaming_provider",
			"custom_providers",
		} {
			var count int
			if err := db.QueryRow(
				`SELECT COUNT(*) FROM settings WHERE key = ?`, key).Scan(&count); err != nil {
				t.Fatalf("count %q failed: %v", key, err)
			}
			if count != 0 {
				t.Errorf("setting %q survived the migration", key)
			}
		}
	})

	t.Run("renames sync_hifi jobs", func(t *testing.T) {
		var old, renamed int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM jobs WHERE type = 'sync_hifi'`).Scan(&old); err != nil {
			t.Fatalf("count sync_hifi failed: %v", err)
		}
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM jobs WHERE type = 'sync_provider'`).Scan(&renamed); err != nil {
			t.Fatalf("count sync_provider failed: %v", err)
		}
		if old != 0 {
			t.Errorf("sync_hifi jobs = %d, want 0", old)
		}
		if renamed != 1 {
			t.Errorf("sync_provider jobs = %d, want 1", renamed)
		}
	})
}

// TestMigration17OnFreshInstall covers the case the first query hits: no
// providers table at all, which must not fail the migration.
func TestMigration17OnFreshInstall(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	runMigration(t, db, 17)

	settings := NewSettingsRepo(db)
	if val, err := settings.Get(SettingQobuzBaseURL); err == nil && val != "" {
		t.Errorf("%s = %q, want unset on a fresh install", SettingQobuzBaseURL, val)
	}
}

func seedLegacyProviderState(t *testing.T, db *DB) {
	t.Helper()

	stmts := []string{
		`CREATE TABLE providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			url TEXT NOT NULL,
			position INTEGER NOT NULL DEFAULT 0,
			type TEXT NOT NULL
		)`,
		`INSERT INTO providers (type, url, name, position)
		 VALUES ('qobuz-direct', 'https://qobuz.example.test/api', 'Custom', 0)`,
		`INSERT INTO providers (type, url, name, position)
		 VALUES ('legacy', 'https://legacy.example.test', 'Legacy', 1)`,
		`INSERT INTO settings (key, value) VALUES ('active_metadata_provider', 'legacy')`,
		`INSERT INTO settings (key, value) VALUES ('active_download_provider', 'legacy')`,
		`INSERT INTO settings (key, value) VALUES ('active_streaming_provider', 'legacy')`,
		`INSERT INTO settings (key, value) VALUES ('active_provider', 'legacy')`,
		`INSERT INTO settings (key, value) VALUES ('custom_providers', '[]')`,
		`INSERT INTO jobs (type, status, source_id) VALUES ('sync_hifi', 'queued', 'track-1')`,
	}

	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("seed failed for %q: %v", q, err)
		}
	}
}

func runMigration(t *testing.T, db *DB, version int) {
	t.Helper()

	for _, m := range migrations {
		if m.version != version {
			continue
		}
		tx, err := db.root.Beginx()
		if err != nil {
			t.Fatalf("begin failed: %v", err)
		}
		if err := m.up(tx); err != nil {
			_ = tx.Rollback()
			t.Fatalf("migration %d failed: %v", version, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit failed: %v", err)
		}
		return
	}
	t.Fatalf("migration %d not found", version)
}

// TestGetJobStatsOnEmptyHistory covers the NULL that SUM returns over zero
// rows, which previously failed to scan and left the history tab statless.
func TestGetJobStatsOnEmptyHistory(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	stats, err := db.GetJobStats()
	if err != nil {
		t.Fatalf("GetJobStats on an empty history failed: %v", err)
	}

	tests := []struct {
		name string
		got  int
	}{
		{"total", stats.Total},
		{"completed", stats.Completed},
		{"failed", stats.Failed},
		{"cancelled", stats.Cancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != 0 {
				t.Errorf("%s = %d, want 0", tt.name, tt.got)
			}
		})
	}
}
