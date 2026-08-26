package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPathUsesGlobalConfigurationRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	var stdout, stderr bytes.Buffer
	if exit := run(context.Background(), []string{"config", "path"}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	want := filepath.Join(root, "review-party", "config.json") + "\n"
	if stdout.String() != want {
		t.Fatalf("path = %q, want %q", stdout.String(), want)
	}
}

func TestConfigShowReadsSelectedConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selected.json")
	payload := `{"schema_version":1,"defaults":{"reviewer":"opencode"}}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if exit := run(context.Background(), []string{"config", "file", "show", "--config", path}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if stdout.String() != payload+"\n" {
		t.Fatalf("output = %q", stdout.String())
	}
}
