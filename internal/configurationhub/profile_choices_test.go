package configurationhub

import (
	"bytes"
	"context"
	"testing"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/discovery"
)

type profileChoiceAdapter struct {
	reviewer string
	models   []discovery.Model
}

func (adapter profileChoiceAdapter) Reviewer() string { return adapter.reviewer }
func (adapter profileChoiceAdapter) Discover(context.Context) discovery.Observation {
	return discovery.Observation{
		Status: discovery.StatusSupported, Models: adapter.models,
		Authentication: discovery.Authentication{Status: discovery.AuthAvailable},
	}
}

type profileChoiceCache struct{ result discovery.Result }

func (cache profileChoiceCache) Load(string) (discovery.Result, bool, error) {
	return cache.result, true, nil
}
func (profileChoiceCache) Save(string, discovery.Result) error { return nil }
func (profileChoiceCache) Forget(string) error                 { return nil }

func TestAccessibleProfileSelectsCachedModelAndReportedEffort(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"}, ValidateName: func(string) error { return nil },
	})
	service := discovery.NewService(discovery.Options{
		Adapters: []discovery.Adapter{profileChoiceAdapter{reviewer: "codex"}},
		Cache: profileChoiceCache{result: discovery.Result{Reviewer: "codex", Status: discovery.StatusSupported, ObservedAt: time.Now(), Models: []discovery.Model{{
			ID: "reported-model", ReasoningEfforts: []string{"low", "high"},
		}}}},
	})
	input := "1\nselected\n1\n1\n2\n8m\n2\nReview carefully.\nn\ny\n"
	var output bytes.Buffer
	editor := editor{manager: manager, RunOptions: RunOptions{
		Context: context.Background(), Input: newLineInput(input), Output: &output, Accessible: true, Discovery: service,
	}}
	if err := editor.createProfile(); err != nil {
		t.Fatalf("create profile: %v\n%s", err, output.String())
	}
	profile, found, err := manager.LoadProfile(configuration.ScopeGlobal, "", "selected")
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if !found {
		t.Fatalf("load profile: found=%v err=%v", found, err)
	}
	wantFields := []string{"codex", "reported-model", "high"}
	gotFields := []string{profile.Reviewer, profile.Model, profile.ReasoningEffort}
	for index := range wantFields {
		if gotFields[index] != wantFields[index] {
			t.Fatalf("published execution fields = %#v", profile)
		}
	}
}

func TestManualModelCompletionClearsStaleExecutionFields(t *testing.T) {
	model := hubModelAtSize(t, 120, 30)
	state := &model.session.profile
	state.draft = configuration.ProfileDraft{Name: "bugs", Reviewer: "codex", Model: "old", ReasoningEffort: "high", AttemptDeadline: "8m"}
	_ = model.openProfileManualModelForm()
	state.manualModel = "new"
	_, _ = model.completeProfileModelManualForm()
	if state.draft.Model != "new" {
		t.Fatalf("model = %q", state.draft.Model)
	}
	if state.draft.ReasoningEffort != "" || state.draft.AttemptDeadline != "" {
		t.Fatalf("stale execution fields = %q %q", state.draft.ReasoningEffort, state.draft.AttemptDeadline)
	}
	_ = model.openProfileManualModelForm()
	state.manualModel = "new"
	_, _ = model.completeProfileModelManualForm()
	if state.draft.Model != "new" {
		t.Fatalf("model = %q", state.draft.Model)
	}
	// An unchanged model preserves the dependent fields.
	state.draft.ReasoningEffort = "low"
	state.draft.AttemptDeadline = "5m"
	_ = model.openProfileManualModelForm()
	_, _ = model.completeProfileModelManualForm()
	if state.draft.ReasoningEffort != "low" || state.draft.AttemptDeadline != "5m" {
		t.Fatalf("preserved execution fields = %q %q", state.draft.ReasoningEffort, state.draft.AttemptDeadline)
	}
}

func TestProfileChoiceRefreshFromAbandonedGenerationIsIgnored(t *testing.T) {
	service := discovery.NewService(discovery.Options{Adapters: []discovery.Adapter{
		profileChoiceAdapter{reviewer: "codex"}, profileChoiceAdapter{reviewer: "grok"},
	}})
	manager := configuration.NewManager(configuration.Options{GlobalRoot: t.TempDir(), Reviewers: []string{"codex", "grok"}})
	runtime := newHubRuntime(RunOptions{Context: context.Background(), Discovery: service}, manager)
	defer runtime.closeProfileChoices()
	model := New(Snapshot{})
	model.runtime = runtime
	model.formKind = formProfileChoicesLoading
	model.session.profile.draft.Reviewer = "codex"
	oldValue := runtime.openProfileChoices("codex")()
	oldMessage, ok := oldValue.(profileChoicesOpenedMsg)
	if !ok {
		t.Fatalf("open choices message has type %T", oldValue)
	}
	_ = runtime.openProfileChoices("grok")

	updated, command := model.receiveProfileChoicesOpened(oldMessage)
	var modelOK bool
	model, modelOK = updated.(Model)
	if !modelOK {
		t.Fatalf("updated model has type %T", updated)
	}
	if command != nil {
		t.Fatal("abandoned generation returned a command")
	}
	if len(model.session.profile.choices) != 0 {
		t.Fatalf("abandoned generation changed choices: %#v", model.session.profile.choices)
	}
}

func TestProfileModelOptionsExposeProvenanceAndManualEntry(t *testing.T) {
	options := profileModelOptions([]discovery.ModelChoice{{
		Model:   discovery.Model{ID: "luna", DisplayName: "Luna"},
		Sources: []discovery.ChoiceSource{discovery.ChoiceSourceCached, discovery.ChoiceSourceConfigured},
	}})
	if len(options) != 2 {
		t.Fatalf("model options = %#v", options)
	}
	values := []string{options[0].Value, options[1].Value}
	wantValues := []string{"luna", manualProfileChoice}
	for index := range wantValues {
		if values[index] != wantValues[index] {
			t.Fatalf("model options = %#v", options)
		}
	}
	if options[0].Key != "Luna (luna) [cached, configured]" {
		t.Fatalf("model label = %q", options[0].Key)
	}
}
