package main

import (
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"empty URL", "", ""},
		{"URL with http scheme", "http://example.com", "http://example.com:80"},
		{"URL with https scheme", "https://example.com", "https://example.com:443"},
		{"URL without scheme", "example.com", "http://example.com:80"},
		{"URL with port", "http://example.com:8080", "http://example.com:8080"},
		{"URL with path", "http://example.com/api", "http://example.com:80/api"},
		{"URL with query parameters", "http://example.com/api?param=value", "http://example.com:80/api?param=value"},
		{"URL with fragment", "http://example.com/api#section", "http://example.com:80/api#section"},
		{"URL with subdomain", "api.example.com", "http://api.example.com:80"},
		{"URL with IP address", "192.168.1.1", "http://192.168.1.1:80"},
		{"URL with IP address and port", "192.168.1.1:8080", "http://192.168.1.1:8080"},
		{"URL with localhost", "localhost:3000", "http://localhost:3000"},
		{"URL with localhost without port", "localhost", "http://localhost:80"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeURL(tt.input); got != tt.expected {
				t.Errorf("normalizeURL(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("normalizes server URLs", func(t *testing.T) {
		t.Setenv("MAIN_SERVER_URL", "example.com")
		t.Setenv("NEW_SERVER_URL", "api.example.com:8080")

		config, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig() unexpected error: %v", err)
		}
		if config.MainServerURL != "http://example.com:80" {
			t.Errorf("MainServerURL = %q, expected %q", config.MainServerURL, "http://example.com:80")
		}
		if config.NewServerURL != "http://api.example.com:8080" {
			t.Errorf("NewServerURL = %q, expected %q", config.NewServerURL, "http://api.example.com:8080")
		}
	})

	t.Run("parses routes with percentages", func(t *testing.T) {
		t.Setenv("MAIN_SERVER_URL", "http://main:8080")
		t.Setenv("NEW_SERVER_URL", "http://new:8080")
		t.Setenv("NEW_SERVER_ROUTES", "/api/v2=25,/health")

		config, err := LoadConfig()
		if err != nil {
			t.Fatalf("LoadConfig() unexpected error: %v", err)
		}
		if len(config.Routes) != 2 {
			t.Fatalf("expected 2 routes, got %v", config.Routes)
		}
		if config.Routes[0].Prefix != "/api/v2" || config.Routes[0].Percentage != 25 {
			t.Errorf("unexpected first route: %v", config.Routes[0])
		}
		if config.Routes[1].Prefix != "/health" || config.Routes[1].Percentage != 100 {
			t.Errorf("unexpected second route: %v", config.Routes[1])
		}
	})

	t.Run("requires server URLs", func(t *testing.T) {
		t.Setenv("MAIN_SERVER_URL", "")
		t.Setenv("NEW_SERVER_URL", "http://new:8080")
		if _, err := LoadConfig(); err == nil {
			t.Error("expected error when MAIN_SERVER_URL is missing")
		}

		t.Setenv("MAIN_SERVER_URL", "http://main:8080")
		t.Setenv("NEW_SERVER_URL", "")
		if _, err := LoadConfig(); err == nil {
			t.Error("expected error when NEW_SERVER_URL is missing")
		}
	})

	t.Run("rejects invalid routes", func(t *testing.T) {
		t.Setenv("MAIN_SERVER_URL", "http://main:8080")
		t.Setenv("NEW_SERVER_URL", "http://new:8080")
		t.Setenv("NEW_SERVER_ROUTES", "/api=150")
		if _, err := LoadConfig(); err == nil {
			t.Error("expected error for percentage above 100")
		}
	})

	t.Run("rejects invalid sampling rate", func(t *testing.T) {
		t.Setenv("MAIN_SERVER_URL", "http://main:8080")
		t.Setenv("NEW_SERVER_URL", "http://new:8080")
		t.Setenv("SAMPLING_RATE", "1.5")
		if _, err := LoadConfig(); err == nil {
			t.Error("expected error for sampling rate above 1.0")
		}
	})
}
