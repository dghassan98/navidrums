package store

import (
	"testing"

	"github.com/cesargomez89/navidrums/internal/constants"
)

// TestSeedDefaultProviders covers migration 16: a fresh install gets the
// default Monochrome instance and is pointed at it, while the official Qobuz
// endpoint is seeded but never selected on its own.
func TestSeedDefaultProviders(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewProvidersRepo(db)

	t.Run("seeds the default monochrome instance", func(t *testing.T) {
		providers, err := repo.ListByType("monochrome")
		if err != nil {
			t.Fatalf("ListByType failed: %v", err)
		}
		if len(providers) != 1 {
			t.Fatalf("monochrome providers = %d, want 1", len(providers))
		}
		if providers[0].URL != constants.MonochromeDefaultURL {
			t.Errorf("url = %q, want %q", providers[0].URL, constants.MonochromeDefaultURL)
		}
	})

	t.Run("seeds the official qobuz endpoint", func(t *testing.T) {
		providers, err := repo.ListByType("qobuz-direct")
		if err != nil {
			t.Fatalf("ListByType failed: %v", err)
		}
		if len(providers) != 1 {
			t.Fatalf("qobuz-direct providers = %d, want 1", len(providers))
		}
		if providers[0].URL != constants.QobuzDirectDefaultURL {
			t.Errorf("url = %q, want %q", providers[0].URL, constants.QobuzDirectDefaultURL)
		}
	})

	t.Run("points a fresh install at monochrome", func(t *testing.T) {
		settings := NewSettingsRepo(db)
		keys := []string{
			SettingActiveMetadataProvider,
			SettingActiveDownloadProvider,
			SettingActiveStreamingProvider,
		}
		for _, key := range keys {
			val, err := settings.Get(key)
			if err != nil {
				t.Fatalf("Get(%q) failed: %v", key, err)
			}
			if val != "monochrome" {
				t.Errorf("%s = %q, want monochrome", key, val)
			}
		}
	})
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
