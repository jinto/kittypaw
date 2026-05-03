package browser

import "testing"

func TestValidateNavigationURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		allowed []string
		wantErr bool
	}{
		{"public_https", "https://example.com/path", nil, false},
		{"public_http", "http://example.com/path", nil, false},
		{"missing_scheme", "example.com", nil, true},
		{"javascript_scheme", "javascript:alert(1)", nil, true},
		{"file_scheme", "file:///etc/passwd", nil, true},
		{"loopback_blocked", "http://127.0.0.1:8080", nil, true},
		{"localhost_blocked", "http://localhost:8080", nil, true},
		{"private_blocked", "http://192.168.1.1", nil, true},
		{"allow_localhost", "http://localhost:8080", []string{"localhost"}, false},
		{"allow_loopback", "http://127.0.0.1:8080", []string{"127.0.0.1"}, false},
		{"allow_wildcard", "http://10.0.0.2", []string{"*"}, false},
		{"reject_unlisted_when_allowlist_present", "https://example.com", []string{"kittypaw.local"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateNavigationURL(tt.rawURL, tt.allowed)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got == "" {
				t.Fatal("normalized URL is empty")
			}
		})
	}
}
