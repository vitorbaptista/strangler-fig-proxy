package main

import (
	"net/http"
	"testing"

	"strangler-fix-proxy/pkg/proxy"
)

func TestShouldRouteToNewServer(t *testing.T) {
	tests := []struct {
		name           string
		routes         []string
		path           string
		expectedResult bool
	}{
		{
			name:           "empty routes should return false",
			routes:         []string{},
			path:           "/api/v1/test",
			expectedResult: false,
		},
		{
			name:           "matching prefix should return true",
			routes:         []string{"/api/v2", "/health"},
			path:           "/api/v2/users",
			expectedResult: true,
		},
		{
			name:           "non-matching prefix should return false",
			routes:         []string{"/api/v2", "/health"},
			path:           "/api/v1/users",
			expectedResult: false,
		},
		{
			name:           "exact match should return true",
			routes:         []string{"/api/v2", "/health"},
			path:           "/health",
			expectedResult: true,
		},
		{
			name:           "health check path should return true",
			routes:         []string{"/api/v2", "/health"},
			path:           "/health/check",
			expectedResult: true,
		},
		{
			name:           "root path with empty routes should return false",
			routes:         []string{},
			path:           "/",
			expectedResult: false,
		},
		{
			name:           "root path with root route should return true",
			routes:         []string{"/"},
			path:           "/anything",
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &proxy.Config{
				NewServerRoutes: tt.routes,
			}
			result := config.ShouldRouteToNewServer(tt.path)
			if result != tt.expectedResult {
				t.Errorf("ShouldRouteToNewServer(%q) = %v, expected %v", tt.path, result, tt.expectedResult)
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		envValue     string
		expected     string
	}{
		{
			name:         "returns default when env var not set",
			key:          "TEST_VAR_NOT_SET",
			defaultValue: "default",
			envValue:     "",
			expected:     "default",
		},
		{
			name:         "returns env value when set",
			key:          "TEST_VAR_SET",
			defaultValue: "default",
			envValue:     "env_value",
			expected:     "env_value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.key, tt.envValue)
			}
			result := getEnv(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnv(%q, %q) = %q, expected %q", tt.key, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue int
		envValue     string
		expected     int
	}{
		{
			name:         "returns default when env var not set",
			key:          "TEST_INT_NOT_SET",
			defaultValue: 42,
			envValue:     "",
			expected:     42,
		},
		{
			name:         "returns parsed int when valid",
			key:          "TEST_INT_VALID",
			defaultValue: 42,
			envValue:     "123",
			expected:     123,
		},
		{
			name:         "returns default when invalid int",
			key:          "TEST_INT_INVALID",
			defaultValue: 42,
			envValue:     "not_a_number",
			expected:     42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.key, tt.envValue)
			}
			result := getEnvInt(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnvInt(%q, %d) = %d, expected %d", tt.key, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

func TestGetEnvFloat(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue float64
		envValue     string
		expected     float64
	}{
		{
			name:         "returns default when env var not set",
			key:          "TEST_FLOAT_NOT_SET",
			defaultValue: 1.5,
			envValue:     "",
			expected:     1.5,
		},
		{
			name:         "returns parsed float when valid",
			key:          "TEST_FLOAT_VALID",
			defaultValue: 1.5,
			envValue:     "2.5",
			expected:     2.5,
		},
		{
			name:         "returns default when invalid float",
			key:          "TEST_FLOAT_INVALID",
			defaultValue: 1.5,
			envValue:     "not_a_number",
			expected:     1.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.key, tt.envValue)
			}
			result := getEnvFloat(tt.key, tt.defaultValue)
			if result != tt.expected {
				t.Errorf("getEnvFloat(%q, %f) = %f, expected %f", tt.key, tt.defaultValue, result, tt.expected)
			}
		})
	}
}

func TestGetEnvSlice(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue []string
		envValue     string
		expected     []string
	}{
		{
			name:         "returns default when env var not set",
			key:          "TEST_SLICE_NOT_SET",
			defaultValue: []string{"default1", "default2"},
			envValue:     "",
			expected:     []string{"default1", "default2"},
		},
		{
			name:         "returns parsed slice when valid",
			key:          "TEST_SLICE_VALID",
			defaultValue: []string{"default1", "default2"},
			envValue:     "value1,value2,value3",
			expected:     []string{"value1", "value2", "value3"},
		},
		{
			name:         "returns single item slice",
			key:          "TEST_SLICE_SINGLE",
			defaultValue: []string{"default"},
			envValue:     "single",
			expected:     []string{"single"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(tt.key, tt.envValue)
			}
			result := getEnvSlice(tt.key, tt.defaultValue)
			if len(result) != len(tt.expected) {
				t.Errorf("getEnvSlice(%q, %v) = %v, expected %v", tt.key, tt.defaultValue, result, tt.expected)
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("getEnvSlice(%q, %v) = %v, expected %v", tt.key, tt.defaultValue, result, tt.expected)
					break
				}
			}
		})
	}
}

func TestCompareResponses(t *testing.T) {
	tests := []struct {
		name             string
		mainStatus       int
		newStatus        int
		mainHeaders      string
		newHeaders       string
		mainBody         string
		newBody          string
		expectedMatch    bool
		expectedMismatch string
	}{
		{
			name:             "identical responses should match",
			mainStatus:       200,
			newStatus:        200,
			mainHeaders:      `{"Content-Type":["application/json"]}`,
			newHeaders:       `{"Content-Type":["application/json"]}`,
			mainBody:         `{"result": "success"}`,
			newBody:          `{"result": "success"}`,
			expectedMatch:    true,
			expectedMismatch: "",
		},
		{
			name:             "different status should not match",
			mainStatus:       200,
			newStatus:        404,
			mainHeaders:      `{"Content-Type":["application/json"]}`,
			newHeaders:       `{"Content-Type":["application/json"]}`,
			mainBody:         `{"result": "success"}`,
			newBody:          `{"result": "success"}`,
			expectedMatch:    false,
			expectedMismatch: "status",
		},
		{
			name:             "different body should not match",
			mainStatus:       200,
			newStatus:        200,
			mainHeaders:      `{"Content-Type":["application/json"]}`,
			newHeaders:       `{"Content-Type":["application/json"]}`,
			mainBody:         `{"result": "success"}`,
			newBody:          `{"result": "failure"}`,
			expectedMatch:    false,
			expectedMismatch: "body",
		},
		{
			name:             "different headers should not match",
			mainStatus:       200,
			newStatus:        200,
			mainHeaders:      `{"Content-Type":["application/json"]}`,
			newHeaders:       `{"Content-Type":["text/plain"]}`,
			mainBody:         `{"result": "success"}`,
			newBody:          `{"result": "success"}`,
			expectedMatch:    false,
			expectedMismatch: "headers",
		},
		{
			name:             "whitespace differences should match",
			mainStatus:       200,
			newStatus:        200,
			mainHeaders:      `{"Content-Type":["application/json"]}`,
			newHeaders:       `{"Content-Type":["application/json"]}`,
			mainBody:         `{"result": "success"}`,
			newBody:          `  {"result": "success"}  `,
			expectedMatch:    true,
			expectedMismatch: "",
		},
		{
			name:             "JSON objects with different key ordering should match",
			mainStatus:       200,
			newStatus:        200,
			mainHeaders:      `{"Content-Type":["application/json"]}`,
			newHeaders:       `{"Content-Type":["application/json"]}`,
			mainBody:         `{"foo": 42, "bar": 51}`,
			newBody:          `{"bar":51,"foo":42}`,
			expectedMatch:    true,
			expectedMismatch: "",
		},
		{
			name:             "non-JSON strings should still be compared as strings",
			mainStatus:       200,
			newStatus:        200,
			mainHeaders:      `{"Content-Type":["text/plain"]}`,
			newHeaders:       `{"Content-Type":["text/plain"]}`,
			mainBody:         `Hello, World!`,
			newBody:          `Hello, World!`,
			expectedMatch:    true,
			expectedMismatch: "",
		},
		{
			name:             "different JSON values should not match",
			mainStatus:       200,
			newStatus:        200,
			mainHeaders:      `{"Content-Type":["application/json"]}`,
			newHeaders:       `{"Content-Type":["application/json"]}`,
			mainBody:         `{"foo": 42, "bar": 51}`,
			newBody:          `{"foo": 42, "bar": 52}`,
			expectedMatch:    false,
			expectedMismatch: "body",
		},
		{
			name:             "non-JSON string bodies should be compared as strings",
			mainStatus:       200,
			newStatus:        200,
			mainHeaders:      `{"Content-Type":["text/plain"]}`,
			newHeaders:       `{"Content-Type":["text/plain"]}`,
			mainBody:         `{Hello, World![`,
			newBody:          `{Hello, World![`,
			expectedMatch:    true,
			expectedMismatch: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, mismatchType := proxy.CompareResponses(tt.mainStatus, tt.newStatus, tt.mainHeaders, tt.newHeaders, tt.mainBody, tt.newBody)
			if match != tt.expectedMatch {
				t.Errorf("CompareResponses() match = %v, expected %v", match, tt.expectedMatch)
			}
			if mismatchType != tt.expectedMismatch {
				t.Errorf("CompareResponses() mismatchType = %q, expected %q", mismatchType, tt.expectedMismatch)
			}
		})
	}
}

func TestNormalizeURLPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "normal path remains unchanged",
			input:    "/api/v1/users",
			expected: "/api/v1/users",
		},
		{
			name:     "double slashes are cleaned",
			input:    "/api//v1///users",
			expected: "/api/v1/users",
		},
		{
			name:     "current directory dots are resolved",
			input:    "/api/./v1/users",
			expected: "/api/v1/users",
		},
		{
			name:     "parent directory dots are resolved",
			input:    "/api/v1/../v2/users",
			expected: "/api/v2/users",
		},
		{
			name:     "root path",
			input:    "/",
			expected: "/",
		},
		{
			name:     "empty path becomes root",
			input:    "",
			expected: "/",
		},
		{
			name:     "dot path becomes root",
			input:    ".",
			expected: "/",
		},
		{
			name:     "complex path with multiple issues",
			input:    "/api//v1/./users/../posts",
			expected: "/api/v1/posts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := proxy.NormalizeURLPath(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeURLPath(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestHeadersToJSON(t *testing.T) {
	tests := []struct {
		name     string
		headers  http.Header
		expected string
	}{
		{
			name:     "nil headers return empty object",
			headers:  nil,
			expected: "{}",
		},
		{
			name:     "empty headers return empty object",
			headers:  make(http.Header),
			expected: "{}",
		},
		{
			name: "single header",
			headers: http.Header{
				"Content-Type": []string{"application/json"},
			},
			expected: `{"Content-Type":["application/json"]}`,
		},
		{
			name: "multiple headers",
			headers: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Custom":     []string{"value1", "value2"},
			},
			expected: `{"Content-Type":["application/json"],"X-Custom":["value1","value2"]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := proxy.HeadersToJSON(tt.headers)
			if result != tt.expected {
				t.Errorf("HeadersToJSON() = %q, expected %q", result, tt.expected)
			}
		})
	}
}
