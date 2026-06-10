package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const internalPathPrefix = "/__strangler_fig"

// Handler is the strangler fig reverse proxy. Every sampled request is served
// by one server (per the routing table) and mirrored to the other in the
// background so the responses can be compared.
type Handler struct {
	config   *Config
	routes   *RouteTable
	database *Database
	client   *http.Client
}

func NewHandler(config *Config, database *Database) *Handler {
	return &Handler{
		config:   config,
		routes:   NewRouteTable(config.Routes),
		database: database,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        200,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// Routes exposes the routing table (used by the dashboard and routes API).
func (h *Handler) Routes() *RouteTable {
	return h.routes
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == internalPathPrefix || strings.HasPrefix(r.URL.Path, internalPathPrefix+"/") {
		h.handleInternal(w, r)
		return
	}

	routeToNew := h.routes.ShouldRouteToNew(r.URL.Path)
	// Strict < so a rate of 0.0 never samples; rand.Float64() is in [0, 1),
	// so 1.0 always samples.
	compare := rand.Float64() < h.config.SamplingRate
	h.handleProxy(w, r, routeToNew, compare)
}

// upstreamResponse is a fully buffered response from one of the servers.
// Buffering keeps comparison and logging simple; this proxy targets
// request/response APIs, not streaming.
type upstreamResponse struct {
	status   int
	header   http.Header
	body     []byte
	duration time.Duration
}

func (h *Handler) handleProxy(w http.ResponseWriter, r *http.Request, routeToNew, compare bool) {
	startTime := time.Now()

	requestBody, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read request body", "method", r.Method, "path", r.URL.Path, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Copy everything the mirror request needs: the original request (and its
	// context) is no longer valid once this handler returns.
	method := r.Method
	urlPath := r.URL.Path
	rawQuery := r.URL.RawQuery
	header := r.Header.Clone()
	clientAddr := clientIP(r)
	setForwardedHeaders(header, r)

	primaryURL, mirrorURL := h.config.MainServerURL, h.config.NewServerURL
	if routeToNew {
		primaryURL, mirrorURL = h.config.NewServerURL, h.config.MainServerURL
	}

	primary := h.forward(method, urlPath, rawQuery, header, primaryURL, requestBody)
	servedBy := ""

	switch {
	case primary != nil && routeToNew:
		servedBy = "new"
	case primary != nil:
		servedBy = "main"
	case routeToNew:
		// New server unavailable: fall back to the main server so the client
		// is not affected by the migration target being down.
		slog.Warn("new server unavailable, falling back to main server",
			"method", method, "path", urlPath, "client", clientAddr)
		fallback := h.forward(method, urlPath, rawQuery, header, h.config.MainServerURL, requestBody)
		if fallback != nil {
			servedBy = "main"
			h.writeResponse(w, fallback)
			if compare {
				go h.logComparison(method, urlPath, rawQuery, header, requestBody, startTime, servedBy, fallback, nil)
			}
		} else {
			http.Error(w, "Both servers unavailable", http.StatusServiceUnavailable)
		}
		return
	}

	if primary == nil {
		http.Error(w, "Main server unavailable", http.StatusServiceUnavailable)
	} else {
		h.writeResponse(w, primary)
	}

	if !compare {
		return
	}

	// Mirror to the other server in the background; the client never waits
	// for the comparison request.
	go func() {
		mirror := h.forward(method, urlPath, rawQuery, header, mirrorURL, requestBody)
		mainResp, newResp := primary, mirror
		if routeToNew {
			mainResp, newResp = mirror, primary
		}
		h.logComparison(method, urlPath, rawQuery, header, requestBody, startTime, servedBy, mainResp, newResp)
	}()
}

// forward sends the request to one server and buffers the response. It
// returns nil when the server cannot be reached; HTTP error statuses are
// valid responses.
func (h *Handler) forward(method, urlPath, rawQuery string, header http.Header, serverURL string, body []byte) *upstreamResponse {
	start := time.Now()

	target, err := url.Parse(serverURL)
	if err != nil {
		slog.Error("invalid server URL", "url", serverURL, "error", err)
		return nil
	}
	target.Path = urlPath
	target.RawQuery = rawQuery

	req, err := http.NewRequest(method, target.String(), bytes.NewReader(body))
	if err != nil {
		slog.Error("failed to build upstream request", "url", target.String(), "error", err)
		return nil
	}
	copyHeaders(req.Header, header)

	resp, err := h.client.Do(req)
	if err != nil {
		slog.Error("upstream request failed", "server", serverURL, "method", method, "path", urlPath, "error", err)
		return nil
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("failed to read upstream response", "server", serverURL, "method", method, "path", urlPath, "error", err)
		return nil
	}

	return &upstreamResponse{
		status:   resp.StatusCode,
		header:   resp.Header,
		body:     responseBody,
		duration: time.Since(start),
	}
}

func (h *Handler) writeResponse(w http.ResponseWriter, resp *upstreamResponse) {
	copyHeaders(w.Header(), resp.header)
	w.WriteHeader(resp.status)
	if _, err := w.Write(resp.body); err != nil {
		slog.Debug("failed to write response to client", "error", err)
	}
}

func (h *Handler) logComparison(method, urlPath, rawQuery string, header http.Header, requestBody []byte, startTime time.Time, servedBy string, mainResp, newResp *upstreamResponse) {
	record := &RequestRecord{
		Timestamp:      startTime,
		Method:         method,
		URLPath:        NormalizeURLPath(urlPath),
		QueryParams:    rawQuery,
		RequestHeaders: HeadersToJSON(header),
		RequestBody:    string(requestBody),
		ServedBy:       servedBy,
	}

	if mainResp != nil {
		record.MainStatus = mainResp.status
		record.MainHeaders = HeadersToJSON(mainResp.header)
		record.MainBody = string(mainResp.body)
		record.MainResponseTimeMs = int(mainResp.duration.Milliseconds())
	} else {
		slog.Warn("main server did not respond", "method", method, "path", urlPath)
	}

	if newResp != nil {
		record.NewStatus = newResp.status
		record.NewHeaders = HeadersToJSON(newResp.header)
		record.NewBody = string(newResp.body)
		record.NewResponseTimeMs = int(newResp.duration.Milliseconds())
	} else {
		slog.Warn("new server did not respond", "method", method, "path", urlPath)
	}

	if mainResp != nil && newResp != nil {
		record.ResponsesMatch, record.MismatchType = CompareResponses(
			record.MainStatus, record.NewStatus, record.MainBody, record.NewBody)

		if !record.ResponsesMatch {
			slog.Info("response mismatch",
				"method", method, "path", urlPath, "type", record.MismatchType,
				"main_status", record.MainStatus, "new_status", record.NewStatus)
		}
	}

	if err := h.database.InsertRequest(record); err != nil {
		slog.Error("failed to log request", "error", err)
	}
}

// hopByHopHeaders must not be forwarded by proxies (RFC 7230 section 6.1).
var hopByHopHeaders = map[string]bool{
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

func copyHeaders(dst, src http.Header) {
	// In addition to the standard set, any header named in the Connection
	// header is hop-by-hop (RFC 7230 section 6.1).
	connectionListed := map[string]bool{}
	for _, value := range src.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			if name = strings.TrimSpace(name); name != "" {
				connectionListed[http.CanonicalHeaderKey(name)] = true
			}
		}
	}

	for key, values := range src {
		canonical := http.CanonicalHeaderKey(key)
		if hopByHopHeaders[canonical] || connectionListed[canonical] {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func setForwardedHeaders(header http.Header, r *http.Request) {
	if ip := clientIP(r); ip != "" {
		if prior := header.Get("X-Forwarded-For"); prior != "" {
			header.Set("X-Forwarded-For", prior+", "+ip)
		} else {
			header.Set("X-Forwarded-For", ip)
		}
	}
	if header.Get("X-Forwarded-Host") == "" {
		header.Set("X-Forwarded-Host", r.Host)
	}
	if header.Get("X-Forwarded-Proto") == "" {
		proto := "http"
		if r.TLS != nil {
			proto = "https"
		}
		header.Set("X-Forwarded-Proto", proto)
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
