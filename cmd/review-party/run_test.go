package main

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

type fakeRunConductor struct {
	records      fakeReviewLoader
	record       model.ReviewRecord
	runBundle    model.ReviewBundle
	profileCalls int
	runCalls     int
}

type failingCommandWriter struct{ err error }

func (writer failingCommandWriter) Write([]byte) (int, error) { return 0, writer.err }

func (conductor *fakeRunConductor) ReviewExplicitProfile(context.Context, model.RunSelection) (model.ReviewRecord, error) {
	conductor.profileCalls++
	return conductor.record, nil
}

func (conductor *fakeRunConductor) Run(context.Context, model.RunSelection) (model.ReviewBundle, error) {
	conductor.runCalls++
	if conductor.runBundle.ID != "" {
		return conductor.runBundle, nil
	}
	return model.ReviewBundle{ID: "rb_unexpected"}, nil
}

func (conductor *fakeRunConductor) Inspect(ctx context.Context, id model.ReviewID) (model.ReviewRecord, error) {
	return conductor.records.Inspect(ctx, id)
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
	report := decodeReport(t, stdout.String())
	if report.Bundle != nil {
		t.Fatalf("report bundle = %#v, want none for an explicit Profile", report.Bundle)
	}
	var ids []model.ReviewID
	for _, entry := range report.Reviews {
		ids = append(ids, entry.ID)
	}
	if want := []model.ReviewID{conductor.record.ID}; !reflect.DeepEqual(ids, want) {
		t.Fatalf("review IDs = %v, want %v", ids, want)
	}
	if strings.Contains(stdout.String(), "rb_unexpected") {
		t.Fatalf("output = %q, must not render a bundle", stdout.String())
	}
}

func TestExecuteRunFailsWhenHumanOutputCannotBeWritten(t *testing.T) {
	conductor := &fakeRunConductor{record: model.ReviewRecord{ID: "rp_output_failure", Lifecycle: model.LifecycleCompleted}}
	var stderr bytes.Buffer
	exit := executeRunWithConductor(context.Background(), conductor, runOptions{
		profile: "bugs", format: "human", configuration: defaultUserConfigurationPath(), subject: model.WorkingChanges(),
	}, commandIO{output: failingCommandWriter{err: errors.New("output is closed")}, errors: &stderr})
	if exit == 0 {
		t.Fatal("output failure returned a successful command result")
	}
	if !strings.Contains(stderr.String(), "write command output") {
		t.Fatalf("stderr = %q, want write diagnostic", stderr.String())
	}
}

func TestExecuteRunBundlePrintsReadableMembersAndFailsOnAnUnreadableOne(t *testing.T) {
	conductor := &fakeRunConductor{
		records: fakeReviewLoader{"rp_bugs": largePatchRecord("rp_bugs", "bugs", 1)},
		runBundle: model.ReviewBundle{ID: "rb_run", Lifecycle: model.LifecycleCompleted, Members: []model.BundleMember{
			{Scope: "global", Profile: "bugs", ReviewID: "rp_bugs", Lifecycle: model.LifecycleCompleted},
			{Scope: "global", Profile: "security", ReviewID: "rp_torn", Lifecycle: model.LifecycleCompleted},
		}},
	}
	var stdout, stderr bytes.Buffer
	exit := executeRunWithConductor(context.Background(), conductor, runOptions{
		format: "json", configuration: defaultUserConfigurationPath(), subject: model.WorkingChanges(),
	}, commandIO{output: &stdout, errors: &stderr})
	if exit != 1 || !strings.Contains(stderr.String(), "rp_torn") {
		t.Fatalf("exit = %d, stderr = %q; want exit 1 naming rp_torn", exit, stderr.String())
	}
	if got := findingCounts(decodeReport(t, stdout.String())); !reflect.DeepEqual(got, []int{1, 0}) {
		t.Fatalf("finding counts = %v, want the readable member's finding beside the unreadable member", got)
	}
}
