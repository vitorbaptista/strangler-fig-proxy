package proxy

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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
		slog.Error("failed to insert request record", "error", err)
	}

	return err
}

func (d *Database) Close() error {
	return d.db.Close()
}

// RunMaintenance enforces the retention period and maximum database size by
// deleting the oldest request logs. Either limit can be disabled by passing 0.
func (d *Database) RunMaintenance(retentionDays, maxSizeMB int) error {
	if retentionDays > 0 {
		cutoff := fmt.Sprintf("-%d days", retentionDays)
		if _, err := d.db.Exec(`DELETE FROM requests WHERE timestamp < datetime('now', ?)`, cutoff); err != nil {
			return fmt.Errorf("retention cleanup failed: %w", err)
		}
	}

	if maxSizeMB > 0 {
		maxBytes := int64(maxSizeMB) * 1024 * 1024
		for {
			size, err := d.sizeBytes()
			if err != nil {
				return fmt.Errorf("size check failed: %w", err)
			}
			if size <= maxBytes {
				break
			}

			// Delete the oldest 20% of rows per pass so even very large
			// databases shrink in a bounded number of (expensive) VACUUMs.
			var count int64
			if err := d.db.QueryRow(`SELECT COUNT(*) FROM requests`).Scan(&count); err != nil {
				return fmt.Errorf("size cleanup failed: %w", err)
			}
			batch := count / 5
			if batch < 1000 {
				batch = 1000
			}

			result, err := d.db.Exec(`DELETE FROM requests WHERE id IN (SELECT id FROM requests ORDER BY id ASC LIMIT ?)`, batch)
			if err != nil {
				return fmt.Errorf("size cleanup failed: %w", err)
			}
			deleted, _ := result.RowsAffected()
			if deleted == 0 {
				break
			}

			// Reclaim the freed pages so sizeBytes reflects the deletions.
			if _, err := d.db.Exec(`VACUUM`); err != nil {
				return fmt.Errorf("vacuum failed: %w", err)
			}
		}
	}

	// Keep the WAL file from growing unbounded.
	if _, err := d.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("wal checkpoint failed: %w", err)
	}

	return nil
}

func (d *Database) sizeBytes() (int64, error) {
	var pageCount, pageSize int64
	if err := d.db.QueryRow(`PRAGMA page_count`).Scan(&pageCount); err != nil {
		return 0, err
	}
	if err := d.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		return 0, err
	}
	return pageCount * pageSize, nil
}

// StartMaintenance runs RunMaintenance immediately and then on the given
// interval until the returned stop function is called. Errors are logged but
// never interrupt the proxy.
func (d *Database) StartMaintenance(retentionDays, maxSizeMB int, interval time.Duration) (stop func()) {
	done := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			if err := d.RunMaintenance(retentionDays, maxSizeMB); err != nil {
				slog.Error("database maintenance failed", "error", err)
			}

			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()

	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

// HeadersToJSON serializes headers for storage. It never fails: unmarshalable
// input degrades to "{}" because logging must not break the proxy.
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
