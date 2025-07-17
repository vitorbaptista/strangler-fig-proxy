package test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

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

func setupProxy(t *testing.T, mainURL, newURL string, samplingRate float64) *httptest.Server {
	return setupProxyWithDB(t, mainURL, newURL, samplingRate, "/tmp/test_proxy_default.db")
}

func setupProxyWithDB(t *testing.T, mainURL, newURL string, samplingRate float64, dbPath string) *httptest.Server {
	os.Remove(dbPath)

	os.Setenv("MAIN_SERVER_URL", mainURL)
	os.Setenv("NEW_SERVER_URL", newURL)
	os.Setenv("SAMPLING_RATE", fmt.Sprintf("%.1f", samplingRate))
	os.Setenv("DATABASE_PATH", dbPath)

	config := &Config{
		MainServerURL: mainURL,
		NewServerURL:  newURL,
		SamplingRate:  samplingRate,
		DatabasePath:  dbPath,
	}

	database, err := InitDatabase(config.DatabasePath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	proxy := NewProxyHandler(config, database)

	return httptest.NewServer(proxy)
}

type Config struct {
	MainServerURL   string
	NewServerURL    string
	SamplingRate    float64
	DatabasePath    string
	NewServerRoutes []string
}

func (c *Config) ShouldRouteToNewServer(path string) bool {
	for _, route := range c.NewServerRoutes {
		if strings.HasPrefix(path, route) {
			return true
		}
	}
	return false
}

type Database struct {
	db *sql.DB
}

type RequestRecord struct {
	ID                 int64     `json:"id"`
	Timestamp          time.Time `json:"timestamp"`
	Method             string    `json:"method"`
	URLPath            string    `json:"url_path"`
	QueryParams        string    `json:"query_params"`
	RequestHeaders     string    `json:"request_headers"`
	RequestBody        string    `json:"request_body"`
	MainStatus         int       `json:"main_status"`
	MainHeaders        string    `json:"main_headers"`
	MainBody           string    `json:"main_body"`
	MainResponseTimeMs int       `json:"main_response_time_ms"`
	NewStatus          int       `json:"new_status"`
	NewHeaders         string    `json:"new_headers"`
	NewBody            string    `json:"new_body"`
	NewResponseTimeMs  int       `json:"new_response_time_ms"`
	ResponsesMatch     bool      `json:"responses_match"`
	MismatchType       string    `json:"mismatch_type"`
}

func InitDatabase(dbPath string) (*Database, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}

	database := &Database{db: db}
	if err := database.createTables(); err != nil {
		return nil, err
	}

	return database, nil
}

func (d *Database) createTables() error {
	query := `
	CREATE TABLE IF NOT EXISTS requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		method TEXT NOT NULL,
		url_path TEXT NOT NULL,
		query_params TEXT,
		request_headers TEXT,
		request_body TEXT,
		main_status INTEGER,
		main_headers TEXT,
		main_body TEXT,
		main_response_time_ms INTEGER,
		new_status INTEGER,
		new_headers TEXT,
		new_body TEXT,
		new_response_time_ms INTEGER,
		responses_match BOOLEAN,
		mismatch_type TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_timestamp ON requests(timestamp);
	CREATE INDEX IF NOT EXISTS idx_url_path ON requests(url_path);
	CREATE INDEX IF NOT EXISTS idx_responses_match ON requests(responses_match);
	`

	_, err := d.db.Exec(query)
	return err
}

func (d *Database) InsertRequest(record *RequestRecord) error {
	query := `
	INSERT INTO requests (
		method, url_path, query_params, request_headers, request_body,
		main_status, main_headers, main_body, main_response_time_ms,
		new_status, new_headers, new_body, new_response_time_ms,
		responses_match, mismatch_type
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := d.db.Exec(query,
		record.Method,
		record.URLPath,
		record.QueryParams,
		record.RequestHeaders,
		record.RequestBody,
		record.MainStatus,
		record.MainHeaders,
		record.MainBody,
		record.MainResponseTimeMs,
		record.NewStatus,
		record.NewHeaders,
		record.NewBody,
		record.NewResponseTimeMs,
		record.ResponsesMatch,
		record.MismatchType,
	)

	return err
}

func headersToJSON(headers map[string][]string) string {
	if headers == nil {
		return "{}"
	}

	data, err := json.Marshal(headers)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func compareResponses(mainStatus, newStatus int, mainHeaders, newHeaders, mainBody, newBody string) (bool, string) {
	if mainStatus != newStatus {
		return false, "status"
	}

	if strings.TrimSpace(mainBody) != strings.TrimSpace(newBody) {
		return false, "body"
	}

	if mainHeaders != newHeaders {
		return false, "headers"
	}

	return true, ""
}

type ProxyHandler struct {
	config   *Config
	database *Database
}

func NewProxyHandler(config *Config, database *Database) *ProxyHandler {
	return &ProxyHandler{
		config:   config,
		database: database,
	}
}

func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/__strangler_fig" {
		p.handleDashboard(w, r)
		return
	}

	if p.config.SamplingRate <= 0 {
		p.forwardToMain(w, r)
		return
	}

	p.handleRequest(w, r)
}

func (p *ProxyHandler) handleRequest(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	r.Body = io.NopCloser(bytes.NewBuffer(requestBody))

	var mainResponse *http.Response
	var newResponse *http.Response
	var mainResponseTime, newResponseTime time.Duration

	mainResponse, mainResponseTime = p.forwardRequest(r, p.config.MainServerURL, requestBody)
	if mainResponse != nil {
		p.copyResponse(w, mainResponse)
	} else {
		http.Error(w, "Main server unavailable", http.StatusServiceUnavailable)
	}

	newResponse, newResponseTime = p.forwardRequest(r, p.config.NewServerURL, requestBody)

	go p.logRequest(r, requestBody, mainResponse, newResponse, mainResponseTime, newResponseTime, startTime)
}

func (p *ProxyHandler) forwardToMain(w http.ResponseWriter, r *http.Request) {
	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	r.Body = io.NopCloser(bytes.NewBuffer(requestBody))

	mainResponse, _ := p.forwardRequest(r, p.config.MainServerURL, requestBody)
	if mainResponse != nil {
		p.copyResponse(w, mainResponse)
	} else {
		http.Error(w, "Main server unavailable", http.StatusServiceUnavailable)
	}
}

func (p *ProxyHandler) forwardRequest(r *http.Request, serverURL string, requestBody []byte) (*http.Response, time.Duration) {
	start := time.Now()

	req, err := http.NewRequest(r.Method, serverURL+r.URL.Path+"?"+r.URL.RawQuery, bytes.NewReader(requestBody))
	if err != nil {
		return nil, time.Since(start)
	}

	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, time.Since(start)
	}

	return resp, time.Since(start)
}

func (p *ProxyHandler) copyResponse(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (p *ProxyHandler) logRequest(r *http.Request, requestBody []byte, mainResp, newResp *http.Response, mainTime, newTime time.Duration, startTime time.Time) {
	record := &RequestRecord{
		Timestamp:          startTime,
		Method:             r.Method,
		URLPath:            r.URL.Path,
		QueryParams:        r.URL.RawQuery,
		RequestHeaders:     headersToJSON(r.Header),
		RequestBody:        string(requestBody),
		MainResponseTimeMs: int(mainTime.Milliseconds()),
		NewResponseTimeMs:  int(newTime.Milliseconds()),
	}

	var mainBody, newBody string
	var mainHeaders, newHeaders string

	if mainResp != nil {
		record.MainStatus = mainResp.StatusCode
		mainHeaders = headersToJSON(mainResp.Header)
		record.MainHeaders = mainHeaders

		if bodyBytes, err := io.ReadAll(mainResp.Body); err == nil {
			mainBody = string(bodyBytes)
			record.MainBody = mainBody
		}
		mainResp.Body.Close()
	}

	if newResp != nil {
		record.NewStatus = newResp.StatusCode
		newHeaders = headersToJSON(newResp.Header)
		record.NewHeaders = newHeaders

		if bodyBytes, err := io.ReadAll(newResp.Body); err == nil {
			newBody = string(bodyBytes)
			record.NewBody = newBody
		}
		newResp.Body.Close()
	}

	if mainResp != nil && newResp != nil {
		record.ResponsesMatch, record.MismatchType = compareResponses(
			record.MainStatus, record.NewStatus,
			mainHeaders, newHeaders,
			mainBody, newBody,
		)
	}

	p.database.InsertRequest(record)
}

func (p *ProxyHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`
<!DOCTYPE html>
<html>
<head>
    <title>Strangler Fig Dashboard</title>
</head>
<body>
    <h1>Strangler Fig Proxy Dashboard</h1>
    <p>Dashboard functionality will be implemented in Phase 2.</p>
</body>
</html>
    `))
}
