package rag

import "testing"

func TestEscapeLanceFilter(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"clean string", "hello world", "hello world"},
		{"single quote", "it's a test", "it''s a test"},
		{"double quote unchanged", `say "hello"`, `say "hello"`},
		{"multiple single quotes", "a'b'c", "a''b''c"},
		{"LIKE pattern preserved", "internal/rag/%", "internal/rag/%"},
		{"empty string", "", ""},
		{"only quote", "'", "''"},
		{"injection attempt", "'; DROP TABLE chunks; --", "''; DROP TABLE chunks; --"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeLanceFilter(tt.in)
			if got != tt.want {
				t.Errorf("escapeLanceFilter(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
