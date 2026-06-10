package test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"strangler-fix-proxy/pkg/proxy"

	_ "github.com/mattn/go-sqlite3"
)

func setupRoutingProxy(t *testing.T, mainURL, newURL, dbPath string, routes []proxy.Route) (*httptest.Server, *proxy.Config) {
	t.Helper()
	os.Remove(dbPath)

	config := &proxy.Config{
		MainServerURL: mainURL,
		NewServerURL:  newURL,
		SamplingRate:  1.0,
		DatabasePath:  dbPath,
		Routes:        routes,
	}

	database, err := proxy.InitDatabase(config.DatabasePath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
		os.Remove(dbPath)
	})

	server := httptest.NewServer(proxy.NewProxyHandler(config, database))
	t.Cleanup(server.Close)
	return server, config
}

func getServedBy(t *testing.T, proxyURL, path string) string {
	t.Helper()
	resp, err := http.Get(proxyURL + path)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	switch {
	case strings.Contains(string(body), `"server": "main"`):
		return "main"
	case strings.Contains(string(body), `"server": "new"`):
		return "new"
	default:
		t.Fatalf("Unexpected response body: %s", string(body))
		return ""
	}
}

func TestPercentageRouting(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxyServer, _ := setupRoutingProxy(t, mainServer.URL, newServer.URL,
		"/tmp/test_proxy_percentage.db", []proxy.Route{
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

	proxyServer, _ := setupRoutingProxy(t, mainServer.URL, newServer.URL,
		"/tmp/test_proxy_routes_api.db", []proxy.Route{
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
	proxyServer, _ := setupRoutingProxy(t, mainServer.URL, "http://localhost:1",
		"/tmp/test_proxy_fallback.db", []proxy.Route{
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

	dbPath := "/tmp/test_proxy_served_by.db"
	proxyServer, _ := setupRoutingProxy(t, mainServer.URL, newServer.URL, dbPath,
		[]proxy.Route{{Prefix: "/migrated", Percentage: 100}})

	getServedBy(t, proxyServer.URL, "/migrated/thing")
	getServedBy(t, proxyServer.URL, "/legacy/thing")

	time.Sleep(100 * time.Millisecond)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

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

	proxyServer, _ := setupRoutingProxy(t, mainServer.URL, newServer.URL,
		"/tmp/test_proxy_dash_routes.db", []proxy.Route{
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
