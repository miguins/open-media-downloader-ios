package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/miguins/open-media-downloader-ios/internal/id"
)

// logLines decodes the JSON log lines written so far and clears the buffer.
func logLines(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	decoder := json.NewDecoder(logs)
	for decoder.More() {
		var line map[string]any
		if err := decoder.Decode(&line); err != nil {
			t.Fatalf("decode log line: %v", err)
		}
		lines = append(lines, line)
	}
	logs.Reset()

	return lines
}

func findLog(t *testing.T, lines []map[string]any, message string) map[string]any {
	t.Helper()
	for _, line := range lines {
		if line["msg"] == message {
			return line
		}
	}
	t.Fatalf("no %q log in %v", message, lines)

	return nil
}

// assertTraced checks that every log line of one request carries the trace ID returned to the client.
func assertTraced(t *testing.T, response *httptest.ResponseRecorder, lines []map[string]any) string {
	t.Helper()
	trace := response.Header().Get("X-Trace-Id")
	if !id.Valid(trace) {
		t.Fatalf("X-Trace-Id = %q; want a valid identifier", trace)
	}
	for _, line := range lines {
		if line["trace_id"] != trace {
			t.Fatalf("log line %v; want trace_id %q", line, trace)
		}
	}

	return trace
}

func TestEveryResponseHasDistinctTraceID(t *testing.T) {
	f := newAPIFixture(t)
	seen := map[string]bool{}
	for _, request := range []struct{ method, path, key string }{
		{http.MethodGet, "/healthz", ""},
		{http.MethodGet, "/readyz", ""},
		{http.MethodGet, "/unknown", ""},
		{http.MethodPost, "/healthz", ""},
		{http.MethodGet, "/v1/jobs/" + id.New(), ""},
		{http.MethodGet, "/v1/jobs/" + id.New(), f.key},
		{http.MethodGet, "/v1/downloads/invalid", ""},
	} {
		response := f.do(t, request.method, request.path, request.key, "")
		trace := assertTraced(t, response, logLines(t, f.logs))
		if seen[trace] {
			t.Fatalf("trace %q repeated", trace)
		}
		seen[trace] = true
	}
}

func TestAccessLogDescribesRequestWithoutSecrets(t *testing.T) {
	f := newAPIFixture(t)
	created := f.createJob(t)
	f.logs.Reset()

	response := f.do(t, http.MethodGet, "/v1/jobs/"+created.ID, f.key, "")
	lines := logLines(t, f.logs)
	assertTraced(t, response, lines)
	access := findLog(t, lines, "request completed")
	if access["level"] != "INFO" || access["method"] != "GET" || access["route"] != "/v1/jobs/{id}" ||
		access["status"] != float64(http.StatusOK) || access["key_name"] != "owner" || access["key_id"] != f.ownerID ||
		access["job_id"] != created.ID {
		t.Fatalf("access log = %v", access)
	}
	if _, ok := access["duration_ms"].(float64); !ok {
		t.Fatalf("access log = %v; want duration_ms", access)
	}
	if bytes, ok := access["bytes"].(float64); !ok || int(bytes) != response.Body.Len() {
		t.Fatalf("access log bytes = %v; want %d", access["bytes"], response.Body.Len())
	}
	if raw := f.logs.String(); strings.Contains(raw, f.key[32:]) {
		t.Fatalf("logs disclosed the API key secret")
	}
}

func TestAccessLogLevels(t *testing.T) {
	f := newAPIFixture(t)
	tests := []struct {
		path, route, level string
	}{
		{"/readyz", "/readyz", "INFO"},
		{"/unknown", "unmatched", "INFO"},
	}
	for _, test := range tests {
		f.do(t, http.MethodGet, test.path, "", "")
		access := findLog(t, logLines(t, f.logs), "request completed")
		if access["route"] != test.route || access["level"] != test.level {
			t.Fatalf("%s access log = %v; want route %q at %s", test.path, access, test.route, test.level)
		}
	}
}

func TestHealthChecksAreNotLogged(t *testing.T) {
	f := newAPIFixture(t)
	response := f.do(t, http.MethodGet, "/healthz", "", "")
	if response.Code != http.StatusOK || response.Header().Get("X-Trace-Id") == "" {
		t.Fatalf("health check = %d; want 200 with a trace", response.Code)
	}
	if lines := logLines(t, f.logs); len(lines) != 0 {
		t.Fatalf("health check logged %v; want nothing even at debug level", lines)
	}
}

func TestAccessLogErrorLevelForServerErrors(t *testing.T) {
	f := newAPIFixture(t)
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	response := f.do(t, http.MethodGet, "/v1/jobs/"+id.New(), f.key, "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; want 500", response.Code)
	}
	access := findLog(t, logLines(t, f.logs), "request completed")
	if access["level"] != "ERROR" || access["status"] != float64(http.StatusInternalServerError) {
		t.Fatalf("access log = %v; want an error-level 500", access)
	}
}

func TestJobLifecycleLogsShareTheRequestTrace(t *testing.T) {
	f := newAPIFixture(t)
	response := f.do(t, http.MethodPost, "/v1/jobs", f.key, `{"url":"https://vimeo.com/123?token=private"}`)
	created := decodeJob(t, response)
	raw := f.logs.String()
	lines := logLines(t, f.logs)
	assertTraced(t, response, lines)
	event := findLog(t, lines, "job created")
	if event["job_id"] != created.ID || event["platform"] != "vimeo" || event["key_name"] != "owner" {
		t.Fatalf("job created log = %v", event)
	}
	if strings.Contains(raw, "vimeo.com") {
		t.Fatalf("logs disclosed the submitted URL: %s", raw)
	}

	response = f.do(t, http.MethodDelete, "/v1/jobs/"+created.ID, f.key, "")
	lines = logLines(t, f.logs)
	assertTraced(t, response, lines)
	if event := findLog(t, lines, "job canceled"); event["job_id"] != created.ID || event["previous_status"] != "queued" {
		t.Fatalf("job canceled log = %v", event)
	}
	f.do(t, http.MethodDelete, "/v1/jobs/"+created.ID, f.key, "")
	for _, line := range logLines(t, f.logs) {
		if line["msg"] == "job canceled" {
			t.Fatal("repeated cancellation logged a new cancellation")
		}
	}
}

func TestCreateJobLogsRejectionReason(t *testing.T) {
	f := newAPIFixture(t)
	tests := []struct {
		body, contentType, reason string
	}{
		{`{"url":"https://vimeo.com/1"}`, "text/plain", "unsupported_media_type"},
		{`{"url":1}`, "application/json", "invalid_request"},
		{`{"url":"` + strings.Repeat("a", 2048) + `"}`, "application/json", "request_too_large"},
		{`{"url":"https://example.com/private-path"}`, "application/json", "unsupported_url"},
	}
	for _, test := range tests {
		f.do(t, http.MethodPost, "/v1/jobs", f.key, test.body, "Content-Type", test.contentType)
		lines := logLines(t, f.logs)
		if event := findLog(t, lines, "job rejected"); event["reason"] != test.reason || event["level"] != "INFO" {
			t.Fatalf("job rejected log = %v; want reason %q", event, test.reason)
		}
		for _, line := range lines {
			encoded, _ := json.Marshal(line)
			if strings.Contains(string(encoded), "private-path") && line["level"] != "DEBUG" {
				t.Fatalf("log disclosed the submitted URL: %s", encoded)
			}
		}
	}

	f.createJob(t)
	f.createJob(t)
	f.logs.Reset()
	f.do(t, http.MethodPost, "/v1/jobs", f.key, `{"url":"https://vimeo.com/1"}`)
	if event := findLog(t, logLines(t, f.logs), "job rejected"); event["reason"] != "too_many_jobs" {
		t.Fatalf("job rejected log = %v; want too_many_jobs", event)
	}
}

func TestDownloadLogs(t *testing.T) {
	f := newAPIFixture(t)
	created := f.createJob(t)
	f.complete(t, created.ID, "media")
	f.logs.Reset()

	response := f.do(t, http.MethodGet, "/v1/jobs/"+created.ID, f.key, "")
	lines := logLines(t, f.logs)
	if event := findLog(t, lines, "download links issued"); event["job_id"] != created.ID || event["items"] != float64(1) {
		t.Fatalf("download links log = %v", event)
	}
	body := decodeJob(t, response)
	token := body.Items[0].DownloadURL[strings.LastIndex(body.Items[0].DownloadURL, "/")+1:]

	response = f.do(t, http.MethodGet, "/v1/downloads/"+token, "", "")
	raw := f.logs.String()
	lines = logLines(t, f.logs)
	assertTraced(t, response, lines)
	if strings.Contains(raw, token) || strings.Contains(raw, f.dataDir) {
		t.Fatalf("download logs disclosed the token or an internal path: %s", raw)
	}
	event := findLog(t, lines, "download started")
	if event["job_id"] != created.ID || event["item_id"] != body.Items[0].ID || event["key_name"] != "owner" ||
		event["key_id"] != f.ownerID || event["size_bytes"] != float64(5) || event["media_type"] != "video/mp4" {
		t.Fatalf("download started log = %v", event)
	}
	if access := findLog(t, lines, "request completed"); access["route"] != "/v1/downloads/{token}" || access["bytes"] != float64(5) {
		t.Fatalf("download access log = %v", access)
	}

	for path, reason := range map[string]string{
		"/v1/downloads/invalid":                    "malformed_token",
		"/v1/downloads/" + strings.Repeat("A", 43): "unknown_or_expired_token",
	} {
		f.do(t, http.MethodGet, path, "", "")
		if event := findLog(t, logLines(t, f.logs), "download rejected"); event["reason"] != reason {
			t.Fatalf("download rejected log for %s = %v; want %q", path, event, reason)
		}
	}
}

func TestStatusRecorder(t *testing.T) {
	recorder := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	if recorder.statusCode() != http.StatusOK {
		t.Fatalf("statusCode() = %d; want 200 before any write", recorder.statusCode())
	}
	if _, err := recorder.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	recorder.WriteHeader(http.StatusTeapot)
	if recorder.statusCode() != http.StatusOK || recorder.bytes != 3 {
		t.Fatalf("recorder = %d %d; want the implicit 200 and 3 bytes", recorder.statusCode(), recorder.bytes)
	}
}
