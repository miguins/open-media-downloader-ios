package config

import (
	"strings"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		present   bool
		want      Config
		wantError bool
	}{
		{name: "unset uses default", want: Config{HTTPAddr: ":8080"}},
		{name: "IPv4 address", value: "127.0.0.1:9090", present: true, want: Config{HTTPAddr: "127.0.0.1:9090"}},
		{name: "hostname", value: "localhost:9090", present: true, want: Config{HTTPAddr: "localhost:9090"}},
		{name: "IPv6 address", value: "[::1]:9090", present: true, want: Config{HTTPAddr: "[::1]:9090"}},
		{name: "empty", value: "", present: true, wantError: true},
		{name: "whitespace", value: "  ", present: true, wantError: true},
		{name: "missing port", value: "localhost", present: true, wantError: true},
		{name: "non-numeric port", value: "localhost:secret-value", present: true, wantError: true},
		{name: "zero port", value: ":0", present: true, wantError: true},
		{name: "port too large", value: ":65536", present: true, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(string) (string, bool) { return tt.value, tt.present }
			got, err := Load(lookup)
			if tt.wantError {
				if err == nil || !strings.Contains(err.Error(), "OMDI_HTTP_ADDR") {
					t.Fatalf("Load() error = %v", err)
				}
				if tt.value != "" && strings.Contains(err.Error(), tt.value) {
					t.Fatalf("Load() leaked input in error: %v", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("Load() = %#v, %v; want %#v, nil", got, err, tt.want)
			}
		})
	}
}
