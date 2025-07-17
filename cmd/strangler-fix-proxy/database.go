package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"path"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

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

func normalizeURLPath(rawPath string) string {
	normalized := path.Clean(rawPath)
	if normalized == "." {
		return "/"
	}
	return normalized
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
