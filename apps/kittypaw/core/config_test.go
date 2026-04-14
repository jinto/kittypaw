package core

import "testing"

func TestBindOrDefault(t *testing.T) {
	tests := []struct {
		bind string
		want string
	}{
		{"", ":3000"},
		{":8080", ":8080"},
		{"0.0.0.0:9000", "0.0.0.0:9000"},
	}
	for _, tt := range tests {
		cfg := ServerConfig{Bind: tt.bind}
		got := cfg.BindOrDefault()
		if got != tt.want {
			t.Errorf("BindOrDefault(%q) = %q, want %q", tt.bind, got, tt.want)
		}
	}
}
