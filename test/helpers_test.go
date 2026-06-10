package test

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vitorbaptista/strangler-fig-proxy/pkg/proxy"

	_ "github.com/mattn/go-sqlite3"
)

// setupProxy starts a proxy in front of the two servers and returns it along
// with the path of its SQLite database.
func setupProxy(t *testing.T, mainURL, newURL string, samplingRate float64, routes []proxy.Route) (*httptest.Server, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "proxy.db")

	config := &proxy.Config{
		MainServerURL: mainURL,
		NewServerURL:  newURL,
		SamplingRate:  samplingRate,
		DatabasePath:  dbPath,
		Routes:        routes,
	}

	database, err := proxy.InitDatabase(config.DatabasePath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	server := httptest.NewServer(proxy.NewHandler(config, database))
	t.Cleanup(server.Close)
	return server, dbPath
}

func openDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// getServedBy makes a request through the proxy and reports which server's
// response came back, based on the test servers' bodies.
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
