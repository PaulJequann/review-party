package configurationhub

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

// RunFirstUse runs the first-use journey of Review Party Initialization with
// the Hub's own forms. It binds the names a declared Review selection expects
// or, without a selection, helps the Caller choose one. Every step publishes
// through its own Plan and is skipped when its outcome already holds, so a
// cancelled journey loses nothing a rerun cannot finish. Unlike Run, the
// journey's forms may run standalone outside accessible mode. After the
// selection it shows or declares a Review Checkpoint and offers its git hooks.
func RunFirstUse(manager *configuration.Manager, options RunOptions) error {
	if options.Context == nil {
		options.Context = context.Background()
	}
	journey := &editor{RunOptions: options, manager: manager, drafts: &draftSet{}, standalone: true}
	err := journey.firstUse()
	if errors.Is(err, huh.ErrUserAborted) {
		_, err = fmt.Fprintln(options.Output, "Setup stopped. Published steps are kept; rerun to finish.")
	}
	return err
}

func (e *editor) firstUse() error {
	binding, err := e.manager.SelectionBinding(e.Repository)
	if err != nil {
		return err
	}
	for _, step := range append(e.selectionSteps(binding), e.checkpointsStep) {
		if err := e.firstUseStep(step); err != nil {
			return err
		}
	}
	return nil
}

// selectionSteps choose a first Review selection, or bind each Global name a
// declared selection lacks.
func (e *editor) selectionSteps(binding configuration.SelectionBinding) []func() error {
	if !binding.Declared {
		return []func() error{e.chooseFirstReview}
	}
	var steps []func() error
	for _, missing := range globalGaps(binding.Unresolved) {
		steps = append(steps, func() error { return e.bindMissing(missing) })
	}
	return steps
}

// firstUseStep runs one journey step. A configuration failure is reported
// and leaves that step for the closing report; an abort ends the journey.
func (e *editor) firstUseStep(step func() error) error {
	err := step()
	if err == nil || errors.Is(err, huh.ErrUserAborted) {
		return err
	}
	_, writeErr := fmt.Fprintf(e.Output, "configuration error: %v\n", err)
	return writeErr
}

// globalGaps keeps the names each Caller binds in Global Configuration,
// Profiles before Parties so a new Party can reference new Profiles. Missing
// Repository definitions belong to the repository's authors.
func globalGaps(unresolved []configuration.UnresolvedReferenceError) []configuration.UnresolvedReferenceError {
	gaps := make([]configuration.UnresolvedReferenceError, 0, len(unresolved))
	for _, missing := range unresolved {
		if missing.Scope == configuration.ScopeGlobal {
			gaps = append(gaps, missing)
		}
	}
	slices.SortStableFunc(gaps, func(left, right configuration.UnresolvedReferenceError) int {
		return kindOrder(left.Kind) - kindOrder(right.Kind)
	})
	return gaps
}

func kindOrder(kind configuration.AuthoredItemKind) int {
	if kind == configuration.ItemProfile {
		return 0
	}
	return 1
}

func (e *editor) bindMissing(missing configuration.UnresolvedReferenceError) error {
	offer := true
	title := fmt.Sprintf("This repository selects Global %s %q, which your Global Configuration lacks. Create it now?", kindLabel(missing.Kind), missing.Name)
	if err := e.form(huh.NewConfirm().Title(title).Value(&offer)); err != nil || !offer {
		return err
	}
	if missing.Kind == configuration.ItemParty {
		return e.createGlobalParty(missing.Name)
	}
	e.draftSet().profile = configuration.ProfileDraft{Target: configuration.ScopeGlobal, Name: missing.Name}
	suggested := ""
	if _, found := e.manager.Template(missing.Name); found {
		suggested = missing.Name
	}
	_, err := e.publishNewProfile(suggested)
	return err
}

func (e *editor) createGlobalParty(name string) error {
	if err := e.refresh(); err != nil {
		return err
	}
	if len(globalProfileOptions(e.profileReferenceOptions())) == 0 {
		_, err := fmt.Fprintf(e.Output, "Global Party %q needs Global Profiles; create one first.\n", name)
		return err
	}
	e.draftSet().party = partyFormDraft{scope: string(configuration.ScopeGlobal), name: name}
	return e.createParty()
}

func kindLabel(kind configuration.AuthoredItemKind) string {
	if kind == configuration.ItemParty {
		return "Party"
	}
	return "Profile"
}

// firstReview is one choice of what an unselected repository reviews. The
// zero value means creating a new Profile.
type firstReview struct {
	kind  configuration.AuthoredItemKind
	scope configuration.Scope
	name  string
}

func (choice firstReview) item() configuration.SelectionItem {
	if choice.kind == configuration.ItemParty {
		return configuration.SelectionItem{Party: choice.name}
	}
	return configuration.SelectionItem{Profile: choice.name}
}

func (e *editor) chooseFirstReview() error {
	options, err := e.firstReviewOptions()
	if err != nil {
		return err
	}
	var choice firstReview
	if err := e.form(huh.NewSelect[firstReview]().Title("This repository has no Review selection. What should it review?").Options(options...).Value(&choice)); err != nil {
		return err
	}
	switch {
	case choice == firstReview{}:
		reference, err := e.publishNewProfile("")
		if err != nil {
			return err
		}
		choice = firstReview{kind: configuration.ItemProfile, scope: reference.Scope, name: reference.Profile}
	case choice.kind == configuration.ItemParty && choice.scope == configuration.ScopeGlobal:
		return e.selectGlobalParty(choice.name)
	}
	return e.addFirstReview(choice.scope, choice.item())
}

// firstReviewOptions offers every valid Party and Profile, Global first,
// followed by Profile Creation.
func (e *editor) firstReviewOptions() ([]huh.Option[firstReview], error) {
	parties, err := e.manager.PartyInventory(e.Repository)
	if err != nil {
		return nil, err
	}
	profiles, err := e.manager.ProfileInventory(e.Repository)
	if err != nil {
		return nil, err
	}
	var options []huh.Option[firstReview]
	for _, scope := range []configuration.Scope{configuration.ScopeGlobal, configuration.ScopeRepository} {
		options = appendReviewOptions(options, scope, configuration.ItemParty, parties)
		options = appendReviewOptions(options, scope, configuration.ItemProfile, profiles)
	}
	return append(options, huh.NewOption("Create a new Profile", firstReview{})), nil
}

func appendReviewOptions[T any](options []huh.Option[firstReview], scope configuration.Scope, kind configuration.AuthoredItemKind, definitions []configuration.Definition[T]) []huh.Option[firstReview] {
	for _, definition := range definitions {
		if definition.Scope != scope || definition.Err != nil {
			continue
		}
		label := fmt.Sprintf("%s %s %s", scopeLabel(scope), kindLabel(kind), definition.Name)
		options = append(options, huh.NewOption(label, firstReview{kind: kind, scope: scope, name: definition.Name}))
	}
	return options
}

func scopeLabel(scope configuration.Scope) string {
	if scope == configuration.ScopeGlobal {
		return "Global"
	}
	return "Repository"
}

// selectGlobalParty applies the shared-repository default from guided
// initialization: a committed Global Party carries no composition, so the
// journey offers to commit it as a Repository Party whose members stay Global
// Profile references. Teammates then bind Profiles, not Parties.
func (e *editor) selectGlobalParty(name string) error {
	save, err := e.offerRepositoryParty(name)
	if err != nil {
		return err
	}
	if !save {
		return e.addFirstReview(configuration.ScopeGlobal, configuration.SelectionItem{Party: name})
	}
	saved, err := e.saveRepositoryParty(name)
	if err != nil || !saved {
		return err
	}
	return e.addFirstReview(configuration.ScopeRepository, configuration.SelectionItem{Party: name})
}

// offerRepositoryParty asks whether to save the Global Party in the
// repository, preselecting yes. An existing Repository Party of that name is
// never replaced, so the Global Party is selected as chosen.
func (e *editor) offerRepositoryParty(name string) (bool, error) {
	_, exists, err := e.manager.LoadParty(configuration.ScopeRepository, e.Repository, name)
	if err != nil || exists {
		return false, err
	}
	save := true
	title := fmt.Sprintf("Save Global Party %q as a Repository Party so teammates bind only its Profiles?", name)
	err = e.form(huh.NewConfirm().Title(title).Value(&save))
	return save, err
}

func (e *editor) saveRepositoryParty(name string) (bool, error) {
	party, found, err := e.manager.LoadParty(configuration.ScopeGlobal, e.Repository, name)
	if err != nil {
		return false, err
	}
	if !found {
		return false, fmt.Errorf("Global Party %q no longer exists", name)
	}
	plan, err := e.manager.PlanPartyCreation(e.Repository, configuration.PartyDraft{
		Target: configuration.ScopeRepository, Name: name, Description: party.Description,
		ConcurrencyLimit: party.ConcurrencyLimit, Profiles: party.Profiles,
	})
	if err != nil {
		return false, err
	}
	published, err := e.reviewAndPublish(plan, func() error { return e.manager.Publish(plan) })
	if err != nil || !published {
		return false, err
	}
	_, err = fmt.Fprintf(e.Output, "Applied the shared-repository default: Repository Party %q now commits the composition of Global Party %q. Its members stay Global Profile references.\n", name, name)
	return err == nil, err
}

func (e *editor) addFirstReview(group configuration.Scope, item configuration.SelectionItem) error {
	selection, err := e.currentReviewSelection()
	if err != nil {
		return err
	}
	return e.publishReviewSelection(configuration.AddReviewSelection(selection, group, item))
}
