package main

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"reviewparty/internal/model"
)

// Live run heartbeat. The engine emits one event per Review lifecycle
// transition; this append-only renderer prints one stderr line per event for
// every output format, so JSON stdout stays a single document while a human or
// a calling agent still sees which Reviews are pending, running, and done.

// progressMessageLimit bounds a hard-error message so a multi-kilobyte store
// or subprocess error cannot flood the heartbeat.
const progressMessageLimit = 160

// runProgressRenderer serializes progress events from concurrent reviewers and
// renders each as one stderr line, after a one-time header naming the run's
// status and wait commands.
type runProgressRenderer struct {
	mutex         sync.Mutex
	output        io.Writer
	configuration string
	announced     bool
	stopped       bool
}

// newRunProgressSink returns the renderer for one run and its engine sink, or
// (nil, nil) when --quiet suppresses the heartbeat.
func newRunProgressSink(quiet bool, stderr io.Writer, configuration string) (*runProgressRenderer, func(model.RunProgressEvent)) {
	if quiet || stderr == nil {
		return nil, nil
	}
	renderer := &runProgressRenderer{output: stderr, configuration: configuration}
	return renderer, renderer.handle
}

// handle satisfies engine.Config.Progress; events arrive from multiple
// reviewer goroutines so every write is serialized.
func (renderer *runProgressRenderer) handle(event model.RunProgressEvent) {
	renderer.mutex.Lock()
	defer renderer.mutex.Unlock()
	if renderer.stopped {
		return
	}
	lines := renderRunProgressEvent(event) + "\n"
	if !renderer.announced {
		renderer.announced = true
		lines = renderer.header(event) + "\n" + lines
	}
	io.WriteString(renderer.output, lines) //nolint:errcheck // Progress is best-effort; a closed stderr must not fail the run.
}

func (renderer *runProgressRenderer) header(event model.RunProgressEvent) string {
	suffix := configurationArgument(renderer.configuration)
	id := string(event.ReviewID)
	subject := "review " + id
	if event.BundleID != "" {
		id = string(event.BundleID)
		subject = fmt.Sprintf("bundle %s · %d review(s)", id, event.Total)
	}
	return fmt.Sprintf("%s · status: review-party status %s%s · wait: review-party wait %s%s", subject, id, suffix, id, suffix)
}

// stop makes late events inert so the final result document is the last output
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
	member := fmt.Sprintf("%s [%d/%d] %s", label, event.Index+1, event.Total, event.ReviewID)
	switch event.Kind {
	case model.RunProgressPending:
		return fmt.Sprintf("◌ %s pending", member)
	case model.RunProgressStarted:
		return fmt.Sprintf("▶ %s started · %s/%s", member, event.Reviewer, event.Model)
	case model.RunProgressAttempt:
		return fmt.Sprintf("· %s attempt %d", member, event.Attempt)
	case model.RunProgressFinished:
		return renderRunProgressFinished(member, event)
	default:
		return fmt.Sprintf("· %s %s", member, event.Kind)
	}
}

func renderRunProgressFinished(member string, event model.RunProgressEvent) string {
	hardError := event.Message != "" && event.Category == ""
	symbol := "✖"
	if event.Lifecycle == model.LifecycleCompleted && !hardError {
		symbol = "✔"
	}
	parts := []string{fmt.Sprintf("%s %s %s", symbol, member, event.Lifecycle)}
	switch {
	case event.Lifecycle == model.LifecycleCompleted && event.Status == string(model.ResultClean):
		parts = append(parts, "clean")
	case event.Lifecycle == model.LifecycleCompleted:
		parts = append(parts, fmt.Sprintf("%d finding(s)", event.FindingCount))
	}
	if event.Category != "" {
		parts = append(parts, string(event.Category))
	}
	if hardError {
		parts = append(parts, boundedProgressMessage(event.Message))
	}
	if event.ElapsedMS > 0 {
		parts = append(parts, formatProgressElapsed(event.ElapsedMS))
	}
	return strings.Join(parts, " · ")
}

func boundedProgressMessage(message string) string {
	line, _, _ := strings.Cut(message, "\n")
	runes := []rune(line)
	if len(runes) <= progressMessageLimit {
		return line
	}
	return string(runes[:progressMessageLimit]) + "…"
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
