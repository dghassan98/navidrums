package catalog

import (
	"testing"
)

func TestParseYear(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"valid date", "2023-05-15", 2023},
		{"valid year only", "2023", 2023},
		{"short year", "23", 0},
		{"empty string", "", 0},
		{"invalid format", "not-a-date", 0},
		{"date with time", "2023-12-31T23:59:59Z", 2023},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseYear(tt.input)
			if result != tt.expected {
				t.Errorf("parseYear(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}
