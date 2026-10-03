package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/extractor"
	"github.com/miguins/open-media-downloader-ios/internal/job"
	"github.com/miguins/open-media-downloader-ios/internal/storage"
)

func TestRedditJobMediaContract(t *testing.T) {
	for _, tc := range []struct {
		raw, normalized string
		count           int
	}{
		{"https://www.reddit.com/comments/abc123/", "https://www.reddit.com/comments/abc123/", 1},
		{"https://www.reddit.com/r/example/s/Ab12Cd34Ef/?utm_source=example", "https://www.reddit.com/r/example/s/Ab12Cd34Ef/", 1},
		{"https://www.reddit.com/gallery/def456/", "https://www.reddit.com/gallery/def456/", 2},
	} {
		t.Run(tc.normalized, func(t *testing.T) {
			f := newAPIFixture(t)
			payload, _ := json.Marshal(map[string]string{"url": tc.raw})
			response := f.do(t, "POST", "/v1/jobs", f.key, string(payload))
			if response.Code != 202 {
				t.Fatalf("POST=%d %s", response.Code, response.Body.String())
			}
			created := decodeJob(t, response)
			if created.URL != tc.normalized || created.Platform != "reddit" || created.Status != "queued" {
				t.Fatalf("job=%+v", created)
			}
			stored, err := f.store.Job(t.Context(), f.ownerID, created.ID)
			if err != nil || stored.SourceURL != tc.normalized {
				t.Fatalf("stored=%+v %v", stored, err)
			}
			claimed, err := f.store.ClaimNextJob(t.Context(), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			work, err := f.layout.PrepareWorkDir(claimed.ID)
			if err != nil {
				t.Fatal(err)
			}
			files, err := (extractor.Fake{}).Extract(t.Context(), extractor.Request{URL: stored.SourceURL, Platform: "reddit", WorkDir: work, MaxBytes: 1 << 20, MaxItems: 2})
			if err != nil {
				t.Fatal(err)
			}
			outputs := make([]storage.Output, len(files))
			for i, file := range files {
				outputs[i] = storage.Output{Name: file.Name, MediaType: file.MediaType}
			}
			items, err := f.layout.Ingest(t.Context(), claimed.ID, outputs, 1<<20, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			var bundle *job.Bundle
			if len(items) > 1 {
				built, err := f.layout.BuildBundle(t.Context(), claimed.ID, items, 2, 1<<20, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				bundle = &built
			}
			if err := claimed.Transition(job.StatusSucceeded, "", time.Now()); err != nil {
				t.Fatal(err)
			}
			if err := f.store.CompleteJob(t.Context(), claimed, items, bundle); err != nil {
				t.Fatal(err)
			}
			response = f.do(t, "GET", "/v1/jobs/"+created.ID, f.key, "")
			var polled struct {
				jobBody
				Bundle *struct {
					DownloadURL string `json:"download_url"`
				} `json:"bundle"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &polled); err != nil {
				t.Fatal(err)
			}
			if polled.URL != tc.normalized || len(polled.Items) != tc.count || polled.Status != "succeeded" {
				t.Fatalf("poll=%+v", polled)
			}
			var media [][]byte
			for _, item := range polled.Items {
				if item.MediaType != "image/jpeg" {
					t.Fatalf("type=%s", item.MediaType)
				}
				download := f.do(t, "GET", strings.TrimPrefix(item.DownloadURL, publicURL), "", "")
				if download.Code != 200 || download.Header().Get("Content-Type") != "image/jpeg" || download.Body.Len() == 0 || download.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("image download contract failed")
				}
				media = append(media, download.Body.Bytes())
			}
			if tc.count == 1 && polled.Bundle != nil {
				t.Fatal("unexpected single-image bundle")
			}
			if tc.count == 2 {
				if polled.Bundle == nil {
					t.Fatal("missing gallery bundle")
				}
				download := f.do(t, "GET", strings.TrimPrefix(polled.Bundle.DownloadURL, publicURL), "", "")
				if download.Code != 200 || download.Header().Get("Content-Type") != "application/zip" {
					t.Fatal("bundle download failed")
				}
				z, err := zip.NewReader(bytes.NewReader(download.Body.Bytes()), int64(download.Body.Len()))
				if err != nil {
					t.Fatal(err)
				}
				if len(z.File) != 2 {
					t.Fatal("incomplete bundle")
				}
				for i, entry := range z.File {
					reader, err := entry.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(reader)
					_ = reader.Close()
					if err != nil || !bytes.Equal(data, media[i]) || entry.Name != polled.Items[i].FileName {
						t.Fatal("bundle order or bytes differ")
					}
				}
			}
			assertError(t, f.do(t, "GET", "/v1/jobs/"+created.ID, f.otherKey, ""), 404, "not_found")
			if strings.Contains(f.logs.String(), "reddit.com") || strings.Contains(f.logs.String(), "Ab12Cd34Ef") {
				t.Fatal("source URL leaked in logs")
			}
		})
	}
}

func TestRedditInvalidShareContract(t *testing.T) {
	f := newAPIFixture(t)
	assertError(t, f.do(t, http.MethodPost, "/v1/jobs", f.key, `{"url":"https://www.reddit.com/r/example/s/short"}`), 422, "unsupported_url")
}
