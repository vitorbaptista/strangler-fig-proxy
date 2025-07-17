package main

import (
	"os"
	"testing"
)

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty URL",
			input:    "",
			expected: "",
		},
		{
			name:     "URL with http scheme",
			input:    "http://example.com",
			expected: "http://example.com:80",
		},
		{
			name:     "URL with https scheme",
			input:    "https://example.com",
			expected: "https://example.com:443",
		},
		{
			name:     "URL without scheme",
			input:    "example.com",
			expected: "http://example.com:80",
		},
		{
			name:     "URL with port",
			input:    "http://example.com:8080",
			expected: "http://example.com:8080",
		},
		{
			name:     "URL with path",
			input:    "http://example.com/api",
			expected: "http://example.com:80/api",
		},
		{
			name:     "URL with query parameters",
			input:    "http://example.com/api?param=value",
			expected: "http://example.com:80/api?param=value",
		},
		{
			name:     "URL with fragment",
			input:    "http://example.com/api#section",
			expected: "http://example.com:80/api#section",
		},
		{
			name:     "URL with subdomain",
			input:    "api.example.com",
			expected: "http://api.example.com:80",
		},
		{
			name:     "URL with IP address",
			input:    "192.168.1.1",
			expected: "http://192.168.1.1:80",
		},
		{
			name:     "URL with IP address and port",
			input:    "192.168.1.1:8080",
			expected: "http://192.168.1.1:8080",
		},
		{
			name:     "URL with localhost",
			input:    "localhost:3000",
			expected: "http://localhost:3000",
		},
		{
			name:     "URL with localhost without port",
			input:    "localhost",
			expected: "http://localhost:80",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeURL(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeURL(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLoadConfigURLNormalization(t *testing.T) {
	// Test that LoadConfig properly normalizes URLs
	originalMainURL := os.Getenv("MAIN_SERVER_URL")
	originalNewURL := os.Getenv("NEW_SERVER_URL")

	// Set test environment variables
	os.Setenv("MAIN_SERVER_URL", "example.com")
	os.Setenv("NEW_SERVER_URL", "api.example.com:8080")

	// Load config
	config := LoadConfig()

	// Check that URLs were normalized
	expectedMainURL := "http://example.com:80"
	expectedNewURL := "http://api.example.com:8080"

	if config.MainServerURL != expectedMainURL {
		t.Errorf("MainServerURL not normalized correctly: got %q, expected %q", config.MainServerURL, expectedMainURL)
	}

	if config.NewServerURL != expectedNewURL {
		t.Errorf("NewServerURL not normalized correctly: got %q, expected %q", config.NewServerURL, expectedNewURL)
	}

	// Restore original environment variables
	if originalMainURL != "" {
		os.Setenv("MAIN_SERVER_URL", originalMainURL)
	} else {
		os.Unsetenv("MAIN_SERVER_URL")
	}

	if originalNewURL != "" {
		os.Setenv("NEW_SERVER_URL", originalNewURL)
	} else {
		os.Unsetenv("NEW_SERVER_URL")
	}
}
