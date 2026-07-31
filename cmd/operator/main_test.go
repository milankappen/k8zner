package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadHCloudToken(t *testing.T) {
	t.Run("reads token from file when HCLOUD_TOKEN_FILE is set", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(path, []byte("file-token\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HCLOUD_TOKEN_FILE", path)
		t.Setenv("HCLOUD_TOKEN", "env-token")

		token, err := loadHCloudToken()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "file-token" {
			t.Errorf("expected trimmed file token to win over env, got %q", token)
		}
	})

	t.Run("falls back to trimmed env var", func(t *testing.T) {
		t.Setenv("HCLOUD_TOKEN_FILE", "")
		t.Setenv("HCLOUD_TOKEN", "  env-token\n")

		token, err := loadHCloudToken()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token != "env-token" {
			t.Errorf("expected trimmed env token, got %q", token)
		}
	})

	t.Run("errors when token file is missing", func(t *testing.T) {
		t.Setenv("HCLOUD_TOKEN_FILE", filepath.Join(t.TempDir(), "missing"))
		t.Setenv("HCLOUD_TOKEN", "env-token")

		if _, err := loadHCloudToken(); err == nil {
			t.Error("expected error for missing token file")
		}
	})

	t.Run("errors when token file is empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HCLOUD_TOKEN_FILE", path)

		if _, err := loadHCloudToken(); err == nil {
			t.Error("expected error for empty token file")
		}
	})

	t.Run("errors when nothing is set", func(t *testing.T) {
		t.Setenv("HCLOUD_TOKEN_FILE", "")
		t.Setenv("HCLOUD_TOKEN", "")

		if _, err := loadHCloudToken(); err == nil {
			t.Error("expected error when no token source is configured")
		}
	})
}
