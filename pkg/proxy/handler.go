package proxy

import (
	"bytes"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
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
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`
<!DOCTYPE html>
<html>
<head>
    <title>Strangler Fig Dashboard</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        .stats { display: flex; gap: 20px; margin-bottom: 20px; }
        .stat-card { border: 1px solid #ddd; padding: 15px; border-radius: 5px; }
        .stat-value { font-size: 24px; font-weight: bold; color: #333; }
        .stat-label { color: #666; }
    </style>
</head>
<body>
    <h1>Strangler Fig Proxy Dashboard</h1>
    <div class="stats">
        <div class="stat-card">
            <div class="stat-value">-</div>
            <div class="stat-label">Total Requests</div>
        </div>
        <div class="stat-card">
            <div class="stat-value">-</div>
            <div class="stat-label">Matches</div>
        </div>
        <div class="stat-card">
            <div class="stat-value">-</div>
            <div class="stat-label">Mismatches</div>
        </div>
    </div>
    <p>Dashboard functionality will be implemented in Phase 2.</p>
    <p>For now, check the SQLite database at: ` + p.config.DatabasePath + `</p>
</body>
</html>
    `))
}
