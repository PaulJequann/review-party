package configuration

import (
	"fmt"
	"path/filepath"
)

// ProfileExecutionUpdate carries a partial execution edit for one existing
// Profile. Empty fields keep the stored value; execution fields only.
type ProfileExecutionUpdate struct {
	Reviewer        string
	Model           string
	ReasoningEffort string
	AttemptDeadline string
}

// Empty reports whether the update changes nothing.
func (update ProfileExecutionUpdate) Empty() bool {
	return update.Reviewer == "" && update.Model == "" && update.ReasoningEffort == "" && update.AttemptDeadline == ""
}

// PlanProfileUpdate validates and stages an in-place execution edit of one
// existing Profile. Template provenance and instructions are retained; only
// profile.json is rewritten.
func (manager *Manager) PlanProfileUpdate(repository Repository, scope Scope, name string, update ProfileExecutionUpdate) (Plan, error) {
	if update.Empty() {
		return Plan{}, fmt.Errorf("supply at least one of reviewer, model, reasoning effort, or attempt deadline")
	}
	before, after, err := manager.resolvedExecutionProfiles(repository, scope, name, update)
	if err != nil {
		return Plan{}, err
	}
	if executionUnchanged(before, after) {
		plan := Plan{state: &planState{owner: manager}}
		plan.state.reason = fmt.Sprintf("Profile %q already uses %s", name, profilePlanSummary(before))
		return plan, nil
	}
	if err := manager.validateProfile(after, before.Instructions); err != nil {
		plan := Plan{state: &planState{owner: manager}}
		plan.state.reason = err.Error()
		return plan, nil
	}
	return manager.stageExecutionUpdate(executionUpdateRequest{
		repository: repository, scope: scope, name: name, before: before, after: after,
	})
}

func (manager *Manager) resolvedExecutionProfiles(repository Repository, scope Scope, name string, update ProfileExecutionUpdate) (Profile, Profile, error) {
	profile, found, err := manager.LoadProfile(scope, repository, name)
	if err != nil {
		return Profile{}, Profile{}, err
	}
	if !found {
		return Profile{}, Profile{}, fmt.Errorf("Profile %q does not exist in %s Configuration", name, scope)
	}
	return profile, applyExecutionUpdate(profile, update), nil
}

func applyExecutionUpdate(profile Profile, update ProfileExecutionUpdate) Profile {
	if update.Reviewer != "" {
		profile.Reviewer = update.Reviewer
	}
	if update.Model != "" {
		profile.Model = update.Model
	}
	if update.ReasoningEffort != "" {
		profile.ReasoningEffort = update.ReasoningEffort
	}
	if update.AttemptDeadline != "" {
		profile.AttemptDeadline = update.AttemptDeadline
	}
	return profile
}

func executionUnchanged(before, after Profile) bool {
	return before.Reviewer == after.Reviewer &&
		before.Model == after.Model &&
		before.ReasoningEffort == after.ReasoningEffort &&
		before.AttemptDeadline == after.AttemptDeadline
}

func (manager *Manager) stageExecutionUpdate(request executionUpdateRequest) (Plan, error) {
	entry, anchor, err := manager.profileEntry(request.scope, request.repository, request.name)
	if err != nil {
		return Plan{}, err
	}
	base := filepath.Dir(entry.Path)
	metadataPath := filepath.Join(base, "profile.json")
	oldMetadata, _, err := readRegularFile(anchor, metadataPath, "Profile metadata", MaximumDocumentBytes)
	if err != nil {
		return Plan{}, err
	}
	metadata, err := renderProfile(request.after)
	if err != nil {
		return Plan{}, err
	}
	if err := profileMetadataPayload.validate(metadata); err != nil {
		plan := Plan{state: &planState{owner: manager}}
		plan.state.reason = err.Error()
		return plan, nil
	}
	target := executionChangeTarget{scope: request.scope, path: base, name: request.name}
	changes := profileExecutionChanges(target, request.before, request.after)
	writes := []pendingWrite{
		{scope: request.scope, anchor: anchor, path: metadataPath, payload: metadata, backup: oldMetadata, existed: true},
	}
	plan := newFilePlan(manager, request.scope, changes[0], writes)
	plan.state.changes = append(plan.state.changes, changes[1:]...)
	return plan, nil
}

type executionUpdateRequest struct {
	repository Repository
	scope      Scope
	name       string
	before     Profile
	after      Profile
}

type executionChangeTarget struct {
	scope Scope
	path  string
	name  string
}

func profileExecutionChanges(target executionChangeTarget, before, after Profile) []Change {
	var changes []Change
	if before.Reviewer != after.Reviewer {
		changes = append(changes, executionChange(target, "reviewer", before.Reviewer, after.Reviewer))
	}
	if before.Model != after.Model {
		changes = append(changes, executionChange(target, "model", before.Model, after.Model))
	}
	if before.ReasoningEffort != after.ReasoningEffort {
		changes = append(changes, executionChange(target, "reasoning_effort", before.ReasoningEffort, after.ReasoningEffort))
	}
	if before.AttemptDeadline != after.AttemptDeadline {
		changes = append(changes, executionChange(target, "attempt_deadline", before.AttemptDeadline, after.AttemptDeadline))
	}
	return changes
}

func executionChange(target executionChangeTarget, field, before, after string) Change {
	return Change{
		Field: "profiles." + target.name + "." + field, Scope: target.scope, Path: target.path,
		Before: before, After: after, HadBefore: true, HadAfter: true,
	}
}
