package config

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func lookupFrom(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, present := values[name]

		return value, present
	}
}

func defaultConfig() Config {
	return Config{
		HTTPAddr:         ":8080",
		DataDir:          "/data",
		AllowedPlatforms: []string{"youtube", "instagram", "tiktok", "x", "reddit", "vimeo"},
		MaxURLLength:     2048,
		MaxRequestBytes:  16384,
		JobTimeout:       10 * time.Minute,
		MaxJobBytes:      2 << 30,
		MinFreeBytes:     1 << 30,
		JobRetention:     24 * time.Hour,
		TokenTTL:         15 * time.Minute,
		PublicURL:        "http://localhost:8080",
		MaxQueuedJobs:    10,
		MaxJobItems:      20,
		Tools: Tools{
			YTDLP:     "/opt/media-tools/bin/yt-dlp",
			GalleryDL: "/opt/media-tools/bin/gallery-dl",
			FFmpeg:    "/opt/ffmpeg/bin/ffmpeg",
			FFprobe:   "/opt/ffmpeg/bin/ffprobe",
		},
	}
}

func TestLoadDefaults(t *testing.T) {
	got, err := Load(lookupFrom(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if want := defaultConfig(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %#v; want %#v", got, want)
	}
}

func TestLoadValidOverrides(t *testing.T) {
	got, err := Load(lookupFrom(map[string]string{
		"OMDI_HTTP_ADDR":         "[::1]:9090",
		"OMDI_DATA_DIR":          "/srv/omdi",
		"OMDI_ALLOWED_PLATFORMS": "vimeo,youtube",
		"OMDI_MAX_URL_LENGTH":    "256",
		"OMDI_MAX_REQUEST_BYTES": "1048576",
		"OMDI_JOB_TIMEOUT":       "30s",
		"OMDI_MAX_JOB_BYTES":     "1048576",
		"OMDI_MIN_FREE_BYTES":    "0",
		"OMDI_JOB_RETENTION":     "5m",
		"OMDI_TOKEN_TTL":         "5m",
		"OMDI_YTDLP_PATH":        "/usr/bin/yt-dlp",
		"OMDI_GALLERYDL_PATH":    "/usr/bin/gallery-dl",
		"OMDI_FFMPEG_PATH":       "/usr/bin/ffmpeg",
		"OMDI_FFPROBE_PATH":      "/usr/bin/ffprobe",
		"OMDI_PUBLIC_URL":        "https://omdi.example.com/base/",
		"OMDI_MAX_QUEUED_JOBS":   "1000",
		"OMDI_MAX_JOB_ITEMS":     "100",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	want := Config{
		HTTPAddr:         "[::1]:9090",
		DataDir:          "/srv/omdi",
		AllowedPlatforms: []string{"vimeo", "youtube"},
		MaxURLLength:     256,
		MaxRequestBytes:  1048576,
		JobTimeout:       30 * time.Second,
		MaxJobBytes:      1048576,
		MinFreeBytes:     0,
		JobRetention:     5 * time.Minute,
		TokenTTL:         5 * time.Minute,
		PublicURL:        "https://omdi.example.com/base",
		MaxQueuedJobs:    1000,
		MaxJobItems:      100,
		Tools: Tools{
			YTDLP:     "/usr/bin/yt-dlp",
			GalleryDL: "/usr/bin/gallery-dl",
			FFmpeg:    "/usr/bin/ffmpeg",
			FFprobe:   "/usr/bin/ffprobe",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %#v; want %#v", got, want)
	}
}

func TestLoadAcceptsHTTPAddrForms(t *testing.T) {
	for _, value := range []string{"127.0.0.1:9090", "localhost:9090", "[::1]:9090", ":65535"} {
		got, err := Load(lookupFrom(map[string]string{"OMDI_HTTP_ADDR": value}))
		if err != nil || got.HTTPAddr != value {
			t.Fatalf("Load(%q) = %q, %v", value, got.HTTPAddr, err)
		}
	}
}

func TestLoadValidMaxJobItems(t *testing.T) {
	got, err := Load(lookupFrom(map[string]string{"OMDI_MAX_JOB_ITEMS": "100"}))
	if err != nil || got.MaxJobItems != 100 {
		t.Fatalf("Load() MaxJobItems = %d, %v; want 100", got.MaxJobItems, err)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "OMDI_HTTP_ADDR", value: ""},
		{name: "OMDI_HTTP_ADDR", value: "  "},
		{name: "OMDI_HTTP_ADDR", value: "localhost"},
		{name: "OMDI_HTTP_ADDR", value: "localhost:secret-value"},
		{name: "OMDI_HTTP_ADDR", value: ":0"},
		{name: "OMDI_HTTP_ADDR", value: ":65536"},
		{name: "OMDI_DATA_DIR", value: ""},
		{name: "OMDI_DATA_DIR", value: "relative/secret-dir"},
		{name: "OMDI_DATA_DIR", value: "/data/../secret-dir"},
		{name: "OMDI_DATA_DIR", value: "/data/"},
		{name: "OMDI_DATA_DIR", value: " /data"},
		{name: "OMDI_ALLOWED_PLATFORMS", value: ""},
		{name: "OMDI_ALLOWED_PLATFORMS", value: "youtube,secret-platform"},
		{name: "OMDI_ALLOWED_PLATFORMS", value: "youtube,,vimeo"},
		{name: "OMDI_ALLOWED_PLATFORMS", value: "youtube, vimeo"},
		{name: "OMDI_ALLOWED_PLATFORMS", value: "youtube,youtube"},
		{name: "OMDI_ALLOWED_PLATFORMS", value: "YouTube"},
		{name: "OMDI_MAX_URL_LENGTH", value: "255"},
		{name: "OMDI_MAX_URL_LENGTH", value: "8193"},
		{name: "OMDI_MAX_URL_LENGTH", value: "secret-number"},
		{name: "OMDI_MAX_URL_LENGTH", value: "+300"},
		{name: "OMDI_MAX_REQUEST_BYTES", value: "1023"},
		{name: "OMDI_MAX_REQUEST_BYTES", value: "1048577"},
		{name: "OMDI_JOB_TIMEOUT", value: "29s"},
		{name: "OMDI_JOB_TIMEOUT", value: "2h1s"},
		{name: "OMDI_JOB_TIMEOUT", value: "10"},
		{name: "OMDI_MAX_JOB_BYTES", value: "1048575"},
		{name: "OMDI_MAX_JOB_BYTES", value: "107374182401"},
		{name: "OMDI_MAX_JOB_BYTES", value: "99999999999999999999999"},
		{name: "OMDI_MIN_FREE_BYTES", value: "-1"},
		{name: "OMDI_MIN_FREE_BYTES", value: "1099511627777"},
		{name: "OMDI_JOB_RETENTION", value: "4m59s"},
		{name: "OMDI_JOB_RETENTION", value: "721h"},
		{name: "OMDI_TOKEN_TTL", value: "59s"},
		{name: "OMDI_TOKEN_TTL", value: "25h"},
		{name: "OMDI_YTDLP_PATH", value: "yt-dlp"},
		{name: "OMDI_GALLERYDL_PATH", value: ""},
		{name: "OMDI_FFMPEG_PATH", value: "/opt//ffmpeg"},
		{name: "OMDI_FFPROBE_PATH", value: "/opt/ffprobe/"},
		{name: "OMDI_PUBLIC_URL", value: ""},
		{name: "OMDI_PUBLIC_URL", value: "omdi.example.com"},
		{name: "OMDI_PUBLIC_URL", value: "/relative"},
		{name: "OMDI_PUBLIC_URL", value: "ftp://omdi.example.com"},
		{name: "OMDI_PUBLIC_URL", value: "https://"},
		{name: "OMDI_PUBLIC_URL", value: "https://user@omdi.example.com"},
		{name: "OMDI_PUBLIC_URL", value: "https://omdi.example.com/?secret=1"},
		{name: "OMDI_PUBLIC_URL", value: "https://omdi.example.com/#secret"},
		{name: "OMDI_PUBLIC_URL", value: "https://omdi.example.com/%zz"},
		{name: "OMDI_MAX_QUEUED_JOBS", value: "-5"},
		{name: "OMDI_MAX_QUEUED_JOBS", value: "1001"},
		{name: "OMDI_MAX_JOB_ITEMS", value: "0"},
		{name: "OMDI_MAX_JOB_ITEMS", value: "101"},
		{name: "OMDI_MAX_JOB_ITEMS", value: "+20"},
		{name: "OMDI_MAX_JOB_ITEMS", value: "secret-number"},
	}

	for _, tt := range tests {
		t.Run(tt.name+"="+tt.value, func(t *testing.T) {
			_, err := Load(lookupFrom(map[string]string{tt.name: tt.value}))
			if err == nil || !strings.Contains(err.Error(), tt.name) {
				t.Fatalf("Load() error = %v; want error naming %s", err, tt.name)
			}
			// The lower-bound value appears in the documented upper bound, 100.
			if tt.name == "OMDI_MAX_JOB_ITEMS" && tt.value == "0" {
				return
			}
			if strings.TrimSpace(tt.value) != "" && strings.Contains(err.Error(), strings.TrimSpace(tt.value)) {
				t.Fatalf("Load() leaked input in error: %v", err)
			}
		})
	}
}

func TestLoadRejectsTokenTTLAboveRetention(t *testing.T) {
	_, err := Load(lookupFrom(map[string]string{
		"OMDI_JOB_RETENTION": "10m",
		"OMDI_TOKEN_TTL":     "11m",
	}))
	if err == nil || !strings.Contains(err.Error(), "OMDI_TOKEN_TTL must not exceed OMDI_JOB_RETENTION") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsRetentionNotAboveTimeout(t *testing.T) {
	_, err := Load(lookupFrom(map[string]string{
		"OMDI_JOB_TIMEOUT":   "10m",
		"OMDI_JOB_RETENTION": "10m",
		"OMDI_TOKEN_TTL":     "5m",
	}))
	if err == nil || !strings.Contains(err.Error(), "OMDI_JOB_RETENTION must exceed OMDI_JOB_TIMEOUT") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadReportsAllErrors(t *testing.T) {
	_, err := Load(lookupFrom(map[string]string{
		"OMDI_HTTP_ADDR":      "",
		"OMDI_DATA_DIR":       "relative",
		"OMDI_MAX_URL_LENGTH": "1",
	}))
	if err == nil {
		t.Fatal("Load() error = nil")
	}
	for _, name := range []string{"OMDI_HTTP_ADDR", "OMDI_DATA_DIR", "OMDI_MAX_URL_LENGTH"} {
		if !strings.Contains(err.Error(), name) {
			t.Fatalf("Load() error = %v; missing %s", err, name)
		}
	}
}
