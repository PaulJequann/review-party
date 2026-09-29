package configurationhub

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

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
	input := "1\nselected\n1\n2\n2\n8m\n2\nReview carefully.\nn\ny\n"
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

// refreshGatedAdapter reports models only once the script's Refresh answer
// has been read. The opening discovery runs concurrently with the manual
// refresh, so counting calls cannot tell the two apart.
type refreshGatedAdapter struct {
	answered <-chan struct{}
	models   []discovery.Model
}

func (refreshGatedAdapter) Reviewer() string { return "codex" }
func (adapter refreshGatedAdapter) Discover(context.Context) discovery.Observation {
	observation := discovery.Observation{
		Status: discovery.StatusSupported, Authentication: discovery.Authentication{Status: discovery.AuthAvailable},
	}
	select {
	case <-adapter.answered:
		observation.Models = adapter.models
	default:
	}
	return observation
}

// signalAfterLines closes answered once the given number of script lines has
// been read. holdingInput returns one line per Read.
type signalAfterLines struct {
	*holdingInput
	remaining int
	answered  chan struct{}
}

func (input *signalAfterLines) Read(p []byte) (int, error) {
	n, err := input.holdingInput.Read(p)
	input.remaining--
	if input.remaining == 0 {
		close(input.answered)
	}
	return n, err
}

func TestAccessibleProfileRefreshSelectsFreshModelAndEffort(t *testing.T) {
	answered := make(chan struct{})
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"}, ValidateName: func(string) error { return nil },
	})
	service := discovery.NewService(discovery.Options{
		Adapters: []discovery.Adapter{refreshGatedAdapter{answered: answered, models: []discovery.Model{{
			ID: "fresh-model", ReasoningEfforts: []string{"medium", "max"},
		}}}},
		Cache: profileChoiceCache{result: discovery.Result{Reviewer: "codex", Status: discovery.StatusSupported, ObservedAt: time.Now(), Models: []discovery.Model{{
			ID: "cached-model", ReasoningEfforts: []string{"low", "high"},
		}}}},
	})
	// Bounded context: a script misaligned with the prompts aborts instead of hanging.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	input := strings.Join([]string{
		"1", "refreshed", "1", // scope, name, reviewer
		"1",  // model: refresh (cached-model only before refresh)
		"3",  // model after refresh: fresh-model
		"2",  // effort reported by fresh-model: max
		"8m", // deadline
		"2", "Review carefully.", "n", "y",
	}, "\n")
	var output bytes.Buffer
	editor := editor{manager: manager, RunOptions: RunOptions{
		Context: ctx, Input: &signalAfterLines{holdingInput: newHoldingInput(input), remaining: 4, answered: answered},
		Output: &output, Accessible: true, Discovery: service,
	}}
	if err := editor.createProfile(); err != nil {
		t.Fatalf("create profile: %v\n%s", err, output.String())
	}
	profile, found, err := manager.LoadProfile(configuration.ScopeGlobal, "", "refreshed")
	if err != nil || !found {
		t.Fatalf("load profile: found=%v err=%v", found, err)
	}
	if profile.Model != "fresh-model" || profile.ReasoningEffort != "max" {
		t.Fatalf("published model/effort = %q/%q, want fresh-model/max\n%s", profile.Model, profile.ReasoningEffort, output.String())
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

func TestProfileModelFormShowsModelsDiscoveredAfterOpening(t *testing.T) {
	model := hubModelAtSize(t, 120, 40)
	state := &model.session.profile
	state.draft = configuration.ProfileDraft{Name: "bugs", Reviewer: "codex", Model: "old-model"}
	openSteppedForm(t, &model, model.openProfileModelForm)

	updated, command := model.Update(profileChoicesRefreshedMsg{reviewer: "codex", result: discovery.Result{
		Reviewer: "codex", Status: discovery.StatusSupported,
		Authentication: discovery.Authentication{Status: discovery.AuthAvailable},
		Models:         []discovery.Model{{ID: "discovered-model"}},
	}})
	model = pumpHubMessages(t, requireHubModel(t, updated), command)
	view := model.form.View()
	if !strings.Contains(view, "discovered-model") {
		t.Fatalf("model form did not show the discovered model:\n%s", view)
	}
	if !strings.Contains(view, "Reviewer codex: supported") {
		t.Fatalf("model form did not show the discovery diagnostic:\n%s", view)
	}
}

// pumpHubMessages runs follow-up commands until none remain. Spinner ticks
// settle on their own once options finish loading, so nothing is filtered;
// an unsettled queue or a hung command fails instead of being dropped.
func pumpHubMessages(t *testing.T, model Model, command tea.Cmd) Model {
	t.Helper()
	pending := []tea.Cmd{command}
	for steps := 0; len(pending) > 0; steps++ {
		if steps == 100 {
			t.Fatal("Hub commands did not settle within 100 steps")
		}
		next := pending[0]
		pending = pending[1:]
		if next == nil {
			continue
		}
		message := runHubTestCommand(t, next)
		if batch, ok := message.(tea.BatchMsg); ok {
			pending = append(pending, batch...)
			continue
		}
		if message == nil {
			continue
		}
		updated, follow := model.Update(message)
		model = requireHubModel(t, updated)
		pending = append(pending, follow)
	}
	return model
}

func runHubTestCommand(t *testing.T, command tea.Cmd) tea.Msg {
	t.Helper()
	result := make(chan tea.Msg, 1)
	go func() { result <- command() }()
	select {
	case message := <-result:
		return message
	case <-time.After(5 * time.Second):
		t.Fatal("Hub command did not return within 5s")
		return nil
	}
}

func TestProfileModelOptionsOfferRefreshFirstAndMarkCurrent(t *testing.T) {
	choices := []discovery.ModelChoice{
		{Model: discovery.Model{ID: "luna"}},
		{Model: discovery.Model{ID: "sol"}},
	}
	for _, test := range []struct {
		name    string
		current string
		keys    []string
		values  []string
	}{
		{"current listed", "sol",
			[]string{"↻ Refresh available models", "luna", "sol [current]", "Enter a model ID manually"},
			[]string{refreshProfileChoice, "luna", "sol", manualProfileChoice}},
		{"current missing", "retired",
			[]string{"↻ Refresh available models", "luna", "sol", "retired [current]", "Enter a model ID manually"},
			[]string{refreshProfileChoice, "luna", "sol", "retired", manualProfileChoice}},
		{"no current", "",
			[]string{"↻ Refresh available models", "luna", "sol", "Enter a model ID manually"},
			[]string{refreshProfileChoice, "luna", "sol", manualProfileChoice}},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := profileModelOptions(choices, test.current)
			if len(options) != len(test.keys) {
				t.Fatalf("model options = %#v", options)
			}
			for index, option := range options {
				if option.Key != test.keys[index] || option.Value != test.values[index] {
					t.Fatalf("option %d = %q/%q, want %q/%q", index, option.Key, option.Value, test.keys[index], test.values[index])
				}
			}
		})
	}
}

func TestProfileModelRefreshReopensDiscovery(t *testing.T) {
	service := discovery.NewService(discovery.Options{Adapters: []discovery.Adapter{
		profileChoiceAdapter{reviewer: "codex", models: []discovery.Model{{ID: "fresh-model"}}},
	}})
	manager := configuration.NewManager(configuration.Options{GlobalRoot: t.TempDir(), Reviewers: []string{"codex"}})
	runtime := newHubRuntime(RunOptions{Context: context.Background(), Discovery: service}, manager)
	defer runtime.closeProfileChoices()
	model := hubModelAtSize(t, 120, 40)
	model.runtime = runtime
	state := &model.session.profile
	state.draft = configuration.ProfileDraft{Name: "bugs", Reviewer: "codex", Model: "old-model"}
	openSteppedForm(t, &model, model.openProfileModelForm)
	state = &model.session.profile
	state.selected = refreshProfileChoice

	updated, command := model.completeProfileModelForm()
	model = requireHubModel(t, updated)
	if model.formKind != formProfileChoicesLoading {
		t.Fatalf("form kind after refresh = %q", model.formKind)
	}
	if model.session.profile.draft.Model != "old-model" {
		t.Fatalf("refresh changed the draft model to %q", model.session.profile.draft.Model)
	}
	model = pumpHubMessages(t, model, command)
	if model.formKind != formProfileModel {
		t.Fatalf("form kind after discovery = %q", model.formKind)
	}
	if view := model.form.View(); !strings.Contains(view, "fresh-model") || !strings.Contains(view, "old-model [current]") {
		t.Fatalf("refreshed model form:\n%s", view)
	}
}

func TestProfileModelFormStartsOnCurrentThenFirstModelThenRefresh(t *testing.T) {
	choices := []discovery.ModelChoice{{Model: discovery.Model{ID: "luna"}}, {Model: discovery.Model{ID: "sol"}}}
	for _, test := range []struct {
		name    string
		current string
		choices []discovery.ModelChoice
		want    string
		cursor  string
	}{
		{"new Profile", "", choices, "luna", "> luna"},
		{"edit keeps current", "sol", choices, "sol", "> sol [current]"},
		{"no discovered models", "", nil, refreshProfileChoice, "> ↻ Refresh available models"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := hubModelAtSize(t, 120, 40)
			state := &model.session.profile
			state.draft = configuration.ProfileDraft{Name: "bugs", Reviewer: "codex", Model: test.current}
			state.choices = test.choices
			// Pump until the lazily loaded options arrive and settle the cursor.
			command := model.openProfileModelForm()
			model = pumpHubMessages(t, model, command)
			if got := model.session.profile.selected; got != test.want {
				t.Fatalf("selected = %q, want %q", got, test.want)
			}
			if view := stripANSI(model.form.View()); !strings.Contains(view, test.cursor) {
				t.Fatalf("cursor is not on %q:\n%s", test.cursor, view)
			}
		})
	}
}

func TestAccessibleModelDefaultsToCurrentThenFirstModelThenRefresh(t *testing.T) {
	choices := []discovery.ModelChoice{{Model: discovery.Model{ID: "luna"}}, {Model: discovery.Model{ID: "sol"}}}
	for _, test := range []struct {
		name    string
		current string
		choices []discovery.ModelChoice
		want    string
	}{
		{"new Profile", "", choices, "luna"},
		{"edit keeps current", "sol", choices, "sol"},
		// Refresh is the default, and the refreshed prompt then defaults
		// to the model it discovered.
		{"no discovered models", "", nil, "fresh-model"},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Bounded context: a default that keeps choosing Refresh aborts instead of looping.
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var output bytes.Buffer
			editor := editor{
				manager: configuration.NewManager(configuration.Options{GlobalRoot: t.TempDir(), Reviewers: []string{"codex"}}),
				RunOptions: RunOptions{
					Context: ctx, Input: newLineInput("\n\n"), Output: &output, Accessible: true,
					Discovery: discovery.NewService(discovery.Options{Adapters: []discovery.Adapter{
						profileChoiceAdapter{reviewer: "codex", models: []discovery.Model{{ID: "fresh-model"}}},
					}}),
				},
			}
			draft := configuration.ProfileDraft{Name: "bugs", Reviewer: "codex", Model: test.current}
			if _, err := editor.editAccessibleModel(&draft, test.choices); err != nil {
				t.Fatalf("edit model: %v\n%s", err, output.String())
			}
			if draft.Model != test.want {
				t.Fatalf("model = %q, want %q\n%s", draft.Model, test.want, output.String())
			}
		})
	}
}
