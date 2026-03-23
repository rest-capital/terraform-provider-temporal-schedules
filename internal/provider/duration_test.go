package provider

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{"", 0, false},
		{"30s", 30 * time.Second, false},
		{"5m", 5 * time.Minute, false},
		{"4h", 4 * time.Hour, false},
		{"4h30m", 4*time.Hour + 30*time.Minute, false},
		{"1d", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"365d", 365 * 24 * time.Hour, false},
		{"invalid", 0, true},
		{"d", 0, true},
		{"abc d", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseDuration(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDuration(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.expected {
				t.Errorf("parseDuration(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}
