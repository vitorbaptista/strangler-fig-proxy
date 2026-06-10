package proxy

import (
	"encoding/json"
	"path"
	"reflect"
	"strings"
)

// CompareResponses reports whether the two responses are equivalent and, when
// not, which part differs ("status" or "body"). Headers are intentionally not
// compared: they vary too much between implementations (dates, server names,
// connection management) to be a useful migration signal.
func CompareResponses(mainStatus, newStatus int, mainBody, newBody string) (bool, string) {
	if mainStatus != newStatus {
		return false, "status"
	}

	if !compareBodies(strings.TrimSpace(mainBody), strings.TrimSpace(newBody)) {
		return false, "body"
	}

	return true, ""
}

// compareBodies compares JSON bodies semantically (key order and formatting
// don't matter) and falls back to exact string comparison otherwise.
func compareBodies(mainBody, newBody string) bool {
	if mainBody == newBody {
		return true
	}

	var mainJSON, newJSON interface{}
	if err := json.Unmarshal([]byte(mainBody), &mainJSON); err != nil {
		return false
	}
	if err := json.Unmarshal([]byte(newBody), &newJSON); err != nil {
		return false
	}

	return reflect.DeepEqual(mainJSON, newJSON)
}

// NormalizeURLPath cleans a request path for storage and aggregation
// (collapses double slashes, resolves "." and "..").
func NormalizeURLPath(rawPath string) string {
	normalized := path.Clean(rawPath)
	if normalized == "." {
		return "/"
	}
	return normalized
}
