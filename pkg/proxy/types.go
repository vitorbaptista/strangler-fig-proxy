package proxy

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"path"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Route directs requests whose path starts with Prefix to the new server
// for Percentage (0-100) of the matching traffic. The remainder keeps being
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

type Config struct {
	MainServerURL         string
	NewServerURL          string
	SamplingRate          float64
	DatabasePath          string
	DatabaseMaxSizeMB     int
	DatabaseRetentionDays int
	Port                  string
	NewServerRoutes       []string // legacy prefix-only routes, treated as 100%
	Routes                []Route

	routesMu sync.RWMutex
}

// MatchRoute returns the first route whose prefix matches path. Legacy
// NewServerRoutes entries are used (at 100%) when no parsed Routes are set.
func (c *Config) MatchRoute(path string) (Route, bool) {
	c.routesMu.RLock()
	defer c.routesMu.RUnlock()

	if len(c.Routes) > 0 {
		for _, route := range c.Routes {
			if strings.HasPrefix(path, route.Prefix) {
				return route, true
			}
		}
		return Route{}, false
	}

	for _, prefix := range c.NewServerRoutes {
		if strings.HasPrefix(path, prefix) {
			return Route{Prefix: prefix, Percentage: 100}, true
		}
	}
	return Route{}, false
}

// ShouldRouteToNewServer decides whether this request should be served by the
// new server. For routes with a partial percentage the decision is
// probabilistic, so traffic can be shifted gradually.
func (c *Config) ShouldRouteToNewServer(path string) bool {
	route, ok := c.MatchRoute(path)
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

// GetRoutes returns a copy of the effective routes.
func (c *Config) GetRoutes() []Route {
	c.routesMu.RLock()
	defer c.routesMu.RUnlock()

	if len(c.Routes) > 0 {
		return append([]Route(nil), c.Routes...)
	}

	routes := make([]Route, 0, len(c.NewServerRoutes))
	for _, prefix := range c.NewServerRoutes {
		routes = append(routes, Route{Prefix: prefix, Percentage: 100})
	}
	return routes
}

// SetRoutes atomically replaces the routing table, allowing traffic
// percentages to be changed at runtime without restarting the proxy.
func (c *Config) SetRoutes(routes []Route) error {
	for _, route := range routes {
		if err := route.Validate(); err != nil {
			return err
		}
	}

	c.routesMu.Lock()
	defer c.routesMu.Unlock()
	c.Routes = append([]Route(nil), routes...)
	c.NewServerRoutes = nil
	return nil
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
	ServedBy           string    `json:"served_by"` // "main" or "new"
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
		mismatch_type TEXT,
		served_by TEXT
	);

	CREATE INDEX IF NOT EXISTS idx_timestamp ON requests(timestamp);
	CREATE INDEX IF NOT EXISTS idx_url_path ON requests(url_path);
	CREATE INDEX IF NOT EXISTS idx_responses_match ON requests(responses_match);
	`

	if _, err := d.db.Exec(query); err != nil {
		return err
	}

	// Migrate databases created before the served_by column existed.
	if _, err := d.db.Exec(`ALTER TABLE requests ADD COLUMN served_by TEXT`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}

	return nil
}

func (d *Database) InsertRequest(record *RequestRecord) error {
	query := `
	INSERT INTO requests (
		method, url_path, query_params, request_headers, request_body,
		main_status, main_headers, main_body, main_response_time_ms,
		new_status, new_headers, new_body, new_response_time_ms,
		responses_match, mismatch_type, served_by
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		record.ServedBy,
	)

	if err != nil {
		log.Printf("Error inserting request record: %v", err)
	}

	return err
}

func (d *Database) Close() error {
	return d.db.Close()
}

func NormalizeURLPath(rawPath string) string {
	normalized := path.Clean(rawPath)
	if normalized == "." {
		return "/"
	}
	return normalized
}

func HeadersToJSON(headers map[string][]string) string {
	if headers == nil {
		return "{}"
	}

	data, err := json.Marshal(headers)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func CompareResponses(mainStatus, newStatus int, mainHeaders, newHeaders, mainBody, newBody string) (bool, string) {
	if mainStatus != newStatus {
		return false, "status"
	}

	if !compareJSONBodies(strings.TrimSpace(mainBody), strings.TrimSpace(newBody)) {
		return false, "body"
	}

	return true, ""
}

func compareJSONBodies(mainBody, newBody string) bool {
	// If both bodies are empty or identical, they match
	if mainBody == newBody {
		return true
	}

	// Try to parse as JSON and compare semantically
	var mainJSON, newJSON interface{}

	if err := json.Unmarshal([]byte(mainBody), &mainJSON); err != nil {
		// If main body is not valid JSON, fall back to string comparison
		return mainBody == newBody
	}

	if err := json.Unmarshal([]byte(newBody), &newJSON); err != nil {
		// If new body is not valid JSON, fall back to string comparison
		return mainBody == newBody
	}

	// Both are valid JSON, compare them semantically
	return reflect.DeepEqual(mainJSON, newJSON)
}
