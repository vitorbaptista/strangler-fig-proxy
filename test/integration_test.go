package test

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/vitorbaptista/strangler-fig-proxy/pkg/proxy"
)

func TestBasicProxyFlow(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxyServer, _ := setupProxy(t, mainServer.URL, newServer.URL, 1.0, nil)

	if servedBy := getServedBy(t, proxyServer.URL, "/test"); servedBy != "main" {
		t.Errorf("Expected main server response, got %q", servedBy)
	}
}

func TestResponseComparison(t *testing.T) {
	mainServer := NewMainServer()
	differentServer := NewDifferentServer()
	defer mainServer.Close()
	defer differentServer.Close()

	proxyServer, dbPath := setupProxy(t, mainServer.URL, differentServer.URL, 1.0, nil)

	resp, err := http.Get(proxyServer.URL + "/test")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	resp.Body.Close()

	waitForLogged(t, dbPath, "/test", 1)

	db := openDB(t, dbPath)
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

	proxyServer, _ := setupProxy(t, mainServer.URL, newServer.URL, 1.0, nil)

	requestBody := `{"test": "data"}`
	resp, err := http.Post(proxyServer.URL+"/api/test", "application/json", strings.NewReader(requestBody))
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

	proxyServer, _ := setupProxy(t, mainServer.URL, errorServer.URL, 1.0, nil)

	// New server errors must not affect the client's response.
	if servedBy := getServedBy(t, proxyServer.URL, "/test"); servedBy != "main" {
		t.Errorf("Expected main server response, got %q", servedBy)
	}
}

func TestSamplingRateZero(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxyServer, dbPath := setupProxy(t, mainServer.URL, newServer.URL, 0.0, nil)

	for i := 0; i < 10; i++ {
		resp, err := http.Get(proxyServer.URL + fmt.Sprintf("/test-%d", i))
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		resp.Body.Close()
	}

	// With sampling rate 0.0 no logging goroutine is ever started, so the
	// absence of rows can be asserted immediately.
	db := openDB(t, dbPath)
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM requests").Scan(&count); err != nil {
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

	proxyServer, _ := setupProxy(t, mainServer.URL, newServer.URL, 1.0, nil)

	resp, err := http.Get(proxyServer.URL + "/__strangler_fig")
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

	proxyServer, _ := setupProxy(t, mainServer.URL, newServer.URL, 1.0, []proxy.Route{
		{Prefix: "/api/v2", Percentage: 100},
		{Prefix: "/health", Percentage: 100},
	})

	if servedBy := getServedBy(t, proxyServer.URL, "/api/v2/test"); servedBy != "new" {
		t.Errorf("Expected new server response for /api/v2, got %q", servedBy)
	}
	if servedBy := getServedBy(t, proxyServer.URL, "/api/v1/test"); servedBy != "main" {
		t.Errorf("Expected main server response for /api/v1, got %q", servedBy)
	}
}

func TestMultipartFormData(t *testing.T) {
	mainServer := NewMainServer()
	newServer := NewNewServer()
	defer mainServer.Close()
	defer newServer.Close()

	proxyServer, _ := setupProxy(t, mainServer.URL, newServer.URL, 1.0, nil)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	field, err := writer.CreateFormField("username")
	if err != nil {
		t.Fatalf("Failed to create form field: %v", err)
	}
	field.Write([]byte("testuser"))

	fileField, err := writer.CreateFormFile("file", "test.txt")
	if err != nil {
		t.Fatalf("Failed to create file field: %v", err)
	}
	fileField.Write([]byte("test file content"))
	writer.Close()

	req, err := http.NewRequest("POST", proxyServer.URL+"/upload", &buf)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := http.DefaultClient.Do(req)
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

	proxyServer, _ := setupProxy(t, mainServer.URL, newServer.URL, 1.0, nil)

	// 1MB request body
	largeBody := make([]byte, 1024*1024)
	for i := range largeBody {
		largeBody[i] = 'A' + byte(i%26)
	}

	resp, err := http.Post(proxyServer.URL+"/large", "text/plain", bytes.NewReader(largeBody))
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

	proxyServer, dbPath := setupProxy(t, mainServer.URL, newServer.URL, 1.0, nil)

	queryURL := proxyServer.URL + "/api/test?param1=value1&param2=value%202&param3=123&param3=456"
	resp, err := http.Get(queryURL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	waitForLogged(t, dbPath, "/api/test", 1)

	db := openDB(t, dbPath)
	var queryParams string
	if err := db.QueryRow("SELECT query_params FROM requests WHERE url_path = '/api/test'").Scan(&queryParams); err != nil {
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

	proxyServer, _ := setupProxy(t, mainServer.URL, newServer.URL, 1.0, nil)

	formData := "username=testuser&password=testpass"
	req, err := http.NewRequest("POST", proxyServer.URL+"/login", strings.NewReader(formData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
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

	proxyServer, _ := setupProxy(t, "http://localhost:1", newServer.URL, 1.0, nil)

	resp, err := http.Get(proxyServer.URL + "/test")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503 when main server unavailable, got %d", resp.StatusCode)
	}
}

func TestBothServersUnavailable(t *testing.T) {
	proxyServer, _ := setupProxy(t, "http://localhost:1", "http://localhost:2", 1.0, nil)

	resp, err := http.Get(proxyServer.URL + "/test")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503 when both servers unavailable, got %d", resp.StatusCode)
	}
}

func TestHopByHopHeadersNotForwarded(t *testing.T) {
	var receivedKeepAlive string
	upstream := NewHeaderCapturingServer(func(h http.Header) {
		receivedKeepAlive = h.Get("Keep-Alive")
	})
	defer upstream.Close()
	newServer := NewNewServer()
	defer newServer.Close()

	proxyServer, _ := setupProxy(t, upstream.URL, newServer.URL, 1.0, nil)

	req, _ := http.NewRequest("GET", proxyServer.URL+"/test", nil)
	req.Header.Set("Keep-Alive", "timeout=5")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	resp.Body.Close()

	if receivedKeepAlive != "" {
		t.Errorf("Expected hop-by-hop Keep-Alive header to be stripped, upstream received %q", receivedKeepAlive)
	}
}

func TestXForwardedForSet(t *testing.T) {
	var forwardedFor string
	upstream := NewHeaderCapturingServer(func(h http.Header) {
		forwardedFor = h.Get("X-Forwarded-For")
	})
	defer upstream.Close()
	newServer := NewNewServer()
	defer newServer.Close()

	proxyServer, _ := setupProxy(t, upstream.URL, newServer.URL, 1.0, nil)

	resp, err := http.Get(proxyServer.URL + "/test")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	resp.Body.Close()

	if forwardedFor == "" {
		t.Error("Expected X-Forwarded-For to be set on the upstream request")
	}
}

func TestConnectionListedHeadersNotForwarded(t *testing.T) {
	var receivedCustom string
	upstream := NewHeaderCapturingServer(func(h http.Header) {
		receivedCustom = h.Get("X-Drop-Me")
	})
	defer upstream.Close()
	newServer := NewNewServer()
	defer newServer.Close()

	proxyServer, _ := setupProxy(t, upstream.URL, newServer.URL, 1.0, nil)

	// Headers named in the Connection header are hop-by-hop (RFC 7230 6.1)
	// even when they are not in the standard set.
	req, _ := http.NewRequest("GET", proxyServer.URL+"/test", nil)
	req.Header.Set("Connection", "X-Drop-Me")
	req.Header.Set("X-Drop-Me", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	resp.Body.Close()

	if receivedCustom != "" {
		t.Errorf("Expected Connection-listed header to be stripped, upstream received %q", receivedCustom)
	}
}
