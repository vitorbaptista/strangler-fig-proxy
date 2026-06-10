package proxy

import (
	"fmt"
	"strconv"
	"strings"
)

// Config holds the static proxy configuration. Routing state that can change
// at runtime lives in RouteTable.
type Config struct {
	MainServerURL         string
	NewServerURL          string
	SamplingRate          float64
	DatabasePath          string
	DatabaseMaxSizeMB     int
	DatabaseRetentionDays int
	Port                  string
	Routes                []Route
	// DashboardToken, when set, gates the /__strangler_fig dashboard and
	// APIs: requests must carry it as "Authorization: Bearer <token>" or a
	// "token" query parameter. Empty means no authentication (isolated
	// deployments only - the requests API exposes recorded traffic).
	DashboardToken string
}

// Route directs requests whose path starts with Prefix to the new server for
// Percentage (0-100) of the matching traffic. The remainder keeps being
// served by the main server (while still being compared in the background).
type Route struct {
	Prefix     string  `json:"prefix"`
	Percentage float64 `json:"percentage"`
}

func (r Route) Validate() error {
	if !strings.HasPrefix(r.Prefix, "/") {
		return fmt.Errorf("route prefix %q must start with /", r.Prefix)
	}
	if r.Percentage < 0 || r.Percentage > 100 {
		return fmt.Errorf("route %q percentage must be between 0 and 100, got %v", r.Prefix, r.Percentage)
	}
	return nil
}

// ParseRoutes parses a comma-separated routes definition such as
// "/api/v2=25,/health" where each entry is "prefix" (implies 100%) or
// "prefix=percentage".
func ParseRoutes(value string) ([]Route, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}

	var routes []Route
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}

		route := Route{Prefix: item, Percentage: 100}
		if idx := strings.Index(item, "="); idx >= 0 {
			route.Prefix = strings.TrimSpace(item[:idx])
			percentage, err := strconv.ParseFloat(strings.TrimSpace(item[idx+1:]), 64)
			if err != nil {
				return nil, fmt.Errorf("invalid percentage in route %q: %w", item, err)
			}
			route.Percentage = percentage
		}

		if err := route.Validate(); err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}

	return routes, nil
}
