package test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

func NewMainServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Server", "main")

		response := fmt.Sprintf(`{"server": "main", "method": "%s", "path": "%s", "body": "%s"}`,
			r.Method, r.URL.Path, readBody(r))

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(response))
	}))
}

func NewNewServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Server", "new")

		response := fmt.Sprintf(`{"server": "new", "method": "%s", "path": "%s", "body": "%s"}`,
			r.Method, r.URL.Path, readBody(r))

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(response))
	}))
}

func NewDifferentServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Server", "new")

		response := fmt.Sprintf(`{"server": "different_response", "method": "%s", "path": "%s", "body": "%s"}`,
			r.Method, r.URL.Path, readBody(r))

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(response))
	}))
}

func NewErrorServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
}

func readBody(r *http.Request) string {
	if r.Body == nil {
		return ""
	}

	buf := make([]byte, 1024)
	n, _ := r.Body.Read(buf)
	body := string(buf[:n])

	return strings.ReplaceAll(body, `"`, `\"`)
}
