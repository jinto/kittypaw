package main

import "testing"

func TestBuildValuePrefersInjectedBuildMetadata(t *testing.T) {
	if got := buildValue("configured", "kakao/v1.2.3"); got != "kakao/v1.2.3" {
		t.Fatalf("buildValue = %q", got)
	}
	if got := buildValue("configured", "dev"); got != "configured" {
		t.Fatalf("buildValue dev = %q", got)
	}
	if got := buildValue("configured", "unknown"); got != "configured" {
		t.Fatalf("buildValue unknown = %q", got)
	}
}

func TestUnixSocketPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{name: "plain absolute path", in: "/tmp/kittykakao.sock", want: "/tmp/kittykakao.sock", ok: true},
		{name: "unix prefix", in: "unix:/tmp/kittykakao.sock", want: "/tmp/kittykakao.sock", ok: true},
		{name: "tcp", in: "127.0.0.1:8787", want: "", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := unixSocketPath(tt.in)
			if got != tt.want || ok != tt.ok {
				t.Fatalf("unixSocketPath(%q) = %q %v, want %q %v", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}
