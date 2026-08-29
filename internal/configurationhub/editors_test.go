package configurationhub

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

func TestAccessibleFormCompletesWithoutBackgroundReader(t *testing.T) {
	var value string
	editor := editor{RunOptions: RunOptions{Context: context.Background(), Input: io.NopCloser(strings.NewReader("value\n")), Output: &bytes.Buffer{}, Accessible: true}}
	if err := editor.form(huh.NewInput().Title("Name").Value(&value)); err != nil {
		t.Fatalf("form: %v", err)
	}
	if value != "value" {
		t.Fatalf("value = %q", value)
	}
}

func TestAccessibleFormReturnsWhenContextCancelled(t *testing.T) {
	input := &blockingInput{started: make(chan struct{}), release: make(chan struct{}), readDone: make(chan struct{})}
	defer func() {
		if err := input.Close(); err != nil {
			t.Errorf("close input: %v", err)
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		editor := editor{RunOptions: RunOptions{Context: ctx, Input: input, Output: io.Discard, Accessible: true}}
		result <- editor.form(huh.NewInput().Title("Name"))
	}()
	waitForSignal(t, input.started, "accessible form did not start reading")
	cancel()
	err := waitForFormResult(t, result)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("form error = %v, want %v", err, huh.ErrUserAborted)
	}
	waitForSignal(t, input.readDone, "accessible form returned before its reader stopped")
}

func waitForSignal(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(message)
	}
}

func waitForFormResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(100 * time.Millisecond):
		t.Fatal("accessible form did not return after context cancellation")
		return nil
	}
}

type blockingInput struct {
	started     chan struct{}
	release     chan struct{}
	readDone    chan struct{}
	startOnce   sync.Once
	releaseOnce sync.Once
	doneOnce    sync.Once
}

func (input *blockingInput) Read([]byte) (int, error) {
	input.startOnce.Do(func() { close(input.started) })
	<-input.release
	input.doneOnce.Do(func() { close(input.readDone) })
	return 0, io.EOF
}

func (input *blockingInput) Close() error {
	input.releaseOnce.Do(func() { close(input.release) })
	return nil
}

func TestCancelledReviewedPlanWritesNothing(t *testing.T) {
	root := t.TempDir()
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: root, Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	plan, err := manager.PlanProfileCreation("", configuration.ProfileDraft{
		Target: configuration.ScopeGlobal, Name: "quality", Reviewer: "codex",
		Model: "luna", ReasoningEffort: "high", AttemptDeadline: "8m", Instructions: "Review carefully.",
	})
	if err != nil || !plan.Valid() {
		t.Fatalf("plan = valid %v, error %v, reason %s", plan.Valid(), err, plan.Reason())
	}
	var output bytes.Buffer
	editor := editor{manager: manager, RunOptions: RunOptions{Input: io.NopCloser(strings.NewReader("n\n")), Output: &output, Accessible: true}}
	published, err := editor.reviewAndPublish(plan, func() error { return editor.manager.Publish(plan) })
	if err != nil {
		t.Fatalf("review plan: %v", err)
	}
	if published {
		t.Fatal("cancelled plan reported publication")
	}
	if _, err := os.Stat(filepath.Join(root, "profiles", "quality")); !os.IsNotExist(err) {
		t.Fatalf("cancel wrote Profile: %v", err)
	}
}

func TestDeclinedProfileReviewReturnsToEditableDraft(t *testing.T) {
	root := t.TempDir()
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: root, Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	flow := configuration.NewProfileOnboarding(manager, configuration.ScopeGlobal)
	if err := flow.ChooseBlank("Review carefully."); err != nil {
		t.Fatal(err)
	}
	for _, field := range []struct {
		name  configuration.OnboardingField
		value configuration.OnboardingText
	}{
		{name: configuration.OnboardingFieldName, value: "quality"},
		{name: configuration.OnboardingFieldReviewer, value: "codex"},
		{name: configuration.OnboardingFieldModel, value: "luna"},
		{name: configuration.OnboardingFieldEffort, value: "high"},
		{name: configuration.OnboardingFieldDeadline, value: "8m"},
	} {
		if err := flow.Set(field.name, field.value); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := flow.Validate(""); err != nil {
		t.Fatal(err)
	}
	editor := editor{manager: manager, drafts: draftSet{profile: flow}, RunOptions: RunOptions{Input: io.NopCloser(strings.NewReader("n\n")), Output: &bytes.Buffer{}, Accessible: true}}
	if err := editor.reviewProfile(flow); err != nil {
		t.Fatal(err)
	}
	if flow.Step() != configuration.OnboardingName {
		t.Fatalf("step after declined review = %q, want %q", flow.Step(), configuration.OnboardingName)
	}
}

func TestProfileFlowChangingBlankToTemplateClearsStaleInstructions(t *testing.T) {
	flow := newProfileFlowWithTemplate(t)
	if err := flow.ChooseBlank("old blank instructions"); err != nil {
		t.Fatal(err)
	}
	if err := flow.ChooseTemplate("bugs"); err != nil {
		t.Fatal(err)
	}
	draft := flow.Draft()
	if draft.Instructions != "" || draft.TemplateID != "bugs" {
		t.Fatalf("draft = %#v", draft)
	}
}

func TestProfileFlowChangingTemplateToBlankUsesBlankInstructions(t *testing.T) {
	flow := newProfileFlowWithTemplate(t)
	if err := flow.ChooseTemplate("bugs"); err != nil {
		t.Fatal(err)
	}
	if err := flow.ChooseBlank("blank instructions"); err != nil {
		t.Fatal(err)
	}
	draft := flow.Draft()
	if draft.TemplateID != "" || draft.Instructions != "blank instructions" {
		t.Fatalf("draft = %#v", draft)
	}
}

func newProfileFlowWithTemplate(t *testing.T) *configuration.ProfileOnboarding {
	t.Helper()
	manager := configuration.NewManager(configuration.Options{Templates: []configuration.Template{{ID: "bugs", Revision: "v1", Instructions: "template instructions"}}})
	return configuration.NewProfileOnboarding(manager, configuration.ScopeGlobal)
}

func TestCancelledBlankProfileFormRetainsPartialInstructions(t *testing.T) {
	flow := configuration.NewProfileOnboarding(nil, configuration.ScopeGlobal)
	err := retainProfileSourceOnFormError(flow, "partial instructions", huh.ErrUserAborted)
	if !errors.Is(err, huh.ErrUserAborted) {
		t.Fatalf("error = %v, want %v", err, huh.ErrUserAborted)
	}
	if got := flow.Draft().Instructions; got != "partial instructions" {
		t.Fatalf("retained instructions = %q, want partial instructions", got)
	}
}

func TestExitCanReturnToRetainedDraft(t *testing.T) {
	flow := configuration.NewProfileOnboarding(nil, configuration.ScopeGlobal)
	if err := flow.ChooseBlank("instructions"); err != nil {
		t.Fatal(err)
	}
	editor := editor{drafts: draftSet{profile: flow}, RunOptions: RunOptions{Input: io.NopCloser(strings.NewReader("n\n")), Output: &bytes.Buffer{}, Accessible: true}}
	exit, err := editor.confirmExit()
	if err != nil {
		t.Fatalf("confirm exit: %v", err)
	}
	if exit {
		t.Fatal("exit confirmed despite return choice")
	}
	if editor.drafts.profile == nil || editor.drafts.profile.Draft().Instructions != "instructions" {
		t.Fatal("draft was discarded")
	}
}

func TestEditorCommandLinePreservesQuotedArguments(t *testing.T) {
	got, err := parseCommandLine(`"/path with spaces/editor" --flag "value with spaces"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []string{"/path with spaces/editor", "--flag", "value with spaces"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("arguments = %#v, want %#v", got, want)
	}
}

func TestInstructionEditorUsesArgumentVectorAndReturnsEditedBytes(t *testing.T) {
	if os.Getenv("GO_WANT_EDITOR_HELPER") == "1" {
		path := os.Args[len(os.Args)-1]
		if err := os.WriteFile(path, []byte("edited\n"), 0o600); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	t.Setenv("EDITOR", os.Args[0]+" -test.run=TestInstructionEditorUsesArgumentVectorAndReturnsEditedBytes")
	t.Setenv("GO_WANT_EDITOR_HELPER", "1")
	got, err := editInstructions(context.Background(), "original\n", strings.NewReader(""), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("edit instructions: %v", err)
	}
	if got != "edited\n" {
		t.Fatalf("instructions = %q", got)
	}
}
