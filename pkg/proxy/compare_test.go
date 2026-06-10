package proxy

import "testing"

func TestCompareResponses(t *testing.T) {
	tests := []struct {
		name           string
		mainStatus     int
		newStatus      int
		mainBody       string
		newBody        string
		expectMatch    bool
		expectMismatch string
	}{
		{
			name:        "identical responses match",
			mainStatus:  200,
			newStatus:   200,
			mainBody:    "hello",
			newBody:     "hello",
			expectMatch: true,
		},
		{
			name:           "different status",
			mainStatus:     200,
			newStatus:      500,
			mainBody:       "hello",
			newBody:        "hello",
			expectMatch:    false,
			expectMismatch: "status",
		},
		{
			name:           "different body",
			mainStatus:     200,
			newStatus:      200,
			mainBody:       "hello",
			newBody:        "goodbye",
			expectMatch:    false,
			expectMismatch: "body",
		},
		{
			name:        "equivalent JSON with different formatting matches",
			mainStatus:  200,
			newStatus:   200,
			mainBody:    `{"a":1,"b":2}`,
			newBody:     `{"b": 2, "a": 1}`,
			expectMatch: true,
		},
		{
			name:           "different JSON values",
			mainStatus:     200,
			newStatus:      200,
			mainBody:       `{"a":1}`,
			newBody:        `{"a":2}`,
			expectMatch:    false,
			expectMismatch: "body",
		},
		{
			name:        "whitespace-only difference matches",
			mainStatus:  200,
			newStatus:   200,
			mainBody:    "hello\n",
			newBody:     "hello",
			expectMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, mismatchType := CompareResponses(tt.mainStatus, tt.newStatus, tt.mainBody, tt.newBody)
			if match != tt.expectMatch {
				t.Errorf("CompareResponses() match = %v, expected %v", match, tt.expectMatch)
			}
			if mismatchType != tt.expectMismatch {
				t.Errorf("CompareResponses() mismatchType = %q, expected %q", mismatchType, tt.expectMismatch)
			}
		})
	}
}

func TestNormalizeURLPath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"/a/b", "/a/b"},
		{"//a//b", "/a/b"},
		{"/a/./b", "/a/b"},
		{"/a/../b", "/b"},
		{"", "/"},
	}

	for _, tt := range tests {
		if got := NormalizeURLPath(tt.input); got != tt.expected {
			t.Errorf("NormalizeURLPath(%q) = %q, expected %q", tt.input, got, tt.expected)
		}
	}
}
