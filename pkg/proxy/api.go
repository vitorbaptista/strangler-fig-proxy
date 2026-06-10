package proxy

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// statsResponse is the machine-readable view of the dashboard, designed for
// agents driving the migration loop: one call returns overall progress, the
// routing table, and the per-path work queue.
type statsResponse struct {
	Total            int        `json:"total"`
	Matches          int        `json:"matches"`
	Mismatches       int        `json:"mismatches"`
	MatchRatio       float64    `json:"match_ratio"`
	ServedByNewRatio float64    `json:"served_by_new_ratio"`
	Routes           []Route    `json:"routes"`
	Paths            []PathStat `json:"paths"`
}

func (h *Handler) handleStatsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data, err := h.collectDashboardData(0)
	if err != nil {
		slog.Error("failed to collect stats", "error", err)
		http.Error(w, "failed to collect stats", http.StatusInternalServerError)
		return
	}

	routes := data.Routes
	if routes == nil {
		routes = []Route{}
	}
	paths := data.PathStats
	if paths == nil {
		paths = []PathStat{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(statsResponse{
		Total:            data.Total,
		Matches:          data.Matches,
		Mismatches:       data.Mismatches,
		MatchRatio:       data.MatchRatio,
		ServedByNewRatio: data.ServedByNewRatio,
		Routes:           routes,
		Paths:            paths,
	}); err != nil {
		slog.Error("failed to encode stats response", "error", err)
	}
}

// handleRequestsAPI returns recorded request/response pairs as JSON. Agents
// use mismatching records as reproduction cases when fixing an endpoint.
//
// Query parameters:
//   - path:   exact url_path filter
//   - match:  "true" or "false" to filter by comparison result
//   - method: HTTP method filter
//   - limit:  max records (default 20, max 100)
func (h *Handler) handleRequestsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := `
		SELECT id, timestamp, method, url_path, query_params, request_headers, request_body,
		       main_status, main_headers, main_body, main_response_time_ms,
		       new_status, new_headers, new_body, new_response_time_ms,
		       responses_match, mismatch_type, served_by
		  FROM requests`
	var conds []string
	var args []interface{}

	if path := r.URL.Query().Get("path"); path != "" {
		conds = append(conds, "url_path = ?")
		args = append(args, path)
	}
	if match := r.URL.Query().Get("match"); match != "" {
		matched, err := strconv.ParseBool(match)
		if err != nil {
			http.Error(w, "match must be true or false", http.StatusBadRequest)
			return
		}
		conds = append(conds, "responses_match = ?")
		args = append(args, matched)
	}
	if method := r.URL.Query().Get("method"); method != "" {
		conds = append(conds, "method = ?")
		args = append(args, method)
	}

	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	for i, cond := range conds {
		if i == 0 {
			query += " WHERE " + cond
		} else {
			query += " AND " + cond
		}
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := h.database.db.Query(query, args...)
	if err != nil {
		slog.Error("failed to query requests", "error", err)
		http.Error(w, "failed to query requests", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	records := []RequestRecord{}
	for rows.Next() {
		record, err := scanRequestRecord(rows)
		if err != nil {
			slog.Error("failed to scan request record", "error", err)
			continue
		}
		records = append(records, record)
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(records); err != nil {
		slog.Error("failed to encode requests response", "error", err)
	}
}

func scanRequestRecord(rows *sql.Rows) (RequestRecord, error) {
	var record RequestRecord
	var timestamp, queryParams, mismatchType, servedBy sql.NullString
	var responsesMatch sql.NullBool

	err := rows.Scan(
		&record.ID, &timestamp, &record.Method, &record.URLPath, &queryParams,
		&record.RequestHeaders, &record.RequestBody,
		&record.MainStatus, &record.MainHeaders, &record.MainBody, &record.MainResponseTimeMs,
		&record.NewStatus, &record.NewHeaders, &record.NewBody, &record.NewResponseTimeMs,
		&responsesMatch, &mismatchType, &servedBy,
	)
	if err != nil {
		return record, err
	}

	if ts, err := time.Parse("2006-01-02 15:04:05", timestamp.String); err == nil {
		record.Timestamp = ts.UTC()
	}
	record.QueryParams = queryParams.String
	record.MismatchType = mismatchType.String
	record.ServedBy = servedBy.String
	record.ResponsesMatch = responsesMatch.Valid && responsesMatch.Bool
	return record, nil
}
