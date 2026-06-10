package proxy

import (
	"math"
	"reflect"
	"testing"
)

func TestParseRoutes(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    []Route
		expectError bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "bare prefix defaults to 100%",
			input:    "/api/v2",
			expected: []Route{{Prefix: "/api/v2", Percentage: 100}},
		},
		{
			name:  "prefix with percentage",
			input: "/api/v2=25",
			expected: []Route{
				{Prefix: "/api/v2", Percentage: 25},
			},
		},
		{
			name:  "multiple routes with mixed syntax",
			input: "/api/v2=25, /health ,/admin=0",
			expected: []Route{
				{Prefix: "/api/v2", Percentage: 25},
				{Prefix: "/health", Percentage: 100},
				{Prefix: "/admin", Percentage: 0},
			},
		},
		{
			name:  "fractional percentage",
			input: "/api=0.5",
			expected: []Route{
				{Prefix: "/api", Percentage: 0.5},
			},
		},
		{
			name:        "invalid percentage",
			input:       "/api=abc",
			expectError: true,
		},
		{
			name:        "percentage above 100",
			input:       "/api=150",
			expectError: true,
		},
		{
			name:        "negative percentage",
			input:       "/api=-5",
			expectError: true,
		},
		{
			name:        "prefix without leading slash",
			input:       "api/v2=50",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			routes, err := ParseRoutes(tt.input)
			if tt.expectError {
				if err == nil {
					t.Fatalf("ParseRoutes(%q) expected error, got %v", tt.input, routes)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRoutes(%q) unexpected error: %v", tt.input, err)
			}
			if !reflect.DeepEqual(routes, tt.expected) {
				t.Errorf("ParseRoutes(%q) = %v, expected %v", tt.input, routes, tt.expected)
			}
		})
	}
}

func TestRouteTableMatch(t *testing.T) {
	tests := []struct {
		name        string
		routes      []Route
		path        string
		expectMatch bool
	}{
		{
			name:        "empty table matches nothing",
			routes:      nil,
			path:        "/api/v1/test",
			expectMatch: false,
		},
		{
			name:        "matching prefix",
			routes:      []Route{{Prefix: "/api/v2", Percentage: 100}},
			path:        "/api/v2/users",
			expectMatch: true,
		},
		{
			name:        "non-matching prefix",
			routes:      []Route{{Prefix: "/api/v2", Percentage: 100}},
			path:        "/api/v1/users",
			expectMatch: false,
		},
		{
			name:        "exact match",
			routes:      []Route{{Prefix: "/health", Percentage: 100}},
			path:        "/health",
			expectMatch: true,
		},
		{
			name:        "root prefix matches everything",
			routes:      []Route{{Prefix: "/", Percentage: 100}},
			path:        "/anything",
			expectMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := NewRouteTable(tt.routes)
			_, ok := table.Match(tt.path)
			if ok != tt.expectMatch {
				t.Errorf("Match(%q) = %v, expected %v", tt.path, ok, tt.expectMatch)
			}
		})
	}
}

func TestRouteTableShouldRouteToNew(t *testing.T) {
	t.Run("0% never routes to new server", func(t *testing.T) {
		table := NewRouteTable([]Route{{Prefix: "/api", Percentage: 0}})
		for i := 0; i < 200; i++ {
			if table.ShouldRouteToNew("/api/users") {
				t.Fatal("expected 0% route to never go to new server")
			}
		}
	})

	t.Run("100% always routes to new server", func(t *testing.T) {
		table := NewRouteTable([]Route{{Prefix: "/api", Percentage: 100}})
		for i := 0; i < 200; i++ {
			if !table.ShouldRouteToNew("/api/users") {
				t.Fatal("expected 100% route to always go to new server")
			}
		}
	})

	t.Run("50% routes roughly half of traffic", func(t *testing.T) {
		table := NewRouteTable([]Route{{Prefix: "/api", Percentage: 50}})
		newCount := 0
		const trials = 2000
		for i := 0; i < trials; i++ {
			if table.ShouldRouteToNew("/api/users") {
				newCount++
			}
		}
		// Allow a wide margin to keep the test non-flaky.
		if newCount < trials*30/100 || newCount > trials*70/100 {
			t.Errorf("expected roughly 50%% routed to new server, got %d/%d", newCount, trials)
		}
	})

	t.Run("non-matching path never routes", func(t *testing.T) {
		table := NewRouteTable([]Route{{Prefix: "/api", Percentage: 100}})
		if table.ShouldRouteToNew("/other") {
			t.Error("expected non-matching path to stay on main server")
		}
	})
}

func TestRouteTableSet(t *testing.T) {
	table := NewRouteTable([]Route{{Prefix: "/old", Percentage: 100}})

	// Replacing the table takes effect immediately.
	newRoutes := []Route{{Prefix: "/api/v2", Percentage: 50}}
	if err := table.Set(newRoutes); err != nil {
		t.Fatalf("Set() unexpected error: %v", err)
	}
	if got := table.Routes(); !reflect.DeepEqual(got, newRoutes) {
		t.Errorf("Routes() = %v, expected %v", got, newRoutes)
	}
	if table.ShouldRouteToNew("/old/thing") {
		t.Error("expected old route to be removed after Set")
	}

	// Invalid routes are rejected and leave the table unchanged.
	if err := table.Set([]Route{{Prefix: "bad", Percentage: 50}}); err == nil {
		t.Error("expected error for prefix without leading slash")
	}
	if err := table.Set([]Route{{Prefix: "/ok", Percentage: 101}}); err == nil {
		t.Error("expected error for percentage above 100")
	}
	if got := table.Routes(); !reflect.DeepEqual(got, newRoutes) {
		t.Errorf("Routes() after rejected update = %v, expected %v", got, newRoutes)
	}

	// Clearing the table disables routing entirely.
	if err := table.Set(nil); err != nil {
		t.Fatalf("Set(nil) unexpected error: %v", err)
	}
	if table.ShouldRouteToNew("/api/v2/users") {
		t.Error("expected no routing after clearing the table")
	}
}

func TestRouteValidateRejectsNaN(t *testing.T) {
	route := Route{Prefix: "/api", Percentage: math.NaN()}
	if err := route.Validate(); err == nil {
		t.Error("expected error for NaN percentage")
	}
	if _, err := ParseRoutes("/api=NaN"); err == nil {
		t.Error("expected ParseRoutes to reject NaN percentage")
	}
}
