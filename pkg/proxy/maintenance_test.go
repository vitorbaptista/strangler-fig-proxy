package proxy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestDatabase(t *testing.T) *Database {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "maintenance_test.db")

	database, err := InitDatabase(dbPath)
	if err != nil {
		t.Fatalf("InitDatabase() failed: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
		os.Remove(dbPath)
	})
	return database
}

func countRequests(t *testing.T, d *Database) int {
	t.Helper()
	var count int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM requests`).Scan(&count); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	return count
}

func TestRunMaintenanceRetention(t *testing.T) {
	database := newTestDatabase(t)

	insert := `INSERT INTO requests (timestamp, method, url_path) VALUES (datetime('now', ?), 'GET', ?)`
	if _, err := database.db.Exec(insert, "-10 days", "/old"); err != nil {
		t.Fatalf("insert failed: %v", err)
	}
	if _, err := database.db.Exec(insert, "-1 days", "/recent"); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	if err := database.RunMaintenance(7, 0); err != nil {
		t.Fatalf("RunMaintenance() failed: %v", err)
	}

	if got := countRequests(t, database); got != 1 {
		t.Errorf("expected 1 row after retention cleanup, got %d", got)
	}

	var urlPath string
	if err := database.db.QueryRow(`SELECT url_path FROM requests`).Scan(&urlPath); err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if urlPath != "/recent" {
		t.Errorf("expected /recent to survive, got %q", urlPath)
	}
}

func TestRunMaintenanceRetentionDisabled(t *testing.T) {
	database := newTestDatabase(t)

	insert := `INSERT INTO requests (timestamp, method, url_path) VALUES (datetime('now', '-100 days'), 'GET', '/ancient')`
	if _, err := database.db.Exec(insert); err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	if err := database.RunMaintenance(0, 0); err != nil {
		t.Fatalf("RunMaintenance() failed: %v", err)
	}

	if got := countRequests(t, database); got != 1 {
		t.Errorf("expected row to survive with retention disabled, got %d rows", got)
	}
}

func TestRunMaintenanceMaxSize(t *testing.T) {
	database := newTestDatabase(t)

	// Insert ~3MB of data (300 rows x 10KB bodies).
	body := strings.Repeat("x", 10*1024)
	for i := 0; i < 300; i++ {
		record := &RequestRecord{
			Method:   "GET",
			URLPath:  fmt.Sprintf("/big/%d", i),
			MainBody: body,
			NewBody:  body,
			ServedBy: "main",
		}
		if err := database.InsertRequest(record); err != nil {
			t.Fatalf("InsertRequest() failed: %v", err)
		}
	}

	before := countRequests(t, database)
	if err := database.RunMaintenance(0, 1); err != nil {
		t.Fatalf("RunMaintenance() failed: %v", err)
	}
	after := countRequests(t, database)

	if after >= before {
		t.Errorf("expected size cleanup to delete rows, before=%d after=%d", before, after)
	}

	size, err := database.sizeBytes()
	if err != nil {
		t.Fatalf("sizeBytes() failed: %v", err)
	}
	if size > 1024*1024 {
		t.Errorf("expected database size <= 1MB after cleanup, got %d bytes", size)
	}
}

func TestStartMaintenanceStop(t *testing.T) {
	database := newTestDatabase(t)

	stop := database.StartMaintenance(7, 100, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	stop()
	stop() // stopping twice must be safe
}
