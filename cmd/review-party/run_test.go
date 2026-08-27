package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

type fakeRunConductor struct {
	record       model.ReviewRecord
	profileCalls int
	runCalls     int
}

func (conductor *fakeRunConductor) ReviewExplicitProfile(context.Context, model.RunSelection) (model.ReviewRecord, error) {
	conductor.profileCalls++
	return conductor.record, nil
}

func (conductor *fakeRunConductor) Run(context.Context, model.RunSelection) (model.ReviewBundle, error) {
	conductor.runCalls++
	return model.ReviewBundle{ID: "rb_unexpected"}, nil
}

func TestExecuteRunExplicitProfileUsesOrdinaryReviewRecord(t *testing.T) {
	conductor := &fakeRunConductor{record: model.ReviewRecord{ID: "rp_explicit", Lifecycle: model.LifecycleCompleted}}
	var stdout, stderr bytes.Buffer
	exit := executeRunWithConductor(context.Background(), conductor, runOptions{
		profile: "bugs", format: "json", configuration: defaultUserConfigurationPath(), subject: model.WorkingChanges(),
	}, commandIO{output: &stdout, errors: &stderr})
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	if conductor.profileCalls != 1 || conductor.runCalls != 0 {
		t.Fatalf("calls = profile:%d run:%d, want profile:1 run:0", conductor.profileCalls, conductor.runCalls)
	}
	var record model.ReviewRecord
	if err := json.Unmarshal(stdout.Bytes(), &record); err != nil {
		t.Fatalf("output = %q: %v", stdout.String(), err)
	}
	if record.ID != conductor.record.ID {
		t.Fatalf("record ID = %q, want %q", record.ID, conductor.record.ID)
	}
	if strings.Contains(stdout.String(), "rb_unexpected") {
		t.Fatalf("output = %q, must not render a bundle", stdout.String())
	}
}
