package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"reviewparty/internal/model"
)

func TestPartiesListOmitsPackagedTemplates(t *testing.T) {
	repository := isolatedProfilesRepository(t)
	var stdout, stderr bytes.Buffer
	if exit := run(context.Background(), []string{"parties", "--repo", repository, "--format", "json"}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	var summaries []model.PartySummary
	if err := json.Unmarshal(stdout.Bytes(), &summaries); err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 0 {
		t.Fatalf("summaries = %#v", summaries)
	}
}
