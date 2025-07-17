package test

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"strangler-fix-proxy/pkg/proxy"

	_ "github.com/mattn/go-sqlite3"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestBasicProxyFlow(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxy := setupProxy(t, mainServer.URL, newServer.URL, 1.0)
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + "/test")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if !strings.Contains(string(body), `"server": "main"`) {
		t.Errorf("Expected main server response, got: %s", string(body))
	}
}

func TestResponseComparison(t *testing.T) {
	mainServer := NewMainServer()
	differentServer := NewDifferentServer()
	defer mainServer.Close()
	defer differentServer.Close()

	dbPath := "/tmp/test_proxy.db"
	defer os.Remove(dbPath)

	proxy := setupProxyWithDB(t, mainServer.URL, differentServer.URL, 1.0, dbPath)
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + "/test")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	time.Sleep(100 * time.Millisecond)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	var responsesMatch bool
	var mismatchType string
	err = db.QueryRow("SELECT responses_match, mismatch_type FROM requests WHERE url_path = '/test'").Scan(&responsesMatch, &mismatchType)
	if err != nil {
		t.Fatalf("Failed to query database: %v", err)
	}

	if responsesMatch {
		t.Error("Expected responses to not match")
	}

	if mismatchType != "body" {
		t.Errorf("Expected mismatch type 'body', got '%s'", mismatchType)
	}
}

func TestPOSTWithBody(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxy := setupProxy(t, mainServer.URL, newServer.URL, 1.0)
	defer proxy.Close()

	requestBody := `{"test": "data"}`
	resp, err := http.Post(proxy.URL+"/api/test", "application/json", strings.NewReader(requestBody))
	if err != nil {
		t.Fatalf("Failed to make POST request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if !strings.Contains(string(body), `"server": "main"`) {
		t.Errorf("Expected main server response, got: %s", string(body))
	}
}

func TestNewServerError(t *testing.T) {
	mainServer := NewMainServer()
	errorServer := NewErrorServer()
	defer mainServer.Close()
	defer errorServer.Close()

	proxy := setupProxy(t, mainServer.URL, errorServer.URL, 1.0)
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + "/test")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 (main server response), got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if !strings.Contains(string(body), `"server": "main"`) {
		t.Errorf("Expected main server response, got: %s", string(body))
	}
}

func TestSamplingRate(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	dbPath := "/tmp/test_proxy_sampling.db"
	defer os.Remove(dbPath)

	proxy := setupProxyWithDB(t, mainServer.URL, newServer.URL, 0.0, dbPath)
	defer proxy.Close()

	for i := 0; i < 10; i++ {
		resp, err := http.Get(proxy.URL + fmt.Sprintf("/test-%d", i))
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		resp.Body.Close()
	}

	time.Sleep(100 * time.Millisecond)

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM requests").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query database: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 logged requests with sampling rate 0.0, got %d", count)
	}
}

func TestDashboard(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxy := setupProxy(t, mainServer.URL, newServer.URL, 1.0)
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + "/__strangler_fig")
	if err != nil {
		t.Fatalf("Failed to make request to dashboard: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200 for dashboard, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read dashboard response: %v", err)
	}

	if !strings.Contains(string(body), "Strangler Fig Proxy Dashboard") {
		t.Error("Expected dashboard HTML content")
	}
}

func TestNewServerRouting(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	dbPath := "/tmp/test_proxy_routing.db"
	defer os.Remove(dbPath)

	config := &proxy.Config{
		MainServerURL:   mainServer.URL,
		NewServerURL:    newServer.URL,
		SamplingRate:    1.0,
		DatabasePath:    dbPath,
		NewServerRoutes: []string{"/api/v2", "/health"},
	}

	database, err := proxy.InitDatabase(config.DatabasePath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	proxyHandler := proxy.NewProxyHandler(config, database)
	proxyServer := httptest.NewServer(proxyHandler)
	defer proxyServer.Close()

	// Test request to new server route
	resp, err := http.Get(proxyServer.URL + "/api/v2/test")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if !strings.Contains(string(body), `"server": "new"`) {
		t.Errorf("Expected new server response for /api/v2, got: %s", string(body))
	}

	// Test request to main server route
	resp2, err := http.Get(proxyServer.URL + "/api/v1/test")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp2.Body.Close()

	body2, err := io.ReadAll(resp2.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if !strings.Contains(string(body2), `"server": "main"`) {
		t.Errorf("Expected main server response for /api/v1, got: %s", string(body2))
	}
}

func TestMultipartFormData(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxy := setupProxy(t, mainServer.URL, newServer.URL, 1.0)
	defer proxy.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add a form field
	field, err := writer.CreateFormField("username")
	if err != nil {
		t.Fatalf("Failed to create form field: %v", err)
	}
	field.Write([]byte("testuser"))

	// Add a file field
	fileField, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatalf("Failed to create file field: %v", err)
	}
	fileField.Write([]byte("test file content"))

	writer.Close()

	req, err := http.NewRequest("POST", proxy.URL+"/upload", &buf)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if !strings.Contains(string(body), `"server": "main"`) {
		t.Errorf("Expected main server response, got: %s", string(body))
	}
}

func TestLargeRequestBody(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxy := setupProxy(t, mainServer.URL, newServer.URL, 1.0)
	defer proxy.Close()

	// Create a large request body (1MB)
	largeBody := make([]byte, 1024*1024)
	for i := range largeBody {
		largeBody[i] = 'A' + byte(i%26)
	}

	resp, err := http.Post(proxy.URL+"/large", "text/plain", bytes.NewReader(largeBody))
	if err != nil {
		t.Fatalf("Failed to make POST request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if !strings.Contains(string(body), `"server": "main"`) {
		t.Errorf("Expected main server response, got: %s", string(body))
	}
}

func TestQueryStringPreservation(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	dbPath := "/tmp/test_proxy_query.db"
	defer os.Remove(dbPath)

	proxy := setupProxyWithDB(t, mainServer.URL, newServer.URL, 1.0, dbPath)
	defer proxy.Close()

	// Make request with complex query string
	queryURL := proxy.URL + "/api/test?param1=value1&param2=value%202&param3=123&param3=456"
	resp, err := http.Get(queryURL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	time.Sleep(100 * time.Millisecond)

	// Check database for query string preservation
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	var queryParams string
	err = db.QueryRow("SELECT query_params FROM requests WHERE url_path = '/api/test'").Scan(&queryParams)
	if err != nil {
		t.Fatalf("Failed to query database: %v", err)
	}

	expectedQuery := "param1=value1&param2=value%202&param3=123&param3=456"
	if queryParams != expectedQuery {
		t.Errorf("Expected query params %q, got %q", expectedQuery, queryParams)
	}
}

func TestFormDataContentType(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxy := setupProxy(t, mainServer.URL, newServer.URL, 1.0)
	defer proxy.Close()

	formData := "username=testuser&password=testpass"
	req, err := http.NewRequest("POST", proxy.URL+"/login", strings.NewReader(formData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}

	if !strings.Contains(string(body), `"server": "main"`) {
		t.Errorf("Expected main server response, got: %s", string(body))
	}
}

func TestMainServerUnavailable(t *testing.T) {
	newServer := NewNewServer()
	defer newServer.Close()

	// Use a non-existent server URL for main server
	proxy := setupProxy(t, "http://localhost:99999", newServer.URL, 1.0)
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + "/test")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503 when main server unavailable, got %d", resp.StatusCode)
	}
}

func TestBothServersUnavailable(t *testing.T) {
	// Use non-existent server URLs for both servers
	proxy := setupProxy(t, "http://localhost:99999", "http://localhost:99998", 1.0)
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + "/test")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503 when both servers unavailable, got %d", resp.StatusCode)
	}
}

func setupProxy(t *testing.T, mainURL, newURL string, samplingRate float64) *httptest.Server {
	return setupProxyWithDB(t, mainURL, newURL, samplingRate, "/tmp/test_proxy_default.db")
}

func setupProxyWithDB(t *testing.T, mainURL, newURL string, samplingRate float64, dbPath string) *httptest.Server {
	os.Remove(dbPath)

	os.Setenv("MAIN_SERVER_URL", mainURL)
	os.Setenv("NEW_SERVER_URL", newURL)
	os.Setenv("SAMPLING_RATE", fmt.Sprintf("%.1f", samplingRate))
	os.Setenv("DATABASE_PATH", dbPath)

	config := &proxy.Config{
		MainServerURL: mainURL,
		NewServerURL:  newURL,
		SamplingRate:  samplingRate,
		DatabasePath:  dbPath,
	}

	database, err := proxy.InitDatabase(config.DatabasePath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	proxyHandler := proxy.NewProxyHandler(config, database)

	return httptest.NewServer(proxyHandler)
}

// Import the main package types and functions
// This file uses the main package's types to avoid duplication
