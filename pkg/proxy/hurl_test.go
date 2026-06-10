package proxy

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestGenerateHurlJSONBody(t *testing.T) {
	records := []RequestRecord{{
		ID:          7,
		Method:      "GET",
		URLPath:     "/api/users",
		QueryParams: "page=1",
		MainStatus:  200,
		MainBody:    `{"total": 2, "items": [{"name": "a"}, {"name": "b"}], "ok": true, "next": null}`,
	}}

	out := GenerateHurl(records)

	for _, want := range []string{
		"GET {{base_url}}/api/users?page=1",
		"HTTP 200",
		"[Asserts]",
		`jsonpath "$['total']" == 2`,
		`jsonpath "$['items']" count == 2`,
		`jsonpath "$['items'][0]['name']" == "a"`,
		`jsonpath "$['items'][1]['name']" == "b"`,
		`jsonpath "$['ok']" == true`,
		`jsonpath "$['next']" == null`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

func TestGenerateHurlTextBody(t *testing.T) {
	t.Run("body with trailing newline uses a readable block", func(t *testing.T) {
		records := []RequestRecord{{
			ID: 1, Method: "GET", URLPath: "/robots.txt", MainStatus: 200,
			MainBody: "User-agent: *\nDisallow: /admin\n",
		}}

		out := GenerateHurl(records)

		if !strings.Contains(out, "```\nUser-agent: *\nDisallow: /admin\n```") {
			t.Errorf("expected exact body block for text response, got:\n%s", out)
		}
	})

	t.Run("body without trailing newline uses base64 for exactness", func(t *testing.T) {
		records := []RequestRecord{{
			ID: 1, Method: "GET", URLPath: "/plain", MainStatus: 200,
			MainBody: "plain text response",
		}}

		out := GenerateHurl(records)

		want := "base64," + base64.StdEncoding.EncodeToString([]byte("plain text response")) + ";"
		if !strings.Contains(out, want) {
			t.Errorf("expected base64 body %q, got:\n%s", want, out)
		}
	})
}

func TestGenerateHurlSkipsWrites(t *testing.T) {
	records := []RequestRecord{
		{ID: 1, Method: "POST", URLPath: "/api/orders", MainStatus: 201, MainBody: `{"id": 1}`},
		{ID: 2, Method: "DELETE", URLPath: "/api/orders/1", MainStatus: 204},
		{ID: 3, Method: "GET", URLPath: "/api/orders", MainStatus: 200, MainBody: `[]`},
	}

	out := GenerateHurl(records)

	if strings.Contains(out, "POST") || strings.Contains(out, "DELETE") {
		t.Errorf("expected write requests to be excluded, got:\n%s", out)
	}
	if !strings.Contains(out, "GET {{base_url}}/api/orders") {
		t.Errorf("expected GET request to be included, got:\n%s", out)
	}
}

func TestGenerateHurlSkipsUnansweredRequests(t *testing.T) {
	records := []RequestRecord{
		{ID: 1, Method: "GET", URLPath: "/down", MainStatus: 0},
	}

	out := GenerateHurl(records)

	if strings.Contains(out, "/down") {
		t.Errorf("expected request without main response to be excluded, got:\n%s", out)
	}
}

func TestGenerateHurlStatusOnlyForEmptyAndHead(t *testing.T) {
	records := []RequestRecord{
		{ID: 1, Method: "GET", URLPath: "/empty", MainStatus: 204, MainBody: ""},
		{ID: 2, Method: "HEAD", URLPath: "/head", MainStatus: 200, MainBody: ""},
	}

	out := GenerateHurl(records)

	if !strings.Contains(out, "GET {{base_url}}/empty\nHTTP 204") {
		t.Errorf("expected status-only entry for empty body, got:\n%s", out)
	}
	if !strings.Contains(out, "HEAD {{base_url}}/head\nHTTP 200") {
		t.Errorf("expected status-only entry for HEAD, got:\n%s", out)
	}
	if strings.Contains(out, "[Asserts]") || strings.Contains(out, "```") {
		t.Errorf("expected no body asserts, got:\n%s", out)
	}
}

func TestGenerateHurlUnsafeKeyFallsBackToExactBody(t *testing.T) {
	body := `{"it's": 1}`
	records := []RequestRecord{{
		ID: 1, Method: "GET", URLPath: "/odd", MainStatus: 200, MainBody: body,
	}}

	out := GenerateHurl(records)

	if strings.Contains(out, "jsonpath") {
		t.Errorf("expected fallback to exact body for unsafe key, got:\n%s", out)
	}
	want := "base64," + base64.StdEncoding.EncodeToString([]byte(body)) + ";"
	if !strings.Contains(out, want) {
		t.Errorf("expected base64 body %q, got:\n%s", want, out)
	}
}

func TestGenerateHurlBodyWithFences(t *testing.T) {
	body := "text with ``` fence\n"
	records := []RequestRecord{{
		ID: 1, Method: "GET", URLPath: "/md", MainStatus: 200, MainBody: body,
	}}

	out := GenerateHurl(records)

	want := "base64," + base64.StdEncoding.EncodeToString([]byte(body)) + ";"
	if !strings.Contains(out, want) {
		t.Errorf("expected base64 body for unembeddable text, got:\n%s", out)
	}
}

func TestJSONAssertsTooLarge(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < maxAssertsPerEntry+10; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("1")
	}
	sb.WriteString("]")

	if _, ok := jsonAsserts(sb.String()); ok {
		t.Error("expected oversized JSON to fall back to exact body assert")
	}
}

func TestGenerateHurlTrailingGarbageFallsBackToExactBody(t *testing.T) {
	// A body starting with valid JSON but with trailing content is not JSON;
	// the proxy's comparison treats it as opaque text, and so must the
	// generated asserts.
	body := `{"a": 1} trailing`
	records := []RequestRecord{{
		ID: 1, Method: "GET", URLPath: "/odd", MainStatus: 200, MainBody: body,
	}}

	out := GenerateHurl(records)

	if strings.Contains(out, "jsonpath") {
		t.Errorf("expected no structural asserts for body with trailing garbage, got:\n%s", out)
	}
	want := "base64," + base64.StdEncoding.EncodeToString([]byte(body)) + ";"
	if !strings.Contains(out, want) {
		t.Errorf("expected base64 exact body, got:\n%s", out)
	}
}
