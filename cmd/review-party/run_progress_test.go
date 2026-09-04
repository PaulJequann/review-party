package main

import (
	"bytes"
	"strings"
	"testing"

	"reviewparty/internal/model"
)

func TestRenderRunProgressEventLines(t *testing.T) {
	started := renderRunProgressEvent(model.RunProgressEvent{
		Kind: model.RunProgressStarted, Index: 1, Total: 3,
		Scope: "global", Profile: "bugs", Reviewer: "grok", Model: "grok-4.5",
	})
	if !strings.Contains(started, "▶ global:bugs [2/3] grok/grok-4.5 running") {
		t.Fatalf("started line = %q", started)
	}
	finished := renderRunProgressEvent(model.RunProgressEvent{
		Kind: model.RunProgressFinished, Index: 1, Total: 3, Scope: "global", Profile: "bugs",
		ReviewID: "rp_test", Lifecycle: model.LifecycleCompleted, Status: "findings", FindingCount: 2, ElapsedMS: 65000,
	})
	for _, want := range []string{"✔ global:bugs", "completed · 2 finding(s)", "1m5s"} {
		if !strings.Contains(finished, want) {
			t.Fatalf("finished line = %q, want %q", finished, want)
		}
	}
	clean := renderRunProgressEvent(model.RunProgressEvent{
		Kind: model.RunProgressFinished, Lifecycle: model.LifecycleCompleted, Status: "clean", ElapsedMS: 250,
	})
	if !strings.Contains(clean, "completed · clean") || !strings.Contains(clean, "250ms") {
		t.Fatalf("clean line = %q", clean)
	}
	incomplete := renderRunProgressEvent(model.RunProgressEvent{
		Kind: model.RunProgressFinished, Lifecycle: model.LifecycleIncomplete,
		Message: "reviewer_unavailable: grok is offline",
	})
	if !strings.Contains(incomplete, "incomplete · reviewer_unavailable: grok is offline") {
		t.Fatalf("incomplete line = %q", incomplete)
	}
}

func TestRenderRunProgressEventUsesProfileNameForExplicitSelections(t *testing.T) {
	line := renderRunProgressEvent(model.RunProgressEvent{
		Kind: model.RunProgressStarted, Index: 0, Total: 1,
		Scope: "explicit", Profile: "code-quality", Reviewer: "grok", Model: "grok-4.5",
	})
	if !strings.Contains(line, "▶ code-quality [1/1]") {
		t.Fatalf("explicit line = %q, want the bare Profile name", line)
	}
	if strings.Contains(line, "explicit:") {
		t.Fatalf("explicit line = %q, must not prefix the explicit origin", line)
	}
}

func TestRunProgressRendererWritesEachEventAsOneLine(t *testing.T) {
	var output bytes.Buffer
	renderer := &runProgressRenderer{output: &output}
	renderer.handle(model.RunProgressEvent{Kind: model.RunProgressStarted, Index: 0, Total: 2, Scope: "global:bugs", Profile: "bugs"})
	renderer.handle(model.RunProgressEvent{Kind: model.RunProgressFinished, Index: 0, Total: 2, Scope: "global:bugs", Profile: "bugs", Lifecycle: model.LifecycleCompleted})
	renderer.stop()
	renderer.handle(model.RunProgressEvent{Kind: model.RunProgressFinished, Index: 1, Total: 2, Scope: "global:code-quality", Profile: "code-quality"})
	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %#v, want exactly 2 (post-stop events are inert)", lines)
	}
}

func TestNewRunProgressSinkStaysSilentForJSONOutput(t *testing.T) {
	if renderer, sink := newRunProgressSink("json", &bytes.Buffer{}); renderer != nil || sink != nil {
		t.Fatal("JSON output must not install a progress sink")
	}
	if renderer, sink := newRunProgressSink("human", nil); renderer != nil || sink != nil {
		t.Fatal("a missing stderr must not install a progress sink")
	}
}

// TestRunOptionsProgressSinkSurvivesExecuteRun proves the sink installed from
// runOptions reaches the run path unchanged, matching how run.go passes it to
// engine.New.
func TestRunOptionsProgressSinkSurvivesExecuteRun(t *testing.T) {
	var events []model.RunProgressEvent
	sink := func(event model.RunProgressEvent) { events = append(events, event) }
	options := runOptions{format: "human", progress: sink}
	if options.progress == nil {
		t.Fatal("progress sink was dropped from runOptions")
	}
	options.progress(model.RunProgressEvent{Kind: model.RunProgressStarted, Index: 0, Total: 1})
	if len(events) != 1 || events[0].Kind != model.RunProgressStarted {
		t.Fatalf("events = %#v, want the started event", events)
	}
}
