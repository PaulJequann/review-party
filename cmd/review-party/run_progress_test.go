package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

func bugsMemberEvent(kind model.RunProgressKind) model.RunProgressEvent {
	return model.RunProgressEvent{
		Kind: kind, BundleID: "rb_X", ReviewID: "rp_A", Index: 0, Total: 2,
		Scope: "global", Profile: "bugs", Reviewer: "grok", Model: "grok-4.5",
	}
}

func TestRenderRunProgressEventLines(t *testing.T) {
	attempt := bugsMemberEvent(model.RunProgressAttempt)
	attempt.Attempt = 1
	findings := bugsMemberEvent(model.RunProgressFinished)
	findings.Lifecycle, findings.Status, findings.FindingCount, findings.ElapsedMS = model.LifecycleCompleted, "findings", 2, 65000
	clean := bugsMemberEvent(model.RunProgressFinished)
	clean.Lifecycle, clean.Status, clean.ElapsedMS = model.LifecycleCompleted, "clean", 250
	deadline := bugsMemberEvent(model.RunProgressFinished)
	deadline.Lifecycle, deadline.Category, deadline.Message, deadline.ElapsedMS = model.LifecycleIncomplete, model.TerminationDeadlineExceeded, "attempt exceeded 8m0s", 480000
	hardError := bugsMemberEvent(model.RunProgressFinished)
	hardError.Lifecycle, hardError.Message, hardError.ElapsedMS = model.LifecycleRunning, "save review: disk full\nsecond line", 1200
	for _, test := range []struct {
		event model.RunProgressEvent
		want  string
	}{
		{bugsMemberEvent(model.RunProgressPending), "◌ global:bugs [1/2] rp_A pending"},
		{bugsMemberEvent(model.RunProgressStarted), "▶ global:bugs [1/2] rp_A started · grok/grok-4.5"},
		{attempt, "· global:bugs [1/2] rp_A attempt 1"},
		{findings, "✔ global:bugs [1/2] rp_A completed · 2 finding(s) · 1m5s"},
		{clean, "✔ global:bugs [1/2] rp_A completed · clean · 250ms"},
		{deadline, "✖ global:bugs [1/2] rp_A incomplete · deadline_exceeded · 8m0s"},
		{hardError, "✖ global:bugs [1/2] rp_A running · save review: disk full · 1s"},
	} {
		if got := renderRunProgressEvent(test.event); got != test.want {
			t.Fatalf("line = %q, want %q", got, test.want)
		}
	}
}

func TestRenderRunProgressEventBoundsHardErrorMessages(t *testing.T) {
	event := bugsMemberEvent(model.RunProgressFinished)
	event.Lifecycle, event.Message = model.LifecycleRunning, strings.Repeat("x", 500)
	line := renderRunProgressEvent(event)
	if !strings.Contains(line, strings.Repeat("x", progressMessageLimit)+"…") || strings.Contains(line, strings.Repeat("x", progressMessageLimit+1)) {
		t.Fatalf("line = %q, want the message cut at %d runes", line, progressMessageLimit)
	}
}

func TestRenderRunProgressEventUsesProfileNameForExplicitSelections(t *testing.T) {
	line := renderRunProgressEvent(model.RunProgressEvent{
		Kind: model.RunProgressStarted, ReviewID: "rp_A", Index: 0, Total: 1,
		Scope: "explicit", Profile: "code-quality", Reviewer: "grok", Model: "grok-4.5",
	})
	if line != "▶ code-quality [1/1] rp_A started · grok/grok-4.5" {
		t.Fatalf("explicit line = %q, want the bare Profile name", line)
	}
}

func TestRunProgressRendererAnnouncesTheRunOnce(t *testing.T) {
	for _, test := range []struct {
		name          string
		first         model.RunProgressEvent
		configuration string
		want          string
	}{
		{"bundle", bugsMemberEvent(model.RunProgressPending), defaultUserConfigurationPath(),
			"bundle rb_X · 2 review(s) · status: review-party status rb_X · wait: review-party wait rb_X"},
		{"explicit", model.RunProgressEvent{Kind: model.RunProgressPending, ReviewID: "rp_A", Total: 1, Scope: "explicit", Profile: "bugs"}, "/tmp/it's.json",
			`review rp_A · status: review-party status rp_A --config '/tmp/it'"'"'s.json' · wait: review-party wait rp_A --config '/tmp/it'"'"'s.json'`},
	} {
		var output bytes.Buffer
		renderer, sink := newRunProgressSink(false, &output, test.configuration)
		sink(test.first)
		second := test.first
		second.Index = 1
		sink(second)
		renderer.stop()
		sink(test.first)
		lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
		if len(lines) != 3 || lines[0] != test.want {
			t.Fatalf("%s lines = %#v, want header %q then two pending lines and nothing after stop", test.name, lines, test.want)
		}
	}
}

func TestQuietRunInstallsNoProgressSink(t *testing.T) {
	if renderer, sink := newRunProgressSink(true, &bytes.Buffer{}, defaultUserConfigurationPath()); renderer != nil || sink != nil {
		t.Fatal("--quiet must not install a progress sink")
	}
}

type progressRunConductor struct {
	fakeRunConductor
	progress func(model.RunProgressEvent)
}

func (conductor *progressRunConductor) Run(ctx context.Context, selection model.RunSelection) (model.ReviewBundle, error) {
	finished := bugsMemberEvent(model.RunProgressFinished)
	finished.Lifecycle, finished.Status = model.LifecycleCompleted, "clean"
	for _, event := range []model.RunProgressEvent{bugsMemberEvent(model.RunProgressPending), bugsMemberEvent(model.RunProgressStarted), finished} {
		conductor.progress(event)
	}
	return conductor.fakeRunConductor.Run(ctx, selection)
}

func TestJSONRunKeepsStdoutCleanWhileStderrCarriesTheHeartbeat(t *testing.T) {
	var stdout, stderr bytes.Buffer
	renderer, sink := newRunProgressSink(false, &stderr, defaultUserConfigurationPath())
	conductor := &progressRunConductor{
		fakeRunConductor: fakeRunConductor{runBundle: model.ReviewBundle{ID: "rb_X", Lifecycle: model.LifecycleCompleted, Members: []model.BundleMember{}}},
		progress:         sink,
	}
	exit := executeRunWithConductor(context.Background(), conductor, runOptions{
		format: "json", configuration: defaultUserConfigurationPath(), subject: model.WorkingChanges(), progress: sink,
	}, commandIO{output: &stdout, errors: &stderr})
	renderer.stop()
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr.String())
	}
	var report reviewReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || report.Bundle == nil || report.Bundle.ID != "rb_X" {
		t.Fatalf("stdout = %q is not the bundle report: %v", stdout.String(), err)
	}
	for _, want := range []string{"bundle rb_X · 2 review(s)", "◌ global:bugs [1/2] rp_A pending", "▶ global:bugs [1/2] rp_A started", "✔ global:bugs [1/2] rp_A completed · clean"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}
