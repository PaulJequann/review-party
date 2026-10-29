package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

func expectedSavedReady(repository, profiles string, limit int, config string) string {
	return fmt.Sprintf("Saved review configuration is ready.\nEffective Profiles: %s\nRepository concurrency: %d\nReviewer access and execution are unverified.\nNext: review-party run --repo %s%s\n", profiles, limit, repository, config)
}

func TestEmptyProfilesHumanActionPreservesJSON(t *testing.T) {
	var human, machine bytes.Buffer
	profiles := []model.ProfileSummary{}
	if err := printProfiles(&human, profiles, "human"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(human.String(), "review-party init --repo .") {
		t.Fatalf("no next action: %s", human.String())
	}
	if err := printProfiles(&machine, profiles, "json"); err != nil {
		t.Fatal(err)
	}
	var result []model.ProfileSummary
	if err := json.Unmarshal(machine.Bytes(), &result); err != nil || len(result) != 0 || strings.TrimSpace(machine.String()) != "[]" {
		t.Fatalf("JSON changed: %s (%v)", machine.String(), err)
	}
}

func TestInitEmptySavedSelectionReportsNextAction(t *testing.T) {
	fixture := newInitFixture(t)
	plan, err := fixture.manager.Plan(configuration.Repository(fixture.repository), []configuration.Intent{
		configuration.SetReviewSelection{Target: configuration.ScopeRepository, Selection: configuration.DefaultReviewSelection()},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
	exit, stdout, stderr := fixture.run(t, commandIO{})
	if exit != 0 || !strings.Contains(stdout, "Missing Review selection:") || strings.Contains(stdout, "is ready") {
		t.Fatalf("exit %d, stdout %s, stderr %s", exit, stdout, stderr)
	}
}
