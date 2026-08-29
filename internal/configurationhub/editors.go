package configurationhub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

type editor struct {
	RunOptions
	manager *configuration.Manager
	drafts  draftSet
}

type draftSet struct {
	profile  *configuration.ProfileOnboarding
	party    partyFormDraft
	copyName string
	reviews  reviewFormDraft
}

func (drafts *draftSet) clear() { *drafts = draftSet{} }
func (drafts draftSet) empty() bool {
	return drafts.profile == nil && drafts.party == (partyFormDraft{}) && drafts.copyName == "" && drafts.reviews == (reviewFormDraft{})
}
func (drafts draftSet) descriptions() []string {
	var result []string
	if drafts.profile != nil {
		draft := drafts.profile.Draft()
		result = append(result, fmt.Sprintf("Profile %q (%s)", draft.Name, draft.Target))
	}
	if drafts.party != (partyFormDraft{}) {
		result = append(result, fmt.Sprintf("Party %q (%s)", drafts.party.name, drafts.party.scope))
	}
	if drafts.copyName != "" {
		result = append(result, fmt.Sprintf("Copy Profile %q", drafts.copyName))
	}
	if drafts.reviews != (reviewFormDraft{}) {
		result = append(result, "Repository Reviews "+drafts.reviews.operation)
	}
	return result
}

func (e *editor) draftDescriptions() []string { return e.drafts.descriptions() }

func (e *editor) draftsEmpty() bool { return e.drafts.empty() }

func (e *editor) clearDrafts() { e.drafts.clear() }

func (e *editor) form(fields ...huh.Field) error {
	ctx := e.Context
	if ctx == nil {
		ctx = context.Background()
	}
	input := e.Input
	if input == nil {
		input = io.NopCloser(strings.NewReader(""))
	}
	form := huh.NewForm(huh.NewGroup(fields...)).WithAccessible(e.Accessible).WithInput(input).WithOutput(e.Output)
	if e.Accessible {
		return runAccessibleForm(ctx, form, input)
	}
	return form.RunWithContext(ctx)
}

func runAccessibleForm(ctx context.Context, form *huh.Form, input io.ReadCloser) error {
	result := make(chan error, 1)
	go func() { result <- form.RunWithContext(ctx) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		closeErr := input.Close()
		formErr := <-result
		return errors.Join(huh.ErrUserAborted, closeErr, formErr)
	}
}

func (e *editor) search() error {
	var query string
	if err := e.form(huh.NewInput().Title("Search Templates, Profiles, Parties, and Reviews").Value(&query)); err != nil {
		return err
	}
	snapshot, err := buildSnapshot(e.manager, e.Repository)
	if err != nil {
		return err
	}
	model := New(snapshot)
	model.query = query
	_, err = io.WriteString(e.Output, model.Render())
	return err
}

func (e *editor) reviewDrafts() error {
	descriptions := e.draftDescriptions()
	if len(descriptions) == 0 {
		_, err := fmt.Fprintln(e.Output, "No unfinished drafts.")
		return err
	}
	if _, err := fmt.Fprintln(e.Output, "Unfinished drafts:"); err != nil {
		return err
	}
	for _, description := range descriptions {
		if _, err := fmt.Fprintln(e.Output, "  "+description); err != nil {
			return err
		}
	}
	var discard bool
	if err := e.form(huh.NewConfirm().Title("Discard all unfinished drafts?").Value(&discard)); err != nil {
		return err
	}
	if discard {
		e.clearDrafts()
	}
	return nil
}

func (e *editor) confirmExit() (bool, error) {
	if e.draftsEmpty() {
		return true, nil
	}
	var discard bool
	if err := e.form(huh.NewConfirm().Title("Discard unfinished drafts and exit?").Value(&discard)); err != nil {
		return false, err
	}
	if discard {
		e.clearDrafts()
	}
	return discard, nil
}

func (e *editor) chooseAction() (hubAction, error) {
	var action string
	areas := menuAreaSpecs()
	options := make([]huh.Option[string], 0, len(areas)+1)
	for _, spec := range areas {
		options = append(options, huh.NewOption(string(spec.area), string(spec.area)))
	}
	options = append(options, huh.NewOption("Search scoped configuration", string(areaSearch)), huh.NewOption("Exit", "exit"))
	err := e.form(huh.NewSelect[string]().Title("Configuration Hub").Options(options...).Value(&action))
	if action == "exit" {
		return hubAction{exit: true}, err
	}
	return hubAction{area: Area(action)}, err
}

func (e *editor) run(action hubAction) (bool, error) {
	if action.exit || action.area == "" {
		return false, nil
	}
	spec, found := areaSpecFor(action.area)
	if !found {
		return true, fmt.Errorf("unknown Hub area %q", action.area)
	}
	if spec.action == nil {
		return true, nil
	}
	return true, spec.action(e)
}

func (e *editor) reviewAndPublish(plan configuration.Plan, publish func() error) (bool, error) {
	return e.reviewAndPublishWithPreview(plan, publish, func(io.Writer) error { return nil })
}

func (e *editor) reviewAndPublishWithPreview(plan configuration.Plan, publish func() error, preview func(io.Writer) error) (bool, error) {
	if !plan.Valid() {
		return false, fmt.Errorf("invalid configuration plan: %s", plan.Reason())
	}
	if err := configuration.RenderPlanHuman(e.Output, plan); err != nil {
		return false, err
	}
	if err := configuration.RenderPlanWarningsHuman(e.Output, plan.Warnings()); err != nil {
		return false, err
	}
	if err := preview(e.Output); err != nil {
		return false, err
	}
	var confirm bool
	if err := e.form(huh.NewConfirm().Title("Publish this complete plan?").Value(&confirm)); err != nil {
		return false, err
	}
	if !confirm {
		return false, nil
	}
	if err := publish(); err != nil {
		return false, err
	}
	return true, nil
}

func scopeOptions() []huh.Option[string] {
	return []huh.Option[string]{huh.NewOption("Global", string(configuration.ScopeGlobal)), huh.NewOption("Repository", string(configuration.ScopeRepository))}
}
