package app

import (
	"encoding/json"
	"testing"
)

func TestParseDiscoverRowsFallsBackToDefaults(t *testing.T) {
	tests := []struct {
		name   string
		stored string
	}{
		{"unset", ""},
		{"malformed", "{not json"},
		{"wrong shape", `{"kind":"new-releases"}`},
		{"every kind unknown", `[{"kind":"nope","enabled":true}]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := ParseDiscoverRows(tt.stored)
			if len(rows) != len(DefaultDiscoverRows()) {
				t.Fatalf("rows = %d, want the full default set", len(rows))
			}
			if !rows[0].Enabled {
				t.Error("defaults should have the first row enabled")
			}
		})
	}
}

func TestParseDiscoverRowsKeepsOrderAndFlags(t *testing.T) {
	stored := `[{"kind":"press-awards","enabled":true},{"kind":"new-releases","enabled":false}]`

	rows := ParseDiscoverRows(stored)

	if rows[0].Kind != "press-awards" || !rows[0].Enabled {
		t.Errorf("first row = %+v, want press-awards enabled", rows[0])
	}
	if rows[1].Kind != "new-releases" || rows[1].Enabled {
		t.Errorf("second row = %+v, want new-releases disabled", rows[1])
	}
}

func TestParseDiscoverRowsDropsUnknownKinds(t *testing.T) {
	// Qobuz 400s on an unknown album/getFeatured type, so a stale stored
	// preference must never reach it.
	stored := `[{"kind":"new-releases","enabled":true},{"kind":"retired-type","enabled":true}]`

	for _, row := range ParseDiscoverRows(stored) {
		if row.Kind == "retired-type" {
			t.Fatal("an unknown kind survived parsing")
		}
	}
}

func TestParseDiscoverRowsAppendsMissingKindsDisabled(t *testing.T) {
	// A config written before a kind existed must still list it, off, so it
	// can be switched on without a migration.
	stored := `[{"kind":"new-releases","enabled":true}]`

	rows := ParseDiscoverRows(stored)
	if len(rows) != len(DefaultDiscoverRows()) {
		t.Fatalf("rows = %d, want the full set", len(rows))
	}
	if rows[0].Kind != "new-releases" || !rows[0].Enabled {
		t.Errorf("stored row lost its position or flag: %+v", rows[0])
	}
	for _, row := range rows[1:] {
		if row.Enabled {
			t.Errorf("appended row %s should default to disabled", row.Kind)
		}
	}
}

func TestParseDiscoverRowsDropsDuplicates(t *testing.T) {
	stored := `[{"kind":"new-releases","enabled":true},{"kind":"new-releases","enabled":false}]`

	count := 0
	for _, row := range ParseDiscoverRows(stored) {
		if row.Kind == "new-releases" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("new-releases appeared %d times, want 1", count)
	}
}

func TestEnabledDiscoverRowsPreservesOrder(t *testing.T) {
	rows := []DiscoverRow{
		{Kind: "press-awards", Enabled: true},
		{Kind: "new-releases", Enabled: false},
		{Kind: PlaylistsRowKind, Enabled: true},
	}

	enabled := EnabledDiscoverRows(rows)
	if len(enabled) != 2 {
		t.Fatalf("enabled = %d, want 2", len(enabled))
	}
	if enabled[0].Kind != "press-awards" || enabled[1].Kind != PlaylistsRowKind {
		t.Errorf("order was not preserved: %+v", enabled)
	}
}

func TestIsValidDiscoverKind(t *testing.T) {
	if !IsValidDiscoverKind(PlaylistsRowKind) {
		t.Error("the playlists row must be valid even though it is not a getFeatured type")
	}
	if !IsValidDiscoverKind("ideal-discography") {
		t.Error("ideal-discography is a real Qobuz type")
	}
	if IsValidDiscoverKind("most-popular") {
		t.Error("most-popular is not a Qobuz type and must be rejected")
	}
}

func TestDefaultDiscoverRowsAreAllValid(t *testing.T) {
	for _, row := range DefaultDiscoverRows() {
		if !IsValidDiscoverKind(row.Kind) {
			t.Errorf("default row %q is not a kind the API accepts", row.Kind)
		}
		if DiscoverRowTitle(row.Kind) == row.Kind {
			t.Errorf("default row %q has no human title", row.Kind)
		}
	}
}

func TestDiscoverRowsRoundTrip(t *testing.T) {
	original := ParseDiscoverRows("")

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	decoded := ParseDiscoverRows(string(encoded))
	if len(decoded) != len(original) {
		t.Fatalf("round trip changed the row count: %d then %d", len(original), len(decoded))
	}
	for i := range original {
		if decoded[i] != original[i] {
			t.Errorf("row %d changed: %+v then %+v", i, original[i], decoded[i])
		}
	}
}
