package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

func TestVersionReportsRuntimeProvenance(t *testing.T) {
	original := currentRuntimeProvenance
	t.Cleanup(func() { currentRuntimeProvenance = original })
	modified := true
	currentRuntimeProvenance = func() model.RuntimeProvenance {
		return model.RuntimeProvenance{Version: "v0.1.0", VCSRevision: "abc123", VCSModified: &modified}
	}

	var stdout, stderr bytes.Buffer
	if exit := run(context.Background(), []string{"version"}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	for _, fact := range []string{"Review Party v0.1.0", "revision: abc123", "modified: true"} {
		if !strings.Contains(stdout.String(), fact) {
			t.Fatalf("output %q does not contain %q", stdout.String(), fact)
		}
	}
}

func TestVersionJSONIsMachineReadable(t *testing.T) {
	original := currentRuntimeProvenance
	t.Cleanup(func() { currentRuntimeProvenance = original })
	modified := false
	currentRuntimeProvenance = func() model.RuntimeProvenance {
		return model.RuntimeProvenance{VCSRevision: "def456", VCSModified: &modified}
	}

	var stdout, stderr bytes.Buffer
	if exit := run(context.Background(), []string{"version", "--format", "json"}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	var got model.RuntimeProvenance
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if got.VCSRevision != "def456" {
		t.Fatalf("revision = %q", got.VCSRevision)
	}
	if got.VCSModified == nil {
		t.Fatal("modified state is absent")
	}
	if *got.VCSModified {
		t.Fatal("modified = true")
	}
}
