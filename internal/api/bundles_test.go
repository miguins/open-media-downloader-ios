package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/auth"
	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/storage"
)

func (f *apiFixture) succeededBundle(t *testing.T) job.Job {
	t.Helper()
	ctx := context.Background()
	j := job.New(f.ownerID, "https://vimeo.com/1", "vimeo", time.Now().UTC(), time.Hour)
	if err := f.store.CreateJob(ctx, j); err != nil {
		t.Fatal(err)
	}
	claimed, err := f.store.ClaimNextJob(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	dir, err := f.layout.PrepareWorkDir(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"video": "abc", "photo": "defg"} {
		if err := os.WriteFile(dir+"/"+name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	items, err := f.layout.Ingest(ctx, j.ID, []storage.Output{{Name: "video", MediaType: "video/mp4"}, {Name: "photo", MediaType: "image/jpeg"}}, 7, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := f.layout.BuildBundle(ctx, j.ID, items, 20, 7, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := claimed.Transition(job.StatusSucceeded, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := f.store.CompleteJob(ctx, claimed, items, &bundle); err != nil {
		t.Fatal(err)
	}
	return claimed
}

func bundleBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestSucceededJobIssuesBundleLink(t *testing.T) {
	f := newAPIFixture(t)
	j := f.succeededBundle(t)
	res := f.do(t, http.MethodGet, "/v1/jobs/"+j.ID, f.key, "")
	if res.Code != 200 {
		t.Fatalf("poll %d", res.Code)
	}
	b, ok := bundleBody(t, res.Body.Bytes())["bundle"].(map[string]any)
	if !ok {
		t.Fatal("missing bundle object")
	}
	if len(b) != 5 || b["media_type"] != "application/zip" || b["size_bytes"].(float64) <= 7 || !strings.HasPrefix(b["download_url"].(string), publicURL+"/v1/bundles/") {
		t.Fatalf("bundle %#v", b)
	}
	first := b["download_url"].(string)
	token := first[strings.LastIndex(first, "/")+1:]
	if len(token) != 43 {
		t.Fatal("invalid token format")
	}
	res = f.do(t, http.MethodGet, "/v1/jobs/"+j.ID, f.key, "")
	second := bundleBody(t, res.Body.Bytes())["bundle"].(map[string]any)
	if first == second["download_url"] {
		t.Fatal("bundle token reused")
	}
	expires, err := time.Parse(time.RFC3339Nano, b["download_expires_at"].(string))
	if err != nil || expires.After(j.ExpiresAt) || expires.After(time.Now().Add(15*time.Minute)) {
		t.Fatalf("expiry %v %v", expires, err)
	}
	assertError(t, f.do(t, http.MethodGet, "/v1/jobs/"+j.ID, f.otherKey, ""), 404, "not_found")
}
func TestJobOmitsBundle(t *testing.T) {
	f := newAPIFixture(t)
	j := job.New(f.ownerID, "https://vimeo.com/1", "vimeo", time.Now().UTC(), time.Hour)
	if err := f.store.CreateJob(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	f.complete(t, j.ID, "abc")
	res := f.do(t, http.MethodGet, "/v1/jobs/"+j.ID, f.key, "")
	if _, ok := bundleBody(t, res.Body.Bytes())["bundle"]; ok {
		t.Fatal("single item bundle exposed")
	}
}

func (f *apiFixture) bundleToken(t *testing.T, j job.Job) string {
	t.Helper()
	res := f.do(t, http.MethodGet, "/v1/jobs/"+j.ID, f.key, "")
	body := bundleBody(t, res.Body.Bytes())
	b := body["bundle"].(map[string]any)
	link := b["download_url"].(string)
	return link[strings.LastIndex(link, "/")+1:]
}
func TestBundleDownloadAndHead(t *testing.T) {
	f := newAPIFixture(t)
	j := f.succeededBundle(t)
	token := f.bundleToken(t, j)
	path := "/v1/bundles/" + token
	res := f.do(t, http.MethodGet, path, "", "")
	if res.Code != 200 {
		t.Fatalf("download %d", res.Code)
	}
	zr, err := zip.NewReader(bytes.NewReader(res.Body.Bytes()), int64(res.Body.Len()))
	if err != nil || len(zr.File) != 2 {
		t.Fatalf("ZIP %v %v", zr, err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		res = f.do(t, method, path, "", "")
		if res.Code != 200 || res.Header().Get("Content-Type") != "application/zip" || res.Header().Get("Cache-Control") != "no-store" || res.Header().Get("Referrer-Policy") != "no-referrer" || res.Header().Get("X-Content-Type-Options") != "nosniff" || res.Header().Get("Accept-Ranges") != "bytes" {
			t.Fatalf("headers %d %#v", res.Code, res.Header())
		}
		if method == http.MethodHead && res.Body.Len() != 0 {
			t.Fatal("HEAD body")
		}
	}
	head := f.do(t, http.MethodHead, path, "", "", "Range", "bytes=999999999-")
	if head.Code != 200 || head.Body.Len() != 0 {
		t.Fatalf("HEAD Range %d %q", head.Code, head.Body.String())
	}
}
func TestBundleDownloadRanges(t *testing.T) {
	f := newAPIFixture(t)
	j := f.succeededBundle(t)
	token := f.bundleToken(t, j)
	path := "/v1/bundles/" + token
	full := f.do(t, http.MethodGet, path, "", "")
	res := f.do(t, http.MethodGet, path, "", "", "Range", "bytes=0-3")
	if res.Code != 206 || !bytes.Equal(res.Body.Bytes(), full.Body.Bytes()[:4]) {
		t.Fatalf("partial %d %q", res.Code, res.Body.Bytes())
	}
	res = f.do(t, http.MethodGet, path, "", "", "Range", fmt.Sprintf("bytes=%d-", full.Body.Len()))
	if res.Code != 416 || res.Header().Get("Content-Range") != fmt.Sprintf("bytes */%d", full.Body.Len()) {
		t.Fatalf("unsatisfiable %d %#v", res.Code, res.Header())
	}
	if res.Header().Get("Cache-Control") != "no-store" || res.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("range error privacy headers %#v", res.Header())
	}
	modified := full.Header().Get("Last-Modified")
	res = f.do(t, http.MethodGet, path, "", "", "If-Modified-Since", modified)
	if res.Code != 304 || res.Body.Len() != 0 {
		t.Fatalf("conditional %d", res.Code)
	}
}
func TestBundleDownloadUniformErrors(t *testing.T) {
	f := newAPIFixture(t)
	j := f.succeededBundle(t)
	token := f.bundleToken(t, j)
	unknown, _ := auth.NewDownloadToken()
	old := token
	f.bundleToken(t, j)
	for _, bad := range []string{"invalid", unknown, old} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			res := f.do(t, method, "/v1/bundles/"+bad, "", "")
			if res.Code != 404 {
				t.Fatalf("bad token %d", res.Code)
			}
			if method == http.MethodHead && res.Body.Len() != 0 {
				t.Fatal("error HEAD body")
			}
		}
	}
	res := f.do(t, http.MethodGet, "/v1/jobs/"+j.ID, f.key, "")
	parsed := bundleBody(t, res.Body.Bytes())
	item := parsed["items"].([]any)[0].(map[string]any)
	link := item["download_url"].(string)
	itemToken := link[strings.LastIndex(link, "/")+1:]
	if r := f.do(t, http.MethodGet, "/v1/bundles/"+itemToken, "", ""); r.Code != 404 {
		t.Fatal("item token authorized bundle")
	}
	token = parsed["bundle"].(map[string]any)["download_url"].(string)
	token = token[strings.LastIndex(token, "/")+1:]
	if r := f.do(t, http.MethodGet, "/v1/downloads/"+token, "", ""); r.Code != 404 {
		t.Fatal("bundle token authorized item")
	}
	if err := f.layout.RemoveJob(j.ID); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		r := f.do(t, method, "/v1/bundles/"+token, "", "")
		if r.Code != 404 || (method == http.MethodHead && r.Body.Len() != 0) {
			t.Fatalf("missing file %d %q", r.Code, r.Body.String())
		}
	}
	if strings.Contains(f.logs.String(), token) || strings.Contains(f.logs.String(), f.dataDir) {
		t.Fatal("bundle logs disclosed secret/path")
	}
}

type deleteOnWrite struct {
	*httptest.ResponseRecorder
	remove func()
	once   bool
}

func (w *deleteOnWrite) Write(p []byte) (int, error) {
	if !w.once {
		w.once = true
		w.remove()
	}
	return w.ResponseRecorder.Write(p)
}
func TestBundleDownloadDeletionBoundary(t *testing.T) {
	f := newAPIFixture(t)
	j := f.succeededBundle(t)
	token := f.bundleToken(t, j)
	path := "/v1/bundles/" + token
	w := &deleteOnWrite{ResponseRecorder: httptest.NewRecorder(), remove: func() {
		if err := f.store.DeleteJob(context.Background(), j.ID); err != nil {
			t.Error(err)
		}
		if err := f.layout.RemoveJob(j.ID); err != nil {
			t.Error(err)
		}
	}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	f.handler.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.Len() == 0 {
		t.Fatalf("opened descriptor stream %d", w.Code)
	}
	assertError(t, f.do(t, http.MethodGet, path, "", ""), 404, "not_found")
}
func TestBundleLinkIssuanceFailure(t *testing.T) {
	f := newAPIFixture(t)
	j := f.succeededBundle(t)
	_ = f.store.Close()
	handler := jobHandler{deps: Dependencies{Store: f.store}}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	if _, err := handler.bundleLink(req, j); err == nil {
		t.Fatal("metadata failure silently omitted bundle")
	}
}

// Keep download parsing tied to the actual bytes, rather than ZIP metadata alone.
func TestBundleContentsMatchItems(t *testing.T) {
	f := newAPIFixture(t)
	j := f.succeededBundle(t)
	token := f.bundleToken(t, j)
	r := f.do(t, http.MethodGet, "/v1/bundles/"+token, "", "")
	zr, err := zip.NewReader(bytes.NewReader(r.Body.Bytes()), int64(r.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"abc", "defg"} {
		reader, err := zr.File[i].Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil || string(data) != want {
			t.Fatalf("bundle entry %d %q %v", i, data, err)
		}
	}
}

func TestItemDownloadUnsatisfiableRange(t *testing.T) {
	f := newAPIFixture(t)
	j := f.createJob(t)
	f.complete(t, j.ID, "abcde")
	poll := f.do(t, http.MethodGet, "/v1/jobs/"+j.ID, f.key, "")
	item := bundleBody(t, poll.Body.Bytes())["items"].([]any)[0].(map[string]any)
	link := item["download_url"].(string)
	token := link[strings.LastIndex(link, "/")+1:]
	r := f.do(t, http.MethodGet, "/v1/downloads/"+token, "", "", "Range", "bytes=5-")
	if r.Code != 416 || r.Header().Get("Content-Range") != "bytes */5" || r.Header().Get("Cache-Control") != "no-store" || r.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("range %d %#v", r.Code, r.Header())
	}
}
