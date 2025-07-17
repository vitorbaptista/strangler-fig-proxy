package proxy

import (
	"bytes"
	"database/sql"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

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

	// Apply sampling rate - if random value is above sampling rate, skip logging
	if rand.Float64() > p.config.SamplingRate {
		p.forwardToMain(w, r)
		return
	}

	p.handleRequest(w, r)
}

func (p *ProxyHandler) handleRequest(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	r.Body = io.NopCloser(bytes.NewBuffer(requestBody))

	var mainResponse *http.Response
	var newResponse *http.Response
	var mainResponseTime, newResponseTime time.Duration
	var mainResponseBody, newResponseBody []byte

	routeToNewServer := p.config.ShouldRouteToNewServer(r.URL.Path)

	if routeToNewServer {
		newResponse, newResponseTime = p.forwardRequest(r, p.config.NewServerURL, requestBody)
		if newResponse != nil {
			newResponseBody, _ = io.ReadAll(newResponse.Body)
			newResponse.Body.Close()
			newResponse.Body = io.NopCloser(bytes.NewBuffer(newResponseBody))
			p.copyResponse(w, newResponse)
		} else {
			http.Error(w, "New server unavailable", http.StatusServiceUnavailable)
		}
		mainResponse, mainResponseTime = p.forwardRequest(r, p.config.MainServerURL, requestBody)
		if mainResponse != nil {
			mainResponseBody, _ = io.ReadAll(mainResponse.Body)
			mainResponse.Body.Close()
			mainResponse.Body = io.NopCloser(bytes.NewBuffer(mainResponseBody))
		}
	} else {
		mainResponse, mainResponseTime = p.forwardRequest(r, p.config.MainServerURL, requestBody)
		if mainResponse != nil {
			mainResponseBody, _ = io.ReadAll(mainResponse.Body)
			mainResponse.Body.Close()
			mainResponse.Body = io.NopCloser(bytes.NewBuffer(mainResponseBody))
			p.copyResponse(w, mainResponse)
		} else {
			http.Error(w, "Main server unavailable", http.StatusServiceUnavailable)
		}
		newResponse, newResponseTime = p.forwardRequest(r, p.config.NewServerURL, requestBody)
		if newResponse != nil {
			newResponseBody, _ = io.ReadAll(newResponse.Body)
			newResponse.Body.Close()
			newResponse.Body = io.NopCloser(bytes.NewBuffer(newResponseBody))
		}
	}

	go p.logRequest(r, requestBody, mainResponse, newResponse, mainResponseTime, newResponseTime, startTime, mainResponseBody, newResponseBody)
}

func (p *ProxyHandler) forwardToMain(w http.ResponseWriter, r *http.Request) {
	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
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

	targetURL, err := url.Parse(serverURL)
	if err != nil {
		log.Printf("Error parsing server URL %s: %v", serverURL, err)
		return nil, 0
	}

	targetURL.Path = r.URL.Path
	targetURL.RawQuery = r.URL.RawQuery

	req, err := http.NewRequest(r.Method, targetURL.String(), bytes.NewReader(requestBody))
	if err != nil {
		log.Printf("Error creating request: %v", err)
		return nil, 0
	}

	for key, values := range r.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error forwarding request to %s: %v", serverURL, err)
		return nil, time.Since(start)
	}

	return resp, time.Since(start)
}

func (p *ProxyHandler) copyResponse(w http.ResponseWriter, resp *http.Response) {
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (p *ProxyHandler) logRequest(r *http.Request, requestBody []byte, mainResp, newResp *http.Response, mainTime, newTime time.Duration, startTime time.Time, mainResponseBody, newResponseBody []byte) {
	record := &RequestRecord{
		Timestamp:          startTime,
		Method:             r.Method,
		URLPath:            NormalizeURLPath(r.URL.Path),
		QueryParams:        r.URL.RawQuery,
		RequestHeaders:     HeadersToJSON(r.Header),
		RequestBody:        string(requestBody),
		MainResponseTimeMs: int(mainTime.Milliseconds()),
		NewResponseTimeMs:  int(newTime.Milliseconds()),
	}

	var mainBody, newBody string
	var mainHeaders, newHeaders string

	if mainResp != nil {
		record.MainStatus = mainResp.StatusCode
		mainHeaders = HeadersToJSON(mainResp.Header)
		record.MainHeaders = mainHeaders
		mainBody = string(mainResponseBody)
		record.MainBody = mainBody
	} else {
		log.Printf("Main server did not respond for %s %s", r.Method, r.URL.Path)
	}

	if newResp != nil {
		record.NewStatus = newResp.StatusCode
		newHeaders = HeadersToJSON(newResp.Header)
		record.NewHeaders = newHeaders
		newBody = string(newResponseBody)
		record.NewBody = newBody
	} else {
		log.Printf("New server did not respond for %s %s", r.Method, r.URL.Path)
	}

	if mainResp != nil && newResp != nil {
		record.ResponsesMatch, record.MismatchType = CompareResponses(
			record.MainStatus, record.NewStatus,
			mainHeaders, newHeaders,
			mainBody, newBody,
		)

		if !record.ResponsesMatch {
			log.Printf("Response mismatch detected for %s %s - Type: %s", r.Method, r.URL.Path, record.MismatchType)
		}
	}

	if err := p.database.InsertRequest(record); err != nil {
		log.Printf("Failed to log request to database: %v", err)
	}
}

func (p *ProxyHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	// Parse limit parameter (default 25, max 200)
	limit := 25
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n > 0 && n <= 200 {
				limit = n
			}
		}
	}

	// Collect overall statistics
	var total, matches, mismatches int
	if err := p.database.db.QueryRow("SELECT COUNT(*) FROM requests").Scan(&total); err != nil {
		http.Error(w, "failed to query total", http.StatusInternalServerError)
		return
	}
	if err := p.database.db.QueryRow("SELECT COUNT(*) FROM requests WHERE responses_match = 1").Scan(&matches); err != nil {
		http.Error(w, "failed to query matches", http.StatusInternalServerError)
		return
	}
	if err := p.database.db.QueryRow("SELECT COUNT(*) FROM requests WHERE responses_match = 0").Scan(&mismatches); err != nil {
		http.Error(w, "failed to query mismatches", http.StatusInternalServerError)
		return
	}

	// Average relative response time (new / main)
	var avgRel sql.NullFloat64
	if err := p.database.db.QueryRow(`SELECT AVG(CAST(new_response_time_ms AS REAL) / NULLIF(main_response_time_ms,0)) FROM requests WHERE main_response_time_ms > 0`).Scan(&avgRel); err != nil {
		http.Error(w, "failed to query avg response time", http.StatusInternalServerError)
		return
	}
	avgRelTime := -1.0
	if avgRel.Valid {
		avgRelTime = avgRel.Float64
	}

	// Recent rows
	rows, err := p.database.db.Query(`
		SELECT id, url_path, mismatch_type, responses_match, main_response_time_ms, new_response_time_ms
		  FROM requests
	  ORDER BY id DESC
		 LIMIT ?`, limit)
	if err != nil {
		http.Error(w, "failed to query rows", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type TableRow struct {
		ID             int64
		URL            string
		MismatchType   string
		ResponsesMatch bool
		ChangeDisplay  string // e.g. +20% / -35% / —
	}

	var tableRows []TableRow
	for rows.Next() {
		var (
			id                            int64
			urlPath                       string
			mismatchType                  sql.NullString
			responsesMatchNull            sql.NullBool
			mainRespTimeMs, newRespTimeMs sql.NullInt64
		)

		if err := rows.Scan(&id, &urlPath, &mismatchType, &responsesMatchNull, &mainRespTimeMs, &newRespTimeMs); err != nil {
			continue // skip bad row
		}

		responsesMatch := false
		if responsesMatchNull.Valid {
			responsesMatch = responsesMatchNull.Bool
		}

		changeDisplay := "—"
		if mainRespTimeMs.Valid && newRespTimeMs.Valid && mainRespTimeMs.Int64 > 0 {
			ratio := float64(newRespTimeMs.Int64) / float64(mainRespTimeMs.Int64)
			percentChange := (ratio - 1) * 100
			sign := ""
			if percentChange > 0 {
				sign = "+"
			}
			changeDisplay = fmt.Sprintf("%s%d%%", sign, int(math.Round(percentChange)))
		}

		tableRows = append(tableRows, TableRow{
			ID:             id,
			URL:            urlPath,
			MismatchType:   mismatchType.String,
			ResponsesMatch: responsesMatch,
			ChangeDisplay:  changeDisplay,
		})
	}

	// Prepare data for template
	data := struct {
		Total, Matches, Mismatches int
		MatchRatio                 float64
		AvgRelTime                 float64
		AvgChangeDisplay           string
		Limit                      int
		Rows                       []TableRow
	}{
		Total:      total,
		Matches:    matches,
		Mismatches: mismatches,
		MatchRatio: func() float64 {
			if total == 0 {
				return 0
			}
			return float64(matches) / float64(total)
		}(),
		AvgRelTime: avgRelTime,
		AvgChangeDisplay: func() string {
			if avgRelTime < 0 {
				return "—"
			}
			percentChange := (avgRelTime - 1) * 100
			sign := ""
			if percentChange > 0 {
				sign = "+"
			}
			return fmt.Sprintf("%s%d%%", sign, int(math.Round(percentChange)))
		}(),
		Limit: limit,
		Rows:  tableRows,
	}

	const tmplStr = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta http-equiv="refresh" content="10">
  <title>Strangler Fig Proxy Dashboard</title>
  <style>
    :root {
      --good-bg: #d1f5d3;
      --bad-bg:  #f8d1d1;
    }
    body { font-family: system-ui, sans-serif; margin: 2rem; }
    table { border-collapse: collapse; width: 100%; }
    th, td { padding: .5rem .75rem; border-bottom: 1px solid #ddd; text-align: right; }
    tr.good { background: var(--good-bg); }
    tr.bad  { background: var(--bad-bg);  }
    tr:hover { opacity: .9; }
  </style>
</head>
<body>
  <h1>Strangler Fig Proxy Dashboard</h1>

  <section id="stats">
    <p>Total requests: {{.Total}}</p>
    <p>Matches: {{.Matches}}</p>
    <p>Mismatches: {{.Mismatches}}</p>
    <p>Match ratio: {{printf "%.2f" (mul100 .MatchRatio)}} %</p>
    <p>Avg new vs main response time change: {{.AvgChangeDisplay}}</p>
  </section>

  <section id="recent">
    <h2>Last {{.Limit}} requests</h2>
    <table>
      <thead>
        <tr><th>ID</th><th>URL</th><th>Failure reason</th><th>Δ Time</th></tr>
      </thead>
      <tbody>
        {{range .Rows}}
          <tr class="{{if .ResponsesMatch}}good{{else}}bad{{end}}">
            <td>{{.ID}}</td>
            <td>{{.URL}}</td>
            <td>{{if .ResponsesMatch}}—{{else}}{{.MismatchType}}{{end}}</td>
            <td>{{.ChangeDisplay}}</td>
          </tr>
        {{end}}
      </tbody>
    </table>
  </section>
</body>
</html>`

	funcMap := template.FuncMap{
		"mul100": func(f float64) float64 { return f * 100 },
	}

	tmpl, err := template.New("dashboard").Funcs(funcMap).Parse(tmplStr)
	if err != nil {
		http.Error(w, "template parse error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "template exec error", http.StatusInternalServerError)
		return
	}
}
