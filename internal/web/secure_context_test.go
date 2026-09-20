package web

import "testing"

func TestSecureContextStatus(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{"https", "https://lan.example.test", true},
		{"http localhost", "http://localhost:8080", true},
		{"http localhost mixed case", "http://LocalHost:8080", true},
		{"http loopback IPv4", "http://127.0.0.1:8080", true},
		{"http loopback IPv6", "http://[::1]:8080", true},
		{"http public host", "http://lan.example.test", false},
		{"http LAN IP", "http://192.168.1.10:8080", false},
		{"invalid URL", "not a url", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SecureContextStatus(tt.baseURL); got != tt.want {
				t.Errorf("SecureContextStatus(%q) = %v, want %v", tt.baseURL, got, tt.want)
			}
		})
	}
}
