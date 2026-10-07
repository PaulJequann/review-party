package configuration

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// BaselinePartyName names the Global Party that composes the baseline
// Profiles.
const BaselinePartyName = "baseline"

// BaselineState is what this machine holds for one piece of the Review Party
// baseline.
type BaselineState string

const (
	BaselineMissing BaselineState = "missing"
	BaselineReady   BaselineState = "ready"
	// BaselineForeign is a Global Profile with a baseline name that was not
	// created from the same-named Template. It is kept and used as is.
	BaselineForeign BaselineState = "foreign"
	BaselineBroken  BaselineState = "broken"
	// BaselineDiffers is a Global Party baseline whose members are not the
	// baseline Profiles. It is never rewritten.
	BaselineDiffers BaselineState = "differs"
)

// BaselineMember is the Global Profile one baseline Template becomes.
type BaselineMember struct {
	Template Template
	State    BaselineState
	Path     string
	Problem  error
}

// BaselineParty is the Global Party that composes the baseline Profiles.
type BaselineParty struct {
	State   BaselineState
	Path    string
	Problem error
}

// Baseline is the Review Party baseline as this machine holds it, derived on
// every read. Members follow Template ID order.
type Baseline struct {
	Members []BaselineMember
	Party   BaselineParty
}

// ProfileExecution is the execution every created baseline Profile shares.
type ProfileExecution struct {
	Reviewer        string
	Model           string
	ReasoningEffort string
	AttemptDeadline string
}

// Complete reports whether every execution field is set.
func (execution ProfileExecution) Complete() bool {
	return !slices.Contains([]string{execution.Reviewer, execution.Model, execution.ReasoningEffort, execution.AttemptDeadline}, "")
}

// Baseline reads the Review Party baseline from Global Configuration.
func (manager *Manager) Baseline(repository Repository) (Baseline, error) {
	var baseline Baseline
	for _, template := range manager.Templates() {
		if !template.Baseline {
			continue
		}
		member, err := manager.baselineMember(repository, template)
		if err != nil {
			return Baseline{}, err
		}
		baseline.Members = append(baseline.Members, member)
	}
	party, err := manager.baselineParty(repository, baseline.references())
	if err != nil {
		return Baseline{}, err
	}
	baseline.Party = party
	return baseline, nil
}

func (manager *Manager) baselineMember(repository Repository, template Template) (BaselineMember, error) {
	member := BaselineMember{Template: template}
	entry, _, err := manager.profileEntry(ScopeGlobal, repository, template.ID)
	switch {
	case err == nil:
		member.Path = filepath.Dir(entry.Path)
	case !errors.Is(err, ErrGlobalRootUnavailable):
		return BaselineMember{}, err
	}
	profile, found, err := manager.LoadProfile(ScopeGlobal, repository, template.ID)
	switch {
	case err != nil:
		member.State, member.Problem = BaselineBroken, err
	case !found:
		member.State = BaselineMissing
	case profile.TemplateID == template.ID:
		member.State = BaselineReady
	default:
		member.State = BaselineForeign
	}
	return member, nil
}

func (manager *Manager) baselineParty(repository Repository, members []ProfileReference) (BaselineParty, error) {
	var party BaselineParty
	entry, _, err := manager.partyEntry(ScopeGlobal, repository, BaselinePartyName)
	switch {
	case err == nil:
		party.Path = entry.Path
	case !errors.Is(err, ErrGlobalRootUnavailable):
		return BaselineParty{}, err
	}
	loaded, found, err := manager.LoadParty(ScopeGlobal, repository, BaselinePartyName)
	switch {
	case err != nil:
		party.State, party.Problem = BaselineBroken, err
	case !found:
		party.State = BaselineMissing
	case slices.Equal(loaded.Profiles, members):
		party.State = BaselineReady
	default:
		party.State = BaselineDiffers
	}
	return party, nil
}

// Offered reports whether any Template belongs to the baseline.
func (baseline Baseline) Offered() bool {
	return len(baseline.Members) > 0
}

// MembersIn lists the members in one state, in Template ID order.
func (baseline Baseline) MembersIn(state BaselineState) []BaselineMember {
	var members []BaselineMember
	for _, member := range baseline.Members {
		if member.State == state {
			members = append(members, member)
		}
	}
	return members
}

// BaselineNames lists member names in order.
func BaselineNames(members []BaselineMember) []string {
	names := make([]string, 0, len(members))
	for _, member := range members {
		names = append(names, member.Template.ID)
	}
	return names
}

// Blocked explains why no baseline write may proceed: a member or the Party
// does not load, or the Party composes other Profiles under the baseline
// name. It returns nil when the baseline can be completed.
func (baseline Baseline) Blocked() error {
	var reasons []string
	for _, member := range baseline.MembersIn(BaselineBroken) {
		reasons = append(reasons, fmt.Sprintf("Global Profile %q at %s does not load: %v", member.Template.ID, member.Path, member.Problem))
	}
	switch baseline.Party.State {
	case BaselineBroken:
		reasons = append(reasons, fmt.Sprintf("Global Party %q at %s does not load: %v", BaselinePartyName, baseline.Party.Path, baseline.Party.Problem))
	case BaselineDiffers:
		reasons = append(reasons, fmt.Sprintf("Global Party %q at %s composes Profiles other than %s; edit or remove that file to use the baseline",
			BaselinePartyName, baseline.Party.Path, strings.Join(BaselineNames(baseline.Members), ", ")))
	case BaselineMissing, BaselineReady, BaselineForeign:
	}
	if len(reasons) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(reasons, "; "))
}

func (baseline Baseline) references() []ProfileReference {
	references := make([]ProfileReference, 0, len(baseline.Members))
	for _, member := range baseline.Members {
		references = append(references, ProfileReference{Scope: ScopeGlobal, Profile: member.Template.ID})
	}
	return references
}

// partyDraft is the Global Party baseline. Its Concurrency Limit lets every
// member run at once.
func (baseline Baseline) partyDraft() PartyDraft {
	return PartyDraft{
		Target: ScopeGlobal, Name: BaselinePartyName,
		Description:      "Review Party baseline: " + strings.Join(BaselineNames(baseline.Members), ", "),
		ConcurrencyLimit: len(baseline.Members), Profiles: baseline.references(),
	}
}

// Selected reports whether a Review selection's Global group holds the
// baseline Party while Templates belong to the baseline.
func (baseline Baseline) Selected(selection ReviewSelection) bool {
	return baseline.Offered() && slices.Contains(selection.Global, SelectionItem{Party: BaselinePartyName})
}

// SelectionBlocked reports why a Review selection holding the baseline Party
// cannot run the baseline, or nil when it does not hold it or can.
func (baseline Baseline) SelectionBlocked(selection ReviewSelection) error {
	if !baseline.Selected(selection) {
		return nil
	}
	return baseline.Blocked()
}

// Select adds the baseline Party to the Global group. A repository without a
// Review selection gets one whose Concurrency Limit lets every member run at
// once; an existing selection keeps its limit.
func (baseline Baseline) Select(current ReviewSelection) SetReviewSelection {
	if current.ConcurrencyLimit == 0 {
		current = ReviewSelection{ConcurrencyLimit: len(baseline.Members)}
	}
	return AddReviewSelection(current, ScopeGlobal, SelectionItem{Party: BaselinePartyName})
}

// Binds reports whether completing the baseline closes a gap in a declared
// selection: the selected Global Party baseline is missing, or it is in place
// and lacks a member.
func (baseline Baseline) Binds(selection ReviewSelection, missing UnresolvedReferenceError) bool {
	if missing.Scope != ScopeGlobal || !baseline.Selected(selection) {
		return false
	}
	if missing.Kind == ItemParty {
		return missing.Name == BaselinePartyName
	}
	return baseline.Party.State == BaselineReady && slices.Contains(BaselineNames(baseline.Members), missing.Name)
}

// Notes name what completing the baseline keeps as it is: Profiles not
// created from their Template, and a selection limit below the member count.
func (baseline Baseline) Notes(current ReviewSelection) []string {
	var notes []string
	for _, member := range baseline.MembersIn(BaselineForeign) {
		notes = append(notes, fmt.Sprintf("Kept Global Profile %s, which was not created from Template %s; the baseline runs it as is.", member.Template.ID, member.Template.ID))
	}
	members := len(baseline.Members)
	if limit := current.ConcurrencyLimit; limit != 0 && limit < members {
		notes = append(notes, fmt.Sprintf("The Review selection keeps Concurrency Limit %d, below the baseline's %d Profiles; raise it to %d to run them at once.",
			limit, members, members))
	}
	return notes
}

// PlanBaselineProfiles stages every Missing baseline member as a Global
// Profile from its Template with one shared execution. The Plan publishes
// all members or none. Existing members are never rewritten, so a baseline
// with nothing Missing yields a valid Plan without changes.
func (manager *Manager) PlanBaselineProfiles(repository Repository, execution ProfileExecution) (Plan, error) {
	baseline, err := manager.Baseline(repository)
	if err != nil {
		return Plan{}, err
	}
	invalid := Plan{state: &planState{owner: manager}}
	if blocked := baseline.Blocked(); blocked != nil {
		invalid.state.reason = blocked.Error()
		return invalid, nil
	}
	missing := baseline.MembersIn(BaselineMissing)
	if len(missing) > 0 && !execution.Complete() {
		invalid.state.reason = "baseline Profiles need a Reviewer, Model, Reasoning Effort, and Attempt Deadline"
		return invalid, nil
	}
	plans := make([]Plan, 0, len(missing))
	for _, member := range missing {
		plan, err := manager.PlanProfileCreation(repository, ProfileDraft{
			Target: ScopeGlobal, Name: member.Template.ID,
			Reviewer: execution.Reviewer, Model: execution.Model,
			ReasoningEffort: execution.ReasoningEffort, AttemptDeadline: execution.AttemptDeadline,
			TemplateID: member.Template.ID, TemplateRevision: member.Template.Revision,
		})
		if err != nil {
			return Plan{}, err
		}
		plans = append(plans, plan)
	}
	return mergePlans(manager, plans), nil
}

// PlanBaselineParty stages the Global Party baseline over the published
// baseline Profiles. A Ready Party yields a valid Plan without changes.
func (manager *Manager) PlanBaselineParty(repository Repository) (Plan, error) {
	baseline, err := manager.Baseline(repository)
	if err != nil {
		return Plan{}, err
	}
	if blocked := baseline.Blocked(); blocked != nil {
		return Plan{state: &planState{owner: manager, reason: blocked.Error()}}, nil
	}
	if baseline.Party.State == BaselineReady {
		return mergePlans(manager, nil), nil
	}
	return manager.PlanPartyCreation(repository, baseline.partyDraft())
}
