package configurationhub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"

	"reviewparty/internal/configuration"
	"reviewparty/internal/discovery"
)

type hubCommands struct {
	plan                func(planRequest) tea.Cmd
	loadReviewSelection func() tea.Cmd
	loadTemplate        func(string) tea.Cmd
	loadProfile         func(scope, name string) tea.Cmd
}

type hubRuntime struct {
	context  context.Context
	input    io.Reader
	output   io.Writer
	commands hubCommands
	choices  profileChoiceRuntime
	receipts ReceiptProvider
}

type profileChoiceRuntime struct {
	mu      sync.Mutex
	next    uint64
	cancel  context.CancelFunc
	service *discovery.Service
	manager *configuration.Manager
	repo    configuration.Repository
}

type profileChoicesOpenedMsg struct {
	generation uint64
	reviewer   string
	choices    []discovery.ModelChoice
	refresh    tea.Cmd
}

type profileChoicesRefreshedMsg struct {
	generation uint64
	reviewer   string
	result     discovery.Result
}

type planRequest struct {
	kind     planKind
	profile  configuration.ProfileDraft
	party    partyFormDraft
	copy     string
	review   reviewPlanRequest
	receipts ReceiptProvider
}

type planKind string

const (
	planProfile     planKind = "profile"
	planProfileEdit planKind = "profile-edit"
	planParty       planKind = "party"
	planCopy        planKind = "copy"
	planReviews     planKind = "reviews"
)

type reviewPlanRequest struct {
	selection configuration.ReviewSelection
	draft     reviewFormDraft
}

type publishRequest struct {
	context    context.Context
	manager    *configuration.Manager
	repository configuration.Repository
	kind       planKind
	plan       configuration.Plan
	receipts   ReceiptProvider
}

type planReadyMsg struct {
	kind     planKind
	summary  string
	warnings []string
	publish  tea.Cmd
}

type planFailedMsg struct {
	kind planKind
	err  error
}

type publishResultMsg struct {
	kind       planKind
	snapshot   Snapshot
	refreshErr error
	err        error
}

type reviewSelectionLoadedMsg struct {
	selection configuration.ReviewSelection
	err       error
}

type templateLoadedMsg struct {
	template configuration.Template
	found    bool
	err      error
}

type profileLoadedMsg struct {
	scope   string
	name    string
	profile configuration.Profile
	found   bool
	err     error
}

type instructionEditResultMsg struct {
	instructions string
	err          error
}

func newHubRuntime(options RunOptions, manager *configuration.Manager) *hubRuntime {
	runtime := &hubRuntime{
		context: options.Context,
		input:   options.Input,
		output:  options.Output,
		commands: hubCommands{
			plan: func(request planRequest) tea.Cmd {
				return planRequestCommand(options.Context, manager, options.Repository, options.ModelChoiceCheck, request)
			},
			loadReviewSelection: func() tea.Cmd {
				return loadReviewSelectionCommand(options.Context, manager, options.Repository)
			},
			loadTemplate: func(id string) tea.Cmd {
				return loadTemplateCommand(options.Context, manager, id)
			},
			loadProfile: func(scope, name string) tea.Cmd {
				return loadProfileCommand(profileLoadRequest{
					context: options.Context, manager: manager, repository: options.Repository, scope: scope, name: name,
				})
			},
		},
	}
	runtime.choices = profileChoiceRuntime{service: options.Discovery, manager: manager, repo: options.Repository}
	runtime.receipts = options.Receipts
	return runtime
}

func (runtime *hubRuntime) openProfileChoices(reviewer string) tea.Cmd {
	if runtime == nil || runtime.choices.service == nil {
		return func() tea.Msg { return profileChoicesOpenedMsg{reviewer: reviewer} }
	}
	runtime.choices.mu.Lock()
	if runtime.choices.cancel != nil {
		runtime.choices.cancel()
	}
	runtime.choices.next++
	generation := runtime.choices.next
	ctx, cancel := context.WithCancel(runtime.context)
	runtime.choices.cancel = cancel
	runtime.choices.mu.Unlock()
	return func() tea.Msg {
		sources, sourceErr := runtime.choices.manager.ProfileModelChoices(runtime.choices.repo, reviewer)
		if sourceErr != nil {
			sources = configuration.ProfileModelChoiceSources{}
		}
		session := runtime.choices.service.Open(ctx, discovery.ChoiceRequest{Reviewer: reviewer, Configured: sources.Configured, Packaged: sources.Packaged})
		refresh := func() tea.Msg {
			result, ok := <-session.Refresh
			session.Close()
			if !ok {
				result = discovery.Result{Reviewer: reviewer, Status: discovery.StatusUnavailable, Diagnostic: "model discovery ended without a result"}
			}
			return profileChoicesRefreshedMsg{generation: generation, reviewer: reviewer, result: result}
		}
		return profileChoicesOpenedMsg{generation: generation, reviewer: reviewer, choices: session.Choices, refresh: refresh}
	}
}

func (runtime *hubRuntime) closeProfileChoices() {
	if runtime == nil {
		return
	}
	runtime.choices.mu.Lock()
	runtime.choices.next++
	if runtime.choices.cancel != nil {
		runtime.choices.cancel()
		runtime.choices.cancel = nil
	}
	runtime.choices.mu.Unlock()
}

func (runtime *hubRuntime) currentProfileChoiceGeneration(generation uint64) bool {
	if runtime == nil {
		return false
	}
	runtime.choices.mu.Lock()
	defer runtime.choices.mu.Unlock()
	return runtime.choices.next == generation
}

func planRequestCommand(ctx context.Context, manager *configuration.Manager, repository configuration.Repository, modelChoiceCheck func(string, string) configuration.ModelChoiceCheck, request planRequest) tea.Cmd {
	return func() tea.Msg {
		if err := contextError(ctx); err != nil {
			return planFailedMsg{kind: request.kind, err: err}
		}
		plan, err := buildPlan(manager, repository, modelChoiceCheck, request)
		if err != nil {
			return planFailedMsg{kind: request.kind, err: err}
		}
		if !plan.Valid() {
			return planFailedMsg{kind: request.kind, err: fmt.Errorf("invalid configuration plan: %s", plan.Reason())}
		}
		var summary strings.Builder
		if err := configuration.RenderPlanHuman(&summary, plan); err != nil {
			return planFailedMsg{kind: request.kind, err: err}
		}
		return planReadyMsg{
			kind: request.kind, summary: summary.String(), warnings: plan.Warnings(),
			publish: publishPlanCommand(publishRequest{
				context: ctx, manager: manager, repository: repository, kind: request.kind, plan: plan,
				receipts: request.receipts,
			}),
		}
	}
}

func buildPlan(manager *configuration.Manager, repository configuration.Repository, modelChoiceCheck func(string, string) configuration.ModelChoiceCheck, request planRequest) (configuration.Plan, error) {
	if manager == nil {
		return configuration.Plan{}, errors.New("Configuration Hub manager is unavailable")
	}
	switch request.kind {
	case planProfile, planProfileEdit:
		return buildProfilePlan(manager, repository, modelChoiceCheck, request)
	case planParty:
		draft, err := partyDraftFromForm(request.party)
		if err != nil {
			return configuration.Plan{}, err
		}
		return manager.PlanPartyCreation(repository, draft)
	case planCopy:
		return manager.PlanProfileCopy(repository, configuration.ScopeRepository, configuration.ScopeGlobal, request.copy)
	case planReviews:
		intent, err := reviewIntentFromForm(request.review.draft, request.review.selection)
		if err != nil {
			return configuration.Plan{}, err
		}
		return manager.Plan(repository, []configuration.Intent{intent})
	default:
		return configuration.Plan{}, fmt.Errorf("unknown plan kind %q", request.kind)
	}
}

func buildProfilePlan(manager *configuration.Manager, repository configuration.Repository, modelChoiceCheck func(string, string) configuration.ModelChoiceCheck, request planRequest) (configuration.Plan, error) {
	editor := editor{manager: manager, RunOptions: RunOptions{Repository: repository, ModelChoiceCheck: modelChoiceCheck}}
	if request.kind == planProfileEdit {
		return editor.planProfileEdit(request.profile.Target, request.profile.Name, request.profile)
	}
	return editor.planProfile(request.profile)
}

func publishPlanCommand(request publishRequest) tea.Cmd {
	return func() tea.Msg {
		if err := contextError(request.context); err != nil {
			return publishResultMsg{kind: request.kind, err: err}
		}
		if request.manager == nil {
			return publishResultMsg{kind: request.kind, err: errors.New("Configuration Hub manager is unavailable")}
		}
		if err := request.manager.Publish(request.plan); err != nil {
			return publishResultMsg{kind: request.kind, err: err}
		}
		snapshot, refreshErr := buildSnapshot(request.manager, request.repository)
		if refreshErr == nil {
			attachReceipts(&snapshot, request.receipts)
		}
		return publishResultMsg{kind: request.kind, snapshot: snapshot, refreshErr: refreshErr}
	}
}

func loadReviewSelectionCommand(ctx context.Context, manager *configuration.Manager, repository configuration.Repository) tea.Cmd {
	return func() tea.Msg {
		if err := contextError(ctx); err != nil {
			return reviewSelectionLoadedMsg{err: err}
		}
		if manager == nil {
			return reviewSelectionLoadedMsg{err: errors.New("Configuration Hub manager is unavailable")}
		}
		selection, _, err := manager.EffectiveReviewSelection(repository)
		if err != nil {
			return reviewSelectionLoadedMsg{err: err}
		}
		if selection.ConcurrencyLimit == 0 {
			selection = configuration.DefaultReviewSelection()
		}
		return reviewSelectionLoadedMsg{selection: selection}
	}
}

func loadTemplateCommand(ctx context.Context, manager *configuration.Manager, id string) tea.Cmd {
	return func() tea.Msg {
		if err := contextError(ctx); err != nil {
			return templateLoadedMsg{err: err}
		}
		if manager == nil {
			return templateLoadedMsg{err: errors.New("Configuration Hub manager is unavailable")}
		}
		template, found := manager.Template(id)
		return templateLoadedMsg{template: template, found: found}
	}
}

type profileLoadRequest struct {
	context    context.Context
	manager    *configuration.Manager
	repository configuration.Repository
	scope      string
	name       string
}

func loadProfileCommand(request profileLoadRequest) tea.Cmd {
	return func() tea.Msg {
		if err := contextError(request.context); err != nil {
			return profileLoadedMsg{scope: request.scope, name: request.name, err: err}
		}
		if request.manager == nil {
			return profileLoadedMsg{scope: request.scope, name: request.name, err: errors.New("Configuration Hub manager is unavailable")}
		}
		profile, found, err := request.manager.LoadProfile(configuration.Scope(request.scope), request.repository, request.name)
		return profileLoadedMsg{scope: request.scope, name: request.name, profile: profile, found: found, err: err}
	}
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func partyDraftFromForm(draft partyFormDraft) (configuration.PartyDraft, error) {
	limit, err := strconv.Atoi(strings.TrimSpace(draft.limit))
	if err != nil {
		return configuration.PartyDraft{}, fmt.Errorf("invalid concurrency limit: %w", err)
	}
	profiles := make([]configuration.ProfileReference, 0)
	rawProfiles := draft.profileRefs
	if len(rawProfiles) == 0 {
		rawProfiles = strings.Fields(draft.profiles)
	}
	for _, raw := range rawProfiles {
		scope, name := configuration.ParseScopedReference(raw)
		if scope == "" {
			scope = configuration.Scope(draft.scope)
		}
		profiles = append(profiles, configuration.ProfileReference{Scope: scope, Profile: name})
	}
	return configuration.PartyDraft{
		Target:           configuration.Scope(draft.scope),
		Name:             draft.name,
		Description:      draft.description,
		ConcurrencyLimit: limit,
		Profiles:         profiles,
	}, nil
}

func reviewIntentFromForm(draft reviewFormDraft, selection configuration.ReviewSelection) (configuration.SetReviewSelection, error) {
	switch draft.operation {
	case "add":
		return addReviewIntent(draft, selection)
	case "remove":
		return removeReviewIntent(draft, selection)
	case "move":
		return moveReviewIntent(draft, selection)
	case "concurrency":
		return concurrencyReviewIntent(draft, selection)
	default:
		return configuration.SetReviewSelection{}, fmt.Errorf("unknown Repository Reviews operation %q", draft.operation)
	}
}

func addReviewIntent(draft reviewFormDraft, selection configuration.ReviewSelection) (configuration.SetReviewSelection, error) {
	item := configuration.SelectionItem{Profile: draft.name}
	if draft.kind == "party" {
		item = configuration.SelectionItem{Party: draft.name}
	}
	return configuration.AddReviewSelection(selection, configuration.Scope(draft.scope), item), nil
}

func removeReviewIntent(draft reviewFormDraft, selection configuration.ReviewSelection) (configuration.SetReviewSelection, error) {
	index, err := strconv.Atoi(draft.index)
	if err != nil {
		return configuration.SetReviewSelection{}, err
	}
	return configuration.RemoveReviewSelection(selection, configuration.Scope(draft.scope), index)
}

func moveReviewIntent(draft reviewFormDraft, selection configuration.ReviewSelection) (configuration.SetReviewSelection, error) {
	from, err := strconv.Atoi(draft.from)
	if err != nil {
		return configuration.SetReviewSelection{}, err
	}
	to, err := strconv.Atoi(draft.to)
	if err != nil {
		return configuration.SetReviewSelection{}, err
	}
	return configuration.MoveReviewSelection(selection, configuration.Scope(draft.scope), from, to)
}

func concurrencyReviewIntent(draft reviewFormDraft, selection configuration.ReviewSelection) (configuration.SetReviewSelection, error) {
	limit, err := strconv.Atoi(draft.concurrency)
	if err != nil {
		return configuration.SetReviewSelection{}, err
	}
	selection.ConcurrencyLimit = limit
	return configuration.SetReviewSelection{Selection: selection}, nil
}
