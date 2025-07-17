package proxy

import (
	"database/sql"
	"encoding/json"
	"log"
	"path"
	"reflect"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Config struct {
	MainServerURL         string
	NewServerURL          string
	SamplingRate          float64
	DatabasePath          string
	DatabaseMaxSizeMB     int
	DatabaseRetentionDays int
	Port                  string
	NewServerRoutes       []string
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
