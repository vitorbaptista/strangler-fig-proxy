package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/vitorbaptista/strangler-fig-proxy/pkg/proxy"
)

func TestPercentageRouting(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxyServer, _ := setupProxy(t, mainServer.URL, newServer.URL, 1.0, []proxy.Route{
		{Prefix: "/full", Percentage: 100},
		{Prefix: "/none", Percentage: 0},
	})

	for i := 0; i < 20; i++ {
		if servedBy := getServedBy(t, proxyServer.URL, "/full/thing"); servedBy != "new" {
			t.Fatalf("Expected /full to always be served by new server, got %q", servedBy)
		}
		if servedBy := getServedBy(t, proxyServer.URL, "/none/thing"); servedBy != "main" {
			t.Fatalf("Expected /none to always be served by main server, got %q", servedBy)
		}
	}
}

func TestRoutesAPI(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxyServer, _ := setupProxy(t, mainServer.URL, newServer.URL, 1.0, []proxy.Route{
		{Prefix: "/api/v2", Percentage: 25},
	})

	// GET returns the current routing table.
	resp, err := http.Get(proxyServer.URL + "/__strangler_fig/api/routes")
	if err != nil {
		t.Fatalf("Failed to GET routes: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200 for GET routes, got %d", resp.StatusCode)
	}

	var routes []proxy.Route
	if err := json.NewDecoder(resp.Body).Decode(&routes); err != nil {
		t.Fatalf("Failed to decode routes: %v", err)
	}
	if len(routes) != 1 || routes[0].Prefix != "/api/v2" || routes[0].Percentage != 25 {
		t.Fatalf("Unexpected routes: %v", routes)
	}

	// PUT replaces the routing table without restarting the proxy.
	update, _ := json.Marshal([]proxy.Route{{Prefix: "/api/v2", Percentage: 100}})
	req, _ := http.NewRequest(http.MethodPut, proxyServer.URL+"/__strangler_fig/api/routes", bytes.NewReader(update))
	req.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to PUT routes: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("Expected status 200 for PUT routes, got %d: %s", resp2.StatusCode, body)
	}

	// The new percentage applies to subsequent requests.
	if servedBy := getServedBy(t, proxyServer.URL, "/api/v2/users"); servedBy != "new" {
		t.Errorf("Expected new server after routes update, got %q", servedBy)
	}

	// Invalid updates are rejected.
	invalid, _ := json.Marshal([]proxy.Route{{Prefix: "/api", Percentage: 150}})
	req3, _ := http.NewRequest(http.MethodPut, proxyServer.URL+"/__strangler_fig/api/routes", bytes.NewReader(invalid))
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatalf("Failed to PUT invalid routes: %v", err)
	}
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid routes, got %d", resp3.StatusCode)
	}
}

func TestNewServerFallback(t *testing.T) {
	mainServer := NewMainServer()
	defer mainServer.Close()

	// Route 100% of traffic to an unreachable new server: the proxy should
	// fall back to the main server instead of failing the request.
	proxyServer, _ := setupProxy(t, mainServer.URL, "http://localhost:1", 1.0, []proxy.Route{
		{Prefix: "/", Percentage: 100},
	})

	if servedBy := getServedBy(t, proxyServer.URL, "/test"); servedBy != "main" {
		t.Errorf("Expected fallback to main server, got %q", servedBy)
	}
}

func TestServedByLogged(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxyServer, dbPath := setupProxy(t, mainServer.URL, newServer.URL, 1.0, []proxy.Route{
		{Prefix: "/migrated", Percentage: 100},
	})

	getServedBy(t, proxyServer.URL, "/migrated/thing")
	getServedBy(t, proxyServer.URL, "/legacy/thing")

	time.Sleep(100 * time.Millisecond)

	db := openDB(t, dbPath)
	var servedBy string
	if err := db.QueryRow("SELECT served_by FROM requests WHERE url_path = '/migrated/thing'").Scan(&servedBy); err != nil {
		t.Fatalf("Failed to query database: %v", err)
	}
	if servedBy != "new" {
		t.Errorf("Expected served_by 'new' for migrated route, got %q", servedBy)
	}

	if err := db.QueryRow("SELECT served_by FROM requests WHERE url_path = '/legacy/thing'").Scan(&servedBy); err != nil {
		t.Fatalf("Failed to query database: %v", err)
	}
	if servedBy != "main" {
		t.Errorf("Expected served_by 'main' for legacy route, got %q", servedBy)
	}
}

func TestDashboardShowsRoutes(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxyServer, _ := setupProxy(t, mainServer.URL, newServer.URL, 1.0, []proxy.Route{
		{Prefix: "/api/v2", Percentage: 42},
	})

	resp, err := http.Get(proxyServer.URL + "/__strangler_fig")
	if err != nil {
		t.Fatalf("Failed to GET dashboard: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read dashboard: %v", err)
	}

	html := string(body)
	if !strings.Contains(html, "/api/v2") {
		t.Error("Expected dashboard to show route prefix /api/v2")
	}
	if !strings.Contains(html, "42") {
		t.Error("Expected dashboard to show route percentage 42")
	}
	if !strings.Contains(html, "Served by new server") {
		t.Error("Expected dashboard to show migration progress stat")
	}
}

func TestRequestDetailPage(t *testing.T) {
	mainServer := NewMainServer()
	differentServer := NewDifferentServer()
	defer mainServer.Close()
	defer differentServer.Close()

	proxyServer, _ := setupProxy(t, mainServer.URL, differentServer.URL, 1.0, nil)

	resp, err := http.Get(proxyServer.URL + "/some/path")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	detail, err := http.Get(proxyServer.URL + "/__strangler_fig/requests/1")
	if err != nil {
		t.Fatalf("Failed to GET request detail: %v", err)
	}
	defer detail.Body.Close()

	if detail.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200 for request detail, got %d", detail.StatusCode)
	}

	body, err := io.ReadAll(detail.Body)
	if err != nil {
		t.Fatalf("Failed to read detail page: %v", err)
	}

	html := string(body)
	if !strings.Contains(html, "/some/path") {
		t.Error("Expected detail page to show the request path")
	}
	if !strings.Contains(html, "mismatch") {
		t.Error("Expected detail page to show mismatch status")
	}
	if !strings.Contains(html, "different_response") {
		t.Error("Expected detail page to show the new server body")
	}

	// Unknown IDs return 404.
	missing, err := http.Get(proxyServer.URL + "/__strangler_fig/requests/99999")
	if err != nil {
		t.Fatalf("Failed to GET missing request: %v", err)
	}
	defer missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404 for unknown request id, got %d", missing.StatusCode)
	}
}

func TestDashboardPerPathStats(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxyServer, _ := setupProxy(t, mainServer.URL, newServer.URL, 1.0, nil)

	for i := 0; i < 3; i++ {
		resp, err := http.Get(proxyServer.URL + "/stats/path")
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		resp.Body.Close()
	}

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get(proxyServer.URL + "/__strangler_fig")
	if err != nil {
		t.Fatalf("Failed to GET dashboard: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read dashboard: %v", err)
	}

	html := string(body)
	if !strings.Contains(html, "Per-path statistics") {
		t.Error("Expected dashboard to show per-path statistics section")
	}
	if !strings.Contains(html, "/stats/path") {
		t.Error("Expected dashboard to list /stats/path in per-path stats")
	}
}

func TestStatsAPI(t *testing.T) {
	mainServer := NewMainServer()
	differentServer := NewDifferentServer()
	defer mainServer.Close()
	defer differentServer.Close()

	proxyServer, _ := setupProxy(t, mainServer.URL, differentServer.URL, 1.0, []proxy.Route{
		{Prefix: "/api/v2", Percentage: 25},
	})

	resp, err := http.Get(proxyServer.URL + "/stats/thing")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	statsResp, err := http.Get(proxyServer.URL + "/__strangler_fig/api/stats")
	if err != nil {
		t.Fatalf("Failed to GET stats: %v", err)
	}
	defer statsResp.Body.Close()

	if statsResp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200 for stats, got %d", statsResp.StatusCode)
	}

	var stats struct {
		Total      int           `json:"total"`
		Mismatches int           `json:"mismatches"`
		Routes     []proxy.Route `json:"routes"`
		Paths      []struct {
			Path  string `json:"path"`
			Count int    `json:"count"`
		} `json:"paths"`
	}
	if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
		t.Fatalf("Failed to decode stats: %v", err)
	}

	if stats.Total != 1 || stats.Mismatches != 1 {
		t.Errorf("Expected 1 total and 1 mismatch, got %+v", stats)
	}
	if len(stats.Routes) != 1 || stats.Routes[0].Prefix != "/api/v2" {
		t.Errorf("Expected configured route in stats, got %+v", stats.Routes)
	}
	if len(stats.Paths) != 1 || stats.Paths[0].Path != "/stats/thing" || stats.Paths[0].Count != 1 {
		t.Errorf("Expected per-path stats for /stats/thing, got %+v", stats.Paths)
	}
}

func TestRequestsAPI(t *testing.T) {
	mainServer := NewMainServer()
	differentServer := NewDifferentServer()
	defer mainServer.Close()
	defer differentServer.Close()

	proxyServer, _ := setupProxy(t, mainServer.URL, differentServer.URL, 1.0, nil)

	for _, path := range []string{"/a", "/a", "/b"} {
		resp, err := http.Get(proxyServer.URL + path)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		resp.Body.Close()
	}

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get(proxyServer.URL + "/__strangler_fig/api/requests?path=/a&match=false&limit=10")
	if err != nil {
		t.Fatalf("Failed to GET requests: %v", err)
	}
	defer resp.Body.Close()

	var records []proxy.RequestRecord
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		t.Fatalf("Failed to decode records: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("Expected 2 records for /a, got %d", len(records))
	}
	for _, record := range records {
		if record.URLPath != "/a" {
			t.Errorf("Expected only /a records, got %q", record.URLPath)
		}
		if record.ResponsesMatch {
			t.Error("Expected only mismatching records")
		}
		if !strings.Contains(record.MainBody, `"server": "main"`) {
			t.Errorf("Expected main body in record, got %q", record.MainBody)
		}
		if !strings.Contains(record.NewBody, "different_response") {
			t.Errorf("Expected new body in record, got %q", record.NewBody)
		}
	}

	// Invalid match parameter is rejected.
	bad, err := http.Get(proxyServer.URL + "/__strangler_fig/api/requests?match=banana")
	if err != nil {
		t.Fatalf("Failed to GET requests: %v", err)
	}
	defer bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400 for invalid match param, got %d", bad.StatusCode)
	}
}

func TestHurlExport(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxyServer, _ := setupProxy(t, mainServer.URL, newServer.URL, 1.0, nil)

	// Two GETs to the same URL (deduplicated), one to another path, one POST
	// (excluded from the export).
	for i := 0; i < 2; i++ {
		resp, err := http.Get(proxyServer.URL + "/api/users?page=1")
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		resp.Body.Close()
	}
	resp, err := http.Get(proxyServer.URL + "/other")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	resp.Body.Close()
	postResp, err := http.Post(proxyServer.URL+"/api/users", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("Failed to make POST request: %v", err)
	}
	postResp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	hurlResp, err := http.Get(proxyServer.URL + "/__strangler_fig/api/tests.hurl?path=/api")
	if err != nil {
		t.Fatalf("Failed to GET hurl export: %v", err)
	}
	defer hurlResp.Body.Close()

	body, err := io.ReadAll(hurlResp.Body)
	if err != nil {
		t.Fatalf("Failed to read hurl export: %v", err)
	}
	suite := string(body)

	if got := strings.Count(suite, "GET {{base_url}}/api/users?page=1"); got != 1 {
		t.Errorf("Expected 1 deduplicated test entry, got %d:\n%s", got, suite)
	}
	if strings.Contains(suite, "/other") {
		t.Errorf("Expected path filter to exclude /other:\n%s", suite)
	}
	if strings.Contains(suite, "POST") {
		t.Errorf("Expected POST to be excluded:\n%s", suite)
	}
	if !strings.Contains(suite, `jsonpath "$['server']" == "main"`) {
		t.Errorf("Expected jsonpath assert on legacy body:\n%s", suite)
	}
}
