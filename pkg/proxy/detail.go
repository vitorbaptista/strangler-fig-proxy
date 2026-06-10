package proxy

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

// DiffLine is one aligned row of the side-by-side response body diff.
type DiffLine struct {
	Main    string
	New     string
	Differs bool
}

// prettyJSON re-indents JSON bodies so the diff highlights semantic
// differences instead of formatting noise. Non-JSON bodies pass through.
func prettyJSON(body string) string {
	var parsed interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return body
	}
	pretty, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		return body
	}
	return string(pretty)
}

// DiffBodies aligns the two response bodies line by line (longest common
// subsequence) and marks the lines that differ.
func DiffBodies(mainBody, newBody string) []DiffLine {
	mainLines := strings.Split(prettyJSON(mainBody), "\n")
	newLines := strings.Split(prettyJSON(newBody), "\n")

	// LCS is O(n*m); fall back to a simple pairwise comparison for huge bodies.
	if len(mainLines)*len(newLines) > 4_000_000 {
		return pairwiseDiff(mainLines, newLines)
	}

	// Standard LCS table.
	lcs := make([][]int, len(mainLines)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(newLines)+1)
	}
	for i := len(mainLines) - 1; i >= 0; i-- {
		for j := len(newLines) - 1; j >= 0; j-- {
			if mainLines[i] == newLines[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var lines []DiffLine
	i, j := 0, 0
	for i < len(mainLines) && j < len(newLines) {
		switch {
		case mainLines[i] == newLines[j]:
			lines = append(lines, DiffLine{Main: mainLines[i], New: newLines[j]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			lines = append(lines, DiffLine{Main: mainLines[i], Differs: true})
			i++
		default:
			lines = append(lines, DiffLine{New: newLines[j], Differs: true})
			j++
		}
	}
	for ; i < len(mainLines); i++ {
		lines = append(lines, DiffLine{Main: mainLines[i], Differs: true})
	}
	for ; j < len(newLines); j++ {
		lines = append(lines, DiffLine{New: newLines[j], Differs: true})
	}

	return lines
}

func pairwiseDiff(mainLines, newLines []string) []DiffLine {
	n := len(mainLines)
	if len(newLines) > n {
		n = len(newLines)
	}
	lines := make([]DiffLine, 0, n)
	for k := 0; k < n; k++ {
		line := DiffLine{}
		if k < len(mainLines) {
			line.Main = mainLines[k]
		}
		if k < len(newLines) {
			line.New = newLines[k]
		}
		line.Differs = line.Main != line.New
		lines = append(lines, line)
	}
	return lines
}

func (h *Handler) handleRequestDetail(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	var record RequestRecord
	var timestamp, queryParams, mismatchType, servedBy sql.NullString
	var responsesMatch sql.NullBool
	err = h.database.db.QueryRow(`
		SELECT timestamp, method, url_path, query_params, request_headers, request_body,
		       main_status, main_headers, main_body, main_response_time_ms,
		       new_status, new_headers, new_body, new_response_time_ms,
		       responses_match, mismatch_type, served_by
		  FROM requests WHERE id = ?`, id).Scan(
		&timestamp, &record.Method, &record.URLPath, &queryParams,
		&record.RequestHeaders, &record.RequestBody,
		&record.MainStatus, &record.MainHeaders, &record.MainBody, &record.MainResponseTimeMs,
		&record.NewStatus, &record.NewHeaders, &record.NewBody, &record.NewResponseTimeMs,
		&responsesMatch, &mismatchType, &servedBy,
	)
	if err == sql.ErrNoRows {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Error("failed to query request detail", "id", id, "error", err)
		http.Error(w, "failed to query request", http.StatusInternalServerError)
		return
	}
	record.ID = id
	record.QueryParams = queryParams.String
	record.MismatchType = mismatchType.String
	record.ServedBy = servedBy.String
	record.ResponsesMatch = responsesMatch.Valid && responsesMatch.Bool

	data := struct {
		Record     RequestRecord
		Timestamp  string
		Diff       []DiffLine
		TokenQuery string
	}{
		Record:     record,
		Timestamp:  timestamp.String,
		Diff:       DiffBodies(record.MainBody, record.NewBody),
		TokenQuery: tokenQuery(r),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := templates.ExecuteTemplate(w, "detail.html", data); err != nil {
		slog.Error("failed to render request detail", "id", id, "error", err)
	}
}
