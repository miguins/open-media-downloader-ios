package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/auth"
	"github.com/miguins/open-media-downloader-ios/internal/id"
	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/logging"
	"github.com/miguins/open-media-downloader-ios/internal/readiness"
	"github.com/miguins/open-media-downloader-ios/internal/storage"
	"github.com/miguins/open-media-downloader-ios/internal/store"
	"github.com/miguins/open-media-downloader-ios/internal/urlpolicy"
)

const publicURL = "https://omdi.example.com/base"

type apiFixture struct {
	handler  http.Handler
	store    *store.Store
	layout   *storage.Layout
	dataDir  string
	key      string
	ownerID  string
	otherKey string
	notified int
	logs     *bytes.Buffer
}

func newAPIFixture(t *testing.T) *apiFixture {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	st, err := store.Open(ctx, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	layout, err := storage.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := urlpolicy.New(urlpolicy.PlatformIDs(), 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &apiFixture{store: st, layout: layout, dataDir: dataDir, logs: &bytes.Buffer{}}
	for _, name := range []string{"owner", "other"} {
		plaintext, record := auth.Generate(name, time.Now().UTC())
		if err := st.CreateAPIKey(ctx, record); err != nil {
			t.Fatal(err)
		}
		if name == "owner" {
			f.key, f.ownerID = plaintext, record.ID
		} else {
			f.otherKey = plaintext
		}
	}
	logger := logging.New(f.logs, slog.LevelDebug)
	f.handler = NewRouter(Dependencies{
		Readiness:     readiness.New(),
		Authenticator: auth.NewAuthenticator(st, logger),
		Store:         st,
		Layout:        layout,
		Policy:        policy,
		Notify:        func() { f.notified++ },
		Settings: Settings{
			PublicURL:       publicURL,
			MaxRequestBytes: 1024,
			MaxQueuedJobs:   2,
			JobRetention:    time.Hour,
			TokenTTL:        15 * time.Minute,
		},
		Logger: logger,
	})

	return f
}

func (f *apiFixture) do(t *testing.T, method, path, key, body string, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequestWithContext(context.Background(), method, path, reader)
	if key != "" {
		request.Header.Set("Authorization", "Bearer "+key)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		request.Header.Set(headers[i], headers[i+1])
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)

	return response
}

type jobBody struct {
	ID        string     `json:"id"`
	Status    string     `json:"status"`
	URL       string     `json:"url"`
	Platform  string     `json:"platform"`
	Error     *string    `json:"error"`
	Detail    *string    `json:"error_detail"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	ExpiresAt time.Time  `json:"expires_at"`
	Items     []itemBody `json:"items"`
}

type itemBody struct {
	ID                string    `json:"id"`
	FileName          string    `json:"file_name"`
	MediaType         string    `json:"media_type"`
	SizeBytes         int64     `json:"size_bytes"`
	DownloadURL       string    `json:"download_url"`
	DownloadExpiresAt time.Time `json:"download_expires_at"`
}

func decodeJob(t *testing.T, response *httptest.ResponseRecorder) jobBody {
	t.Helper()
	var body jobBody
	decoder := json.NewDecoder(response.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		t.Fatalf("decode job: %v", err)
	}

	return body
}

func assertError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status || response.Body.String() != `{"error":"`+code+`"}`+"\n" {
		t.Fatalf("response = %d %q; want %d %s", response.Code, response.Body.String(), status, code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func (f *apiFixture) createJob(t *testing.T) jobBody {
	t.Helper()
	response := f.do(t, http.MethodPost, "/v1/jobs", f.key, `{"url":"https://VIMEO.com/123#frag"}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("create status = %d %s", response.Code, response.Body.String())
	}

	return decodeJob(t, response)
}

func TestCreateJob(t *testing.T) {
	f := newAPIFixture(t)
	response := f.do(t, http.MethodPost, "/v1/jobs", f.key, `{"url":"https://VIMEO.com/123#frag"}`)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	body := decodeJob(t, response)
	if !id.Valid(body.ID) || body.Status != "queued" || body.URL != "https://vimeo.com/123" || body.Platform != "vimeo" ||
		body.Error != nil || body.Detail != nil || body.Items != nil || body.ExpiresAt.Sub(body.CreatedAt) != time.Hour {
		t.Fatalf("body = %#v", body)
	}
	if got := response.Header().Get("Location"); got != "/v1/jobs/"+body.ID {
		t.Fatalf("Location = %q", got)
	}
	if f.notified != 1 {
		t.Fatalf("worker notified %d times", f.notified)
	}
	stored, err := f.store.Job(context.Background(), f.ownerID, body.ID)
	if err != nil || stored.Status != job.StatusQueued {
		t.Fatalf("stored job = %#v, %v", stored, err)
	}
}

func TestCreateJobRejectsInvalidRequests(t *testing.T) {
	f := newAPIFixture(t)
	tests := []struct {
		name    string
		key     string
		body    string
		headers []string
		status  int
		code    string
	}{
		{name: "no key", body: `{"url":"https://vimeo.com/1"}`, status: 401, code: "unauthorized"},
		{name: "wrong content type", key: f.key, body: `{"url":"https://vimeo.com/1"}`, headers: []string{"Content-Type", "text/plain"}, status: 415, code: "unsupported_media_type"},
		{name: "malformed content type", key: f.key, body: `{"url":"https://vimeo.com/1"}`, headers: []string{"Content-Type", "application/json; ="}, status: 415, code: "unsupported_media_type"},
		{name: "malformed JSON", key: f.key, body: `{"url":`, status: 400, code: "invalid_request"},
		{name: "unknown field", key: f.key, body: `{"url":"https://vimeo.com/1","extra":1}`, status: 400, code: "invalid_request"},
		{name: "missing url", key: f.key, body: `{}`, status: 400, code: "invalid_request"},
		{name: "not an object", key: f.key, body: `["https://vimeo.com/1"]`, status: 400, code: "invalid_request"},
		{name: "trailing data", key: f.key, body: `{"url":"https://vimeo.com/1"} {}`, status: 400, code: "invalid_request"},
		{name: "too large", key: f.key, body: `{"url":"https://vimeo.com/` + strings.Repeat("a", 2048) + `"}`, status: 413, code: "request_too_large"},
		{name: "unsupported url", key: f.key, body: `{"url":"http://127.0.0.1/admin"}`, status: 422, code: "unsupported_url"},
		{name: "collection url", key: f.key, body: `{"url":"https://vimeo.com/channels/staffpicks"}`, status: 422, code: "unsupported_url"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := f.do(t, http.MethodPost, "/v1/jobs", tt.key, tt.body, tt.headers...)
			assertError(t, response, tt.status, tt.code)
		})
	}
	if f.notified != 0 {
		t.Fatal("rejected requests notified the worker")
	}
	jobs, err := f.store.Jobs(context.Background(), store.JobFilter{OwnerID: f.ownerID})
	if err != nil || len(jobs) != 0 {
		t.Fatalf("rejected requests persisted jobs: %d, %v", len(jobs), err)
	}
}

func TestCreateJobEnforcesQueueLimit(t *testing.T) {
	f := newAPIFixture(t)
	f.createJob(t)
	f.createJob(t)
	response := f.do(t, http.MethodPost, "/v1/jobs", f.key, `{"url":"https://vimeo.com/1"}`)
	assertError(t, response, http.StatusTooManyRequests, "too_many_jobs")
	if response := f.do(t, http.MethodPost, "/v1/jobs", f.otherKey, `{"url":"https://vimeo.com/1"}`); response.Code != http.StatusAccepted {
		t.Fatalf("other key status = %d; limits must be per key", response.Code)
	}
}

func TestGetJob(t *testing.T) {
	f := newAPIFixture(t)
	created := f.createJob(t)

	response := f.do(t, http.MethodGet, "/v1/jobs/"+created.ID, f.key, "")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if body := decodeJob(t, response); body.ID != created.ID || body.Status != "queued" || body.Items != nil {
		t.Fatalf("body = %#v", body)
	}
	for name, request := range map[string][2]string{
		"other owner": {created.ID, f.otherKey},
		"unknown":     {id.New(), f.key},
		"invalid id":  {"not-an-id", f.key},
	} {
		t.Run(name, func(t *testing.T) {
			assertError(t, f.do(t, http.MethodGet, "/v1/jobs/"+request[0], request[1], ""), http.StatusNotFound, "not_found")
		})
	}
	assertError(t, f.do(t, http.MethodGet, "/v1/jobs/"+created.ID, "", ""), http.StatusUnauthorized, "unauthorized")
}

// complete runs the job through storage and the store as the worker would.
func (f *apiFixture) complete(t *testing.T, jobID string, contents string) {
	t.Helper()
	ctx := context.Background()
	claimed, err := f.store.ClaimNextJob(ctx, time.Now().UTC())
	if err != nil || claimed.ID != jobID {
		t.Fatalf("ClaimNextJob() = %#v, %v", claimed, err)
	}
	workDir, err := f.layout.PrepareWorkDir(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "out"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := f.layout.Ingest(context.Background(), jobID, []storage.Output{{Name: "out", MediaType: "video/mp4"}}, 1<<20, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if err := claimed.Transition(job.StatusSucceeded, "", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.CompleteJob(ctx, claimed, items, nil); err != nil {
		t.Fatal(err)
	}
}

func TestSucceededJobIssuesDownloadLinks(t *testing.T) {
	f := newAPIFixture(t)
	created := f.createJob(t)
	f.complete(t, created.ID, "media-bytes")

	first := decodeJob(t, f.do(t, http.MethodGet, "/v1/jobs/"+created.ID, f.key, ""))
	if first.Status != "succeeded" || len(first.Items) != 1 {
		t.Fatalf("body = %#v", first)
	}
	item := first.Items[0]
	if !strings.HasPrefix(item.DownloadURL, publicURL+"/v1/downloads/") || item.MediaType != "video/mp4" ||
		item.SizeBytes != 11 || item.FileName != "omdi-"+created.ID[:8]+"-1.mp4" {
		t.Fatalf("item = %#v", item)
	}
	if item.DownloadExpiresAt.After(first.ExpiresAt) || time.Until(item.DownloadExpiresAt) > 15*time.Minute {
		t.Fatalf("download expiry = %v; job expiry = %v", item.DownloadExpiresAt, first.ExpiresAt)
	}

	second := decodeJob(t, f.do(t, http.MethodGet, "/v1/jobs/"+created.ID, f.key, ""))
	if second.Items[0].DownloadURL == item.DownloadURL {
		t.Fatal("poll reused a download token")
	}
	oldPath := strings.TrimPrefix(item.DownloadURL, publicURL)
	newPath := strings.TrimPrefix(second.Items[0].DownloadURL, publicURL)
	assertError(t, f.do(t, http.MethodGet, oldPath, "", ""), http.StatusNotFound, "not_found")
	if response := f.do(t, http.MethodGet, newPath, "", ""); response.Code != http.StatusOK || response.Body.String() != "media-bytes" {
		t.Fatalf("download = %d %q", response.Code, response.Body.String())
	}
}

func TestCancelJob(t *testing.T) {
	f := newAPIFixture(t)
	queued := f.createJob(t)

	response := f.do(t, http.MethodDelete, "/v1/jobs/"+queued.ID, f.key, "")
	if response.Code != http.StatusOK || decodeJob(t, response).Status != "canceled" {
		t.Fatalf("cancel = %d", response.Code)
	}
	response = f.do(t, http.MethodDelete, "/v1/jobs/"+queued.ID, f.key, "")
	if response.Code != http.StatusOK || decodeJob(t, response).Status != "canceled" {
		t.Fatalf("repeated cancel = %d", response.Code)
	}

	running := f.createJob(t)
	if _, err := f.store.ClaimNextJob(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	response = f.do(t, http.MethodDelete, "/v1/jobs/"+running.ID, f.key, "")
	if response.Code != http.StatusOK || decodeJob(t, response).Status != "canceled" {
		t.Fatalf("cancel running = %d", response.Code)
	}

	done := f.createJob(t)
	f.complete(t, done.ID, "x")
	assertError(t, f.do(t, http.MethodDelete, "/v1/jobs/"+done.ID, f.key, ""), http.StatusConflict, "conflict")
	assertError(t, f.do(t, http.MethodDelete, "/v1/jobs/"+done.ID, f.otherKey, ""), http.StatusNotFound, "not_found")
	assertError(t, f.do(t, http.MethodDelete, "/v1/jobs/"+id.New(), f.key, ""), http.StatusNotFound, "not_found")
}

func TestDownload(t *testing.T) {
	f := newAPIFixture(t)
	created := f.createJob(t)
	f.complete(t, created.ID, "0123456789")
	body := decodeJob(t, f.do(t, http.MethodGet, "/v1/jobs/"+created.ID, f.key, ""))
	path := strings.TrimPrefix(body.Items[0].DownloadURL, publicURL)

	response := f.do(t, http.MethodGet, path, "", "")
	if response.Code != http.StatusOK || response.Body.String() != "0123456789" {
		t.Fatalf("download = %d %q", response.Code, response.Body.String())
	}
	for header, want := range map[string]string{
		"Content-Type":           "video/mp4",
		"Content-Length":         "10",
		"Content-Disposition":    `attachment; filename=omdi-` + created.ID[:8] + `-1.mp4`,
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "no-store",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := response.Header().Get(header); got != want {
			t.Fatalf("%s = %q; want %q", header, got, want)
		}
	}

	if response := f.do(t, http.MethodHead, path, "", ""); response.Code != http.StatusOK || response.Body.Len() != 0 {
		t.Fatalf("HEAD = %d, %d bytes", response.Code, response.Body.Len())
	}
	if response := f.do(t, http.MethodGet, path, "", "", "Range", "bytes=2-4"); response.Code != http.StatusPartialContent || response.Body.String() != "234" {
		t.Fatalf("range = %d %q", response.Code, response.Body.String())
	}
	if response := f.do(t, http.MethodGet, path, f.key, ""); response.Code != http.StatusOK {
		t.Fatalf("download with an API key = %d", response.Code)
	}
}

func TestDownloadRejectsInvalidTokens(t *testing.T) {
	f := newAPIFixture(t)
	created := f.createJob(t)
	f.complete(t, created.ID, "x")
	items, err := f.store.Items(context.Background(), f.ownerID, created.ID)
	if err != nil {
		t.Fatal(err)
	}

	expired, expiredHash := auth.NewDownloadToken()
	if err := f.store.ReplaceDownloadToken(context.Background(), job.DownloadToken{
		Hash: expiredHash, ItemID: items[0].ID, OwnerID: f.ownerID, CreatedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	unknown, _ := auth.NewDownloadToken()
	for name, token := range map[string]string{"malformed": "abc", "unknown": unknown, "expired": expired} {
		t.Run(name, func(t *testing.T) {
			assertError(t, f.do(t, http.MethodGet, "/v1/downloads/"+token, "", ""), http.StatusNotFound, "not_found")
		})
	}

	body := decodeJob(t, f.do(t, http.MethodGet, "/v1/jobs/"+created.ID, f.key, ""))
	path := strings.TrimPrefix(body.Items[0].DownloadURL, publicURL)
	if err := f.layout.RemoveJob(created.ID); err != nil {
		t.Fatal(err)
	}
	assertError(t, f.do(t, http.MethodGet, path, "", ""), http.StatusNotFound, "not_found")
}

type deadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (d *deadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	d.deadlines = append(d.deadlines, deadline)

	return nil
}

func TestDeadlineWriterExtendsEachWrite(t *testing.T) {
	recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	writer := newDeadlineWriter(recorder, time.Minute)
	for range 3 {
		if _, err := writer.Write([]byte("chunk")); err != nil {
			t.Fatal(err)
		}
	}
	if len(recorder.deadlines) != 4 {
		t.Fatalf("deadlines set %d times; want initial plus one per write", len(recorder.deadlines))
	}
	for _, deadline := range recorder.deadlines {
		if time.Until(deadline) < 50*time.Second {
			t.Fatalf("deadline %v is not extended", deadline)
		}
	}
	if writer.Unwrap() != recorder {
		t.Fatal("Unwrap() does not return the wrapped writer")
	}
}

func TestHandlersReportStoreFailures(t *testing.T) {
	f := newAPIFixture(t)
	created := f.createJob(t)
	logs := &strings.Builder{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	keyID, secret, _ := auth.Parse(f.key)
	authenticator := auth.NewAuthenticator(hashOnly{keyID: keyID, secret: secret}, logger)
	policy, _ := urlpolicy.New(urlpolicy.PlatformIDs(), 2048)
	f.handler = NewRouter(Dependencies{
		Readiness: readiness.New(), Authenticator: authenticator, Store: f.store, Layout: f.layout, Policy: policy,
		Notify: func() {}, Settings: Settings{PublicURL: publicURL, MaxRequestBytes: 1024, MaxQueuedJobs: 2, JobRetention: time.Hour, TokenTTL: time.Minute},
		Logger: logger,
	})
	_ = f.store.Close()

	assertError(t, f.do(t, http.MethodPost, "/v1/jobs", f.key, `{"url":"https://vimeo.com/1"}`), http.StatusInternalServerError, "internal")
	assertError(t, f.do(t, http.MethodGet, "/v1/jobs/"+created.ID, f.key, ""), http.StatusInternalServerError, "internal")
	assertError(t, f.do(t, http.MethodDelete, "/v1/jobs/"+created.ID, f.key, ""), http.StatusInternalServerError, "internal")
	if strings.Contains(logs.String(), "vimeo.com") || strings.Contains(logs.String(), f.dataDir) {
		t.Fatalf("logs leaked details: %s", logs.String())
	}
}

// hashOnly serves a single key record from memory.
type hashOnly struct {
	keyID  string
	secret []byte
}

func (h hashOnly) APIKey(_ context.Context, keyID string) (store.APIKey, error) {
	if keyID != h.keyID {
		return store.APIKey{}, store.ErrNotFound
	}
	sum := sha256.Sum256(h.secret)

	return store.APIKey{ID: keyID, SecretHash: sum[:]}, nil
}

func (h hashOnly) TouchAPIKey(context.Context, string, time.Time) error { return nil }

func TestFailedJobReportsErrorCode(t *testing.T) {
	f := newAPIFixture(t)
	created := f.createJob(t)
	claimed, err := f.store.ClaimNextJob(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	failed := claimed
	if err := failed.Transition(job.StatusFailed, job.ErrorTimeout, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.UpdateJobStatus(context.Background(), failed, job.StatusRunning); err != nil {
		t.Fatal(err)
	}
	body := decodeJob(t, f.do(t, http.MethodGet, "/v1/jobs/"+created.ID, f.key, ""))
	if body.Status != "failed" || body.Error == nil || *body.Error != "timeout" || body.Detail != nil || body.Items != nil {
		t.Fatalf("body = %#v", body)
	}
	assertError(t, f.do(t, http.MethodDelete, "/v1/jobs/"+created.ID, f.key, ""), http.StatusConflict, "conflict")
}

func TestFailedJobReportsErrorDetail(t *testing.T) {
	f := newAPIFixture(t)
	created := f.createJob(t)
	claimed, err := f.store.ClaimNextJob(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	failed := claimed
	if err := failed.Transition(job.StatusFailed, job.ErrorExtractionFailed, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	failed.ErrorDetail = job.DetailRateLimited
	if err := f.store.UpdateJobStatus(context.Background(), failed, job.StatusRunning); err != nil {
		t.Fatal(err)
	}
	body := decodeJob(t, f.do(t, http.MethodGet, "/v1/jobs/"+created.ID, f.key, ""))
	if body.Error == nil || *body.Error != "extraction_failed" || body.Detail == nil || *body.Detail != "rate_limited" {
		t.Fatalf("body = %#v; want extraction_failed with rate_limited", body)
	}
}
