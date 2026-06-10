package proxy

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
)

//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"mul100": func(f float64) float64 { return f * 100 },
}).ParseFS(templateFS, "templates/*.html"))

func (h *Handler) handleInternal(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == internalPathPrefix:
		h.handleDashboard(w, r)
	case path == internalPathPrefix+"/api/routes":
		h.handleRoutesAPI(w, r)
	case path == internalPathPrefix+"/api/stats":
		h.handleStatsAPI(w, r)
	case path == internalPathPrefix+"/api/requests":
		h.handleRequestsAPI(w, r)
	case path == internalPathPrefix+"/api/tests.hurl":
		h.handleHurlAPI(w, r)
	case strings.HasPrefix(path, internalPathPrefix+"/requests/"):
		h.handleRequestDetail(w, r, strings.TrimPrefix(path, internalPathPrefix+"/requests/"))
	default:
		http.NotFound(w, r)
	}
}

// handleRoutesAPI exposes the routing table for runtime inspection (GET) and
// modification (PUT), so migration percentages can be adjusted without
// restarting the proxy.
func (h *Handler) handleRoutesAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// fall through to the response below
	case http.MethodPut, http.MethodPost:
		var routes []Route
		if err := json.NewDecoder(r.Body).Decode(&routes); err != nil {
			http.Error(w, fmt.Sprintf("invalid routes JSON: %v", err), http.StatusBadRequest)
			return
		}
		if err := h.routes.Set(routes); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		slog.Info("routing table updated", "routes", routes)
	default:
		w.Header().Set("Allow", "GET, PUT, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	routes := h.routes.Routes()
	if routes == nil {
		routes = []Route{}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(routes); err != nil {
		slog.Error("failed to encode routes response", "error", err)
	}
}

// TableRow is one entry in the dashboard's recent requests table.
type TableRow struct {
	ID             int64
	URL            string
	MismatchType   string
	ResponsesMatch bool
	ChangeDisplay  string // e.g. +20% / -35% / —
}

// PathStat aggregates comparison results for one URL path.
type PathStat struct {
	Path           string  `json:"path"`
	Count          int     `json:"count"`
	MatchPct       float64 `json:"match_pct"`
	ServedByNewPct float64 `json:"served_by_new_pct"`
	AvgMainMs      float64 `json:"avg_main_ms"`
	AvgNewMs       float64 `json:"avg_new_ms"`
}

type dashboardData struct {
	Total, Matches, Mismatches int
	MatchRatio                 float64
	ServedByNewRatio           float64
	AvgChangeDisplay           string
	Limit                      int
	Rows                       []TableRow
	Routes                     []Route
	PathStats                  []PathStat
}

func (h *Handler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	limit := 25
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}

	data, err := h.collectDashboardData(limit)
	if err != nil {
		slog.Error("failed to collect dashboard data", "error", err)
		http.Error(w, "failed to collect dashboard data", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "dashboard.html", data); err != nil {
		slog.Error("failed to render dashboard", "error", err)
	}
}

func (h *Handler) collectDashboardData(limit int) (*dashboardData, error) {
	db := h.database.db
	data := &dashboardData{Limit: limit, Routes: h.routes.Routes()}

	if err := db.QueryRow(`SELECT COUNT(*) FROM requests`).Scan(&data.Total); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM requests WHERE responses_match = 1`).Scan(&data.Matches); err != nil {
		return nil, err
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM requests WHERE responses_match = 0`).Scan(&data.Mismatches); err != nil {
		return nil, err
	}

	var servedByNew int
	if err := db.QueryRow(`SELECT COUNT(*) FROM requests WHERE served_by = 'new'`).Scan(&servedByNew); err != nil {
		return nil, err
	}
	if data.Total > 0 {
		data.MatchRatio = float64(data.Matches) / float64(data.Total)
		data.ServedByNewRatio = float64(servedByNew) / float64(data.Total)
	}

	// Average relative response time (new / main).
	var avgRel sql.NullFloat64
	if err := db.QueryRow(`SELECT AVG(CAST(new_response_time_ms AS REAL) / NULLIF(main_response_time_ms,0)) FROM requests WHERE main_response_time_ms > 0`).Scan(&avgRel); err != nil {
		return nil, err
	}
	data.AvgChangeDisplay = "—"
	if avgRel.Valid {
		data.AvgChangeDisplay = formatPercentChange(avgRel.Float64)
	}

	pathRows, err := db.Query(`
		SELECT url_path,
		       COUNT(*) AS cnt,
		       100.0 * SUM(CASE WHEN responses_match = 1 THEN 1 ELSE 0 END) / COUNT(*),
		       100.0 * SUM(CASE WHEN served_by = 'new' THEN 1 ELSE 0 END) / COUNT(*),
		       COALESCE(AVG(main_response_time_ms), 0),
		       COALESCE(AVG(new_response_time_ms), 0)
		  FROM requests
	  GROUP BY url_path
	  ORDER BY cnt DESC
		 LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer pathRows.Close()
	for pathRows.Next() {
		var s PathStat
		if err := pathRows.Scan(&s.Path, &s.Count, &s.MatchPct, &s.ServedByNewPct, &s.AvgMainMs, &s.AvgNewMs); err != nil {
			continue
		}
		data.PathStats = append(data.PathStats, s)
	}

	rows, err := db.Query(`
		SELECT id, url_path, mismatch_type, responses_match, main_response_time_ms, new_response_time_ms
		  FROM requests
	  ORDER BY id DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			row                           TableRow
			mismatchType                  sql.NullString
			responsesMatch                sql.NullBool
			mainRespTimeMs, newRespTimeMs sql.NullInt64
		)
		if err := rows.Scan(&row.ID, &row.URL, &mismatchType, &responsesMatch, &mainRespTimeMs, &newRespTimeMs); err != nil {
			continue
		}

		row.MismatchType = mismatchType.String
		row.ResponsesMatch = responsesMatch.Valid && responsesMatch.Bool
		row.ChangeDisplay = "—"
		if mainRespTimeMs.Valid && newRespTimeMs.Valid && mainRespTimeMs.Int64 > 0 {
			row.ChangeDisplay = formatPercentChange(float64(newRespTimeMs.Int64) / float64(mainRespTimeMs.Int64))
		}
		data.Rows = append(data.Rows, row)
	}

	return data, nil
}

// formatPercentChange renders a new/main time ratio as "+20%" or "-35%".
func formatPercentChange(ratio float64) string {
	percentChange := (ratio - 1) * 100
	sign := ""
	if percentChange > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s%d%%", sign, int(math.Round(percentChange)))
}
