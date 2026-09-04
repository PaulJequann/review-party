package main

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/mattn/go-isatty"

	"reviewparty/internal/model"
)

// Live run progress rendering. The engine emits one event per reviewer start
// and completion; this append-only renderer prints one stderr line per event
// so a run shows live per-reviewer activity instead of minutes of silence.
// Lines are append-only: safe under concurrency, piped or redirected runs
// stay silent, and the final bundle document remains the last output.

// runProgressRenderer serializes progress events from concurrent reviewers and
// renders each as one stderr line.
type runProgressRenderer struct {
	mutex   sync.Mutex
	output  io.Writer
	stopped bool
}

// newRunProgressSink returns the renderer for one run and its engine sink, or
// (nil, nil) when progress must stay silent: non-human output (JSON stdout
// must remain a single parseable document) or a non-terminal stderr (pipes and
// CI logs keep clean, prefixable output).
func newRunProgressSink(format string, stderr io.Writer) (*runProgressRenderer, func(model.RunProgressEvent)) {
	if format != "human" || stderr == nil || !isStderrTerminal(stderr) {
		return nil, nil
	}
	renderer := &runProgressRenderer{output: stderr}
	return renderer, renderer.handle
}

func isStderrTerminal(stderr io.Writer) bool {
	file, ok := stderr.(*os.File)
	return ok && (isatty.IsTerminal(file.Fd()) || isatty.IsCygwinTerminal(file.Fd()))
}

// handle satisfies engine.Config.Progress; events arrive from multiple
// reviewer goroutines so every write is serialized.
func (renderer *runProgressRenderer) handle(event model.RunProgressEvent) {
	renderer.mutex.Lock()
	defer renderer.mutex.Unlock()
	if renderer.stopped {
		return
	}
	fmt.Fprintf(renderer.output, "%s\n", renderRunProgressEvent(event)) //nolint:errcheck // Progress is best-effort; a closed stderr must not fail the run.
}

// stop makes late events inert so the final bundle document is the last output
// a human sees. Safe to call on a nil renderer or more than once.
func (renderer *runProgressRenderer) stop() {
	if renderer == nil {
		return
	}
	renderer.mutex.Lock()
	defer renderer.mutex.Unlock()
	renderer.stopped = true
}

func renderRunProgressEvent(event model.RunProgressEvent) string {
	label := event.Scope + ":" + event.Profile
	if event.Scope == "explicit" {
		label = event.Profile
	}
	switch event.Kind {
	case model.RunProgressStarted:
		return fmt.Sprintf("▶ %s [%d/%d] %s/%s running", label, event.Index+1, event.Total, event.Reviewer, event.Model)
	case model.RunProgressFinished:
		return fmt.Sprintf("✔ %s %s", label, runProgressOutcome(event))
	default:
		return fmt.Sprintf("· %s %s", label, event.Kind)
	}
}

func runProgressOutcome(event model.RunProgressEvent) string {
	outcome := string(event.Lifecycle)
	switch {
	case event.Lifecycle == model.LifecycleCompleted && event.Status == string(model.ResultClean):
		outcome += " · clean"
	case event.Lifecycle == model.LifecycleCompleted:
		outcome += fmt.Sprintf(" · %d finding(s)", event.FindingCount)
	case event.Message != "":
		outcome += " · " + event.Message
	}
	if event.ElapsedMS > 0 {
		outcome += " · " + formatProgressElapsed(event.ElapsedMS)
	}
	return outcome
}

// formatProgressElapsed renders milliseconds as m/s durations for progress lines.
func formatProgressElapsed(elapsedMS int64) string {
	if elapsedMS < 1000 {
		return fmt.Sprintf("%dms", elapsedMS)
	}
	seconds := elapsedMS / 1000
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	return fmt.Sprintf("%dm%ds", seconds/60, seconds%60)
}
