package config

import (
	"log/slog"
	"strings"
	"testing"
)

func TestLoadLogLevel(t *testing.T) {
	for value, want := range map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	} {
		got, err := Load(lookupFrom(map[string]string{"OMDI_LOG_LEVEL": value}))
		if err != nil {
			t.Fatalf("Load(%q) error = %v", value, err)
		}
		if got.LogLevel != want {
			t.Fatalf("Load(%q).LogLevel = %v; want %v", value, got.LogLevel, want)
		}
	}
}

func TestLoadRejectsInvalidLogLevel(t *testing.T) {
	for _, value := range []string{"", "INFO", "trace", "warn+2", " info"} {
		_, err := Load(lookupFrom(map[string]string{"OMDI_LOG_LEVEL": value}))
		if err == nil || !strings.Contains(err.Error(), "OMDI_LOG_LEVEL") {
			t.Fatalf("Load(%q) error = %v; want an OMDI_LOG_LEVEL error", value, err)
		}
	}
}
