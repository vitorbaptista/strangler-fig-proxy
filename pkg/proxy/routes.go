package proxy

import (
	"math/rand"
	"strings"
	"sync"
)

// RouteTable is the runtime-mutable routing state: which path prefixes are
// served by the new server and for what share of their traffic. It is safe
// for concurrent use, so the routes API can update it while requests flow.
type RouteTable struct {
	mu     sync.RWMutex
	routes []Route
}

func NewRouteTable(routes []Route) *RouteTable {
	return &RouteTable{routes: append([]Route(nil), routes...)}
}

// Match returns the first route whose prefix matches path.
func (t *RouteTable) Match(path string) (Route, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	for _, route := range t.routes {
		if strings.HasPrefix(path, route.Prefix) {
			return route, true
		}
	}
	return Route{}, false
}

// ShouldRouteToNew decides whether a request for path should be served by the
// new server. For routes with a partial percentage the decision is
// probabilistic, so traffic shifts gradually.
func (t *RouteTable) ShouldRouteToNew(path string) bool {
	route, ok := t.Match(path)
	if !ok {
		return false
	}
	if route.Percentage >= 100 {
		return true
	}
	if route.Percentage <= 0 {
		return false
	}
	return rand.Float64()*100 < route.Percentage
}

// Routes returns a copy of the current routing table.
func (t *RouteTable) Routes() []Route {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return append([]Route(nil), t.routes...)
}

// Set atomically replaces the routing table after validating every route, so
// traffic percentages can be changed without restarting the proxy.
func (t *RouteTable) Set(routes []Route) error {
	for _, route := range routes {
		if err := route.Validate(); err != nil {
			return err
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.routes = append([]Route(nil), routes...)
	return nil
}
