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

func TestAccessibleNextActionRefreshesSnapshotForEditors(t *testing.T) {
	root := t.TempDir()
	repository := configuration.Repository(t.TempDir())
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: root, Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	publishRepositoryProfile(t, manager, repository, "late")
	editor := &editor{
		manager: manager,
		RunOptions: RunOptions{
			Repository: repository, Accessible: true,
			Input: newLineInput("8\n"), Output: &bytes.Buffer{},
		},
	}
	// The editor started before the profile existed; the next menu render must
	// refresh the snapshot the editors consume, or the copy form offers nothing.
	action, err := editor.nextAction()
	if err != nil {
		t.Fatalf("nextAction: %v", err)
	}
	if !action.exit {
		t.Fatalf("action = %#v, want exit", action)
	}
	if !hubSnapshotHasProfile(editor.snapshot, "late", "repository") {
		t.Fatalf("snapshot was not refreshed: %#v", editor.snapshot.Items)
	}
}

// publishRepositoryProfile publishes one minimal complete Repository Profile.
func publishRepositoryProfile(t *testing.T, manager *configuration.Manager, repository configuration.Repository, name string) {
	t.Helper()
	plan, err := manager.PlanProfileCreation(repository, configuration.ProfileDraft{
		Target: configuration.ScopeRepository, Name: name, Reviewer: "codex",
		Model: "luna", ReasoningEffort: "high", AttemptDeadline: "8m", Instructions: "Review.",
	})
	if err != nil {
		t.Fatalf("plan profile %q: %v", name, err)
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatalf("publish profile %q: %v", name, err)
	}
}

// hubSnapshotHasProfile reports whether the snapshot inventory lists one Profile
// by name and scope.
func hubSnapshotHasProfile(snapshot Snapshot, name, scope string) bool {
	for _, item := range snapshot.Items {
		if item.Kind != itemProfile {
			continue
		}
		if item.Name == name && item.Scope == ItemScope(scope) {
			return true
		}
	}
	return false
}

func TestAccessibleChooseActionRoutesCopyOption(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	var output bytes.Buffer
	// Five area options precede the copy option; huh accessible selects take
	// a one-based option number.
	editor := editor{manager: manager, RunOptions: RunOptions{
		Input: newLineInput("6\n"), Output: &output, Accessible: true,
	}}
	action, err := editor.chooseAction()
	if err != nil {
		t.Fatalf("chooseAction: %v", err)
	}
	if action.exit || action.area != areaCopyProfile {
		t.Fatalf("copy option routed to %#v, want area %q", action, areaCopyProfile)
	}
}

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

type lineInput struct {
	data []byte
}

func newLineInput(value string) *lineInput { return &lineInput{data: []byte(value)} }

func (input *lineInput) Read(p []byte) (int, error) {
	if len(input.data) == 0 {
		return 0, io.EOF
	}
	n := len(input.data)
	if newline := bytes.IndexByte(input.data, '\n'); newline >= 0 {
		n = newline + 1
	}
	if n > len(p) {
		n = len(p)
	}
	copy(p, input.data[:n])
	input.data = input.data[n:]
	return n, nil
}

func (*lineInput) Close() error { return nil }

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

func TestProfileInstructionRevisionUsesTextFallback(t *testing.T) {
	draft := completeProfileDraft()
	editor := editor{RunOptions: RunOptions{Input: newLineInput("n\nUpdated instructions\n"), Output: &bytes.Buffer{}, Accessible: true}}
	if err := editor.reviseProfileInstructions(&draft); err != nil {
		t.Fatal(err)
	}
	if draft.Instructions != "Updated instructions" {
		t.Fatalf("instructions = %q, want Updated instructions", draft.Instructions)
	}
	if draft.TemplateID != "" || draft.TemplateRevision != "" {
		t.Fatalf("edited instructions retained template provenance: %#v", draft)
	}
}

func TestCreateProfilePublishesDirectDraft(t *testing.T) {
	root := t.TempDir()
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: root, Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	input := "1\n2\nReview bugs.\nn\nbugs\ncodex\nluna\nhigh\n8m\ny\n"
	editor := editor{manager: manager, RunOptions: RunOptions{Input: newLineInput(input), Output: &bytes.Buffer{}, Accessible: true}}
	if err := editor.createProfile(); err != nil {
		t.Fatal(err)
	}
	if editor.drafts.profile != (configuration.ProfileDraft{}) {
		t.Fatalf("published draft was retained: %#v", editor.drafts.profile)
	}
	if _, found, err := manager.LoadProfile(configuration.ScopeGlobal, "", "bugs"); err != nil || !found {
		t.Fatalf("published Profile found=%v, err=%v", found, err)
	}
}

func TestProfilePlanningErrorOffersDirectDraftRevision(t *testing.T) {
	root := t.TempDir()
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: root, Reviewers: []string{"codex"},
		ValidateName: func(name string) error {
			if name == "bad" {
				return errors.New("name is reserved")
			}
			return nil
		},
	})
	draft := completeProfileDraft()
	draft.Name = "bad"
	var output bytes.Buffer
	editor := editor{manager: manager, drafts: &draftSet{profile: draft}, RunOptions: RunOptions{
		Input: newLineInput("2\nquality\ny\n"), Output: &output, Accessible: true,
	}}
	if err := editor.createProfile(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "name is reserved") {
		t.Fatalf("planning error was not displayed: %q", output.String())
	}
	if _, found, err := manager.LoadProfile(configuration.ScopeGlobal, "", "quality"); err != nil || !found {
		t.Fatalf("revised Profile found=%v, err=%v", found, err)
	}
}

func TestDeclinedProfileReviewOffersDraftRevision(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex", "grok"},
		ValidateName: func(string) error { return nil },
	})
	draft := completeProfileDraft()
	plan, err := manager.PlanProfileCreation("", draft)
	if err != nil || !plan.Valid() {
		t.Fatalf("plan = valid %v, err = %v, reason = %s", plan.Valid(), err, plan.Reason())
	}
	editor := editor{manager: manager, drafts: &draftSet{profile: draft}, RunOptions: RunOptions{
		Input: newLineInput("n\n4\nnew-model\n"), Output: &bytes.Buffer{}, Accessible: true,
	}}
	published, err := editor.reviewAndPublish(plan, func() error { return manager.Publish(plan) })
	if err != nil {
		t.Fatal(err)
	}
	if published {
		t.Fatal("declined review reported publication")
	}
	if err := editor.reviseProfile(&editor.drafts.profile); err != nil {
		t.Fatal(err)
	}
	if editor.drafts.profile.Model != "new-model" {
		t.Fatalf("revised model = %q, want new-model", editor.drafts.profile.Model)
	}
}

func TestChangingReviewerClearsDependentExecutionFields(t *testing.T) {
	draft := completeProfileDraft()
	editor := editor{RunOptions: RunOptions{
		Input: newLineInput("3\ngrok\n"), Output: &bytes.Buffer{}, Accessible: true,
	}}
	if err := editor.reviseProfile(&draft); err != nil {
		t.Fatal(err)
	}
	if draft.Reviewer != "grok" {
		t.Fatalf("reviewer = %q, want grok", draft.Reviewer)
	}
	if draft.Model != "" || draft.ReasoningEffort != "" || draft.AttemptDeadline != "" {
		t.Fatalf("dependent fields not cleared: model=%q, effort=%q, deadline=%q", draft.Model, draft.ReasoningEffort, draft.AttemptDeadline)
	}
}

func TestReviewSelectionPreviewShowsProposedExpansion(t *testing.T) {
	root := t.TempDir()
	repository := configuration.Repository(t.TempDir())
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: root, Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	publishPreviewProfile(t, manager, configuration.ScopeGlobal, "")
	publishPreviewProfile(t, manager, configuration.ScopeRepository, repository)

	selection := configuration.ReviewSelection{
		ConcurrencyLimit: 1,
		Global:           []configuration.SelectionItem{{Profile: "shared"}},
		Repository:       []configuration.SelectionItem{{Profile: "shared"}, {Profile: "shared"}},
	}
	var output bytes.Buffer
	if err := renderReviewSelectionPreview(&output, manager, repository, selection); err != nil {
		t.Fatal(err)
	}
	preview := output.String()
	requirePreviewContains(t, preview, "resolved review selection:")
	requirePreviewContains(t, preview, "[global] shared")
	requirePreviewContains(t, preview, "[repository] shared")
	requirePreviewContains(t, preview, "deduplicated [repository] shared")
	requirePreviewContains(t, preview, `Profiles named "shared" from both Global and Repository Configuration`)
	if _, value, err := manager.EffectiveReviewSelection(repository); err != nil {
		t.Fatal(err)
	} else if value.Authored {
		t.Fatal("preview published the proposed selection")
	}
}

func publishPreviewProfile(t *testing.T, manager *configuration.Manager, target configuration.Scope, repository configuration.Repository) {
	t.Helper()
	plan, err := manager.PlanProfileCreation(repository, configuration.ProfileDraft{
		Target: target, Name: "shared", Reviewer: "codex", Model: "luna",
		ReasoningEffort: "high", AttemptDeadline: "1m", Instructions: "Review shared.\n",
	})
	if err != nil || !plan.Valid() {
		t.Fatalf("%s profile plan = valid %v, error %v, reason %s", target, plan.Valid(), err, plan.Reason())
	}
	if err := manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

func requirePreviewContains(t *testing.T, preview, want string) {
	t.Helper()
	if !strings.Contains(preview, want) {
		t.Fatalf("preview = %q, missing %q", preview, want)
	}
}

func TestProfileValidationUsesModelChoiceCheck(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	draft := completeProfileDraft()
	var selected string
	editor := editor{manager: manager, RunOptions: RunOptions{
		ModelChoiceCheck: func(reviewer, model string) configuration.ModelChoiceCheck {
			selected = reviewer + "/" + model
			return configuration.ModelChoiceCheck{Status: configuration.ModelChoicesUnknown}
		},
	}}
	plan, err := editor.planProfile(draft)
	if err != nil {
		t.Fatal(err)
	}
	if selected != "codex/luna" {
		t.Fatalf("model choice checked = %q, want codex/luna", selected)
	}
	if len(plan.Warnings()) != 1 || !strings.Contains(plan.Warnings()[0], `model "luna"`) {
		t.Fatalf("plan warnings omitted model warning: %#v", plan.Warnings())
	}
}

func TestProfileFlowChangingBlankToTemplateClearsStaleInstructions(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{Templates: []configuration.Template{{ID: "bugs", Revision: "v1", Instructions: "template instructions"}}})
	draft := configuration.ProfileDraft{Target: configuration.ScopeGlobal, Instructions: "old blank instructions"}
	editor := editor{manager: manager, RunOptions: RunOptions{Input: newLineInput("1\nn\n"), Output: &bytes.Buffer{}, Accessible: true}}
	if err := editor.chooseProfileTemplate(&draft); err != nil {
		t.Fatal(err)
	}
	if draft.Instructions != "" || draft.TemplateID != "bugs" || draft.TemplateRevision != "v1" {
		t.Fatalf("draft = %#v", draft)
	}
}

func TestProfileFlowChangingTemplateToBlankUsesBlankInstructions(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{Templates: []configuration.Template{{ID: "bugs", Revision: "v1", Instructions: "template instructions"}}})
	draft := configuration.ProfileDraft{Target: configuration.ScopeGlobal, TemplateID: "bugs", TemplateRevision: "v1"}
	editor := editor{manager: manager, RunOptions: RunOptions{Input: newLineInput("blank instructions\nn\n"), Output: &bytes.Buffer{}, Accessible: true}}
	if err := editor.chooseBlankProfileInstructions(&draft); err != nil {
		t.Fatal(err)
	}
	if draft.TemplateID != "" || draft.Instructions != "blank instructions" {
		t.Fatalf("draft = %#v", draft)
	}
}

func completeProfileDraft() configuration.ProfileDraft {
	return configuration.ProfileDraft{
		Target: configuration.ScopeGlobal, Name: "quality", Reviewer: "codex", Model: "luna",
		ReasoningEffort: "high", AttemptDeadline: "8m", Instructions: "Review carefully.",
	}
}

func TestExitCanReturnToRetainedDraft(t *testing.T) {
	editor := editor{drafts: &draftSet{profile: configuration.ProfileDraft{Target: configuration.ScopeGlobal, Instructions: "instructions"}}, RunOptions: RunOptions{Input: io.NopCloser(strings.NewReader("n\n")), Output: &bytes.Buffer{}, Accessible: true}}
	exit, err := editor.confirmExit()
	if err != nil {
		t.Fatalf("confirm exit: %v", err)
	}
	if exit {
		t.Fatal("exit confirmed despite return choice")
	}
	if editor.drafts.profile.Instructions != "instructions" {
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

func TestWindowsEditorCommandLinePreservesPathSeparators(t *testing.T) {
	got, err := parseWindowsCommandLine(`"C:\Program Files\Editor\editor.exe" --wait "C:\tmp\instructions.md"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []string{`C:\Program Files\Editor\editor.exe`, "--wait", `C:\tmp\instructions.md`}
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
