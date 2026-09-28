package config

import (
	"strings"
	"testing"
	"time"

	"github.com/miguins/open-media-downloader-ios/internal/auth"
)

func TestLoadRejectsInvalidKeyRestoration(t *testing.T) {
	for _, tt := range []struct {
		name     string
		env      map[string]string
		variable string
	}{
		{"empty toggle", map[string]string{"OMDI_RESTORE_API_KEY_ON_STARTUP": ""}, "OMDI_RESTORE_API_KEY_ON_STARTUP"},
		{"invalid toggle", map[string]string{"OMDI_RESTORE_API_KEY_ON_STARTUP": "secret-value"}, "OMDI_RESTORE_API_KEY_ON_STARTUP"},
		{"spaced toggle", map[string]string{"OMDI_RESTORE_API_KEY_ON_STARTUP": " true"}, "OMDI_RESTORE_API_KEY_ON_STARTUP"},
		{"missing key", map[string]string{"OMDI_RESTORE_API_KEY_ON_STARTUP": "true"}, "OMDI_API_KEY"},
		{"empty key", map[string]string{"OMDI_RESTORE_API_KEY_ON_STARTUP": "true", "OMDI_API_KEY": ""}, "OMDI_API_KEY"},
		{"invalid key", map[string]string{"OMDI_RESTORE_API_KEY_ON_STARTUP": "true", "OMDI_API_KEY": "secret-value"}, "OMDI_API_KEY"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(lookupFrom(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.variable) {
				t.Fatalf("expected configuration error naming %s, got %v", tt.variable, err)
			}
			if strings.Contains(err.Error(), "secret-value") {
				t.Fatal("configuration error disclosed a value")
			}
		})
	}
}

func TestLoadKeyRestoration(t *testing.T) {
	plaintext, _ := auth.Generate("phone", time.Now().UTC())
	got, err := Load(lookupFrom(map[string]string{
		"OMDI_RESTORE_API_KEY_ON_STARTUP": "true",
		"OMDI_API_KEY":                    plaintext,
	}))
	if err != nil || !got.RestoreAPIKeyOnStartup || got.APIKey != plaintext {
		t.Fatalf("valid restoration configuration rejected: %v", err)
	}
	for _, toggle := range []string{"false", ""} {
		values := map[string]string{"OMDI_API_KEY": "ignored-invalid-key"}
		if toggle != "" {
			values["OMDI_RESTORE_API_KEY_ON_STARTUP"] = toggle
		}
		got, err := Load(lookupFrom(values))
		if err != nil || got.RestoreAPIKeyOnStartup || got.APIKey != "" {
			t.Fatalf("disabled restoration retained or validated the secret: %v", err)
		}
	}
}
