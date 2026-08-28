package configuration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ProfileDraft contains all choices required to create an executable Profile.
type ProfileDraft struct {
	Target           Scope
	Name             string
	Reviewer         string
	Model            string
	ReasoningEffort  string
	AttemptDeadline  string
	Instructions     string
	TemplateID       string
	TemplateRevision string
}

// Templates returns immutable packaged Templates in stable ID order.
func (manager *Manager) Templates() []Template {
	result := append([]Template(nil), manager.templates...)
	sort.Slice(result, func(left, right int) bool { return result[left].ID < result[right].ID })
	return result
}

// PlanProfileCreation validates and stages one complete two-file Profile.
func (manager *Manager) PlanProfileCreation(repository Repository, draft ProfileDraft) (Plan, error) {
	profile, instructions, err := manager.profileFromDraft(draft)
	if err != nil {
		return Plan{}, err
	}
	plan := Plan{state: &planState{}}
	if err := manager.validateProfile(profile, instructions); err != nil {
		plan.state.reason = err.Error()
		return plan, nil
	}
	anchor, directory, conflict, err := manager.profileCreationTarget(draft.Target, repository, draft.Name)
	if err != nil {
		return Plan{}, err
	}
	if conflict != "" {
		plan.state.reason = conflict
		return plan, nil
	}
	metadata, err := renderProfile(profile)
	if err != nil {
		return Plan{}, err
	}
	publication := pendingProfilePublication{scope: draft.Target, anchor: anchor, directory: directory, metadata: metadata, instructions: []byte(instructions)}
	if err := publication.validateSize(); err != nil {
		plan.state.reason = err.Error()
		return plan, nil
	}
	change := Change{
		Field: "profiles." + draft.Name, Scope: draft.Target, Path: directory,
		After: profilePlanSummary(profile), HadAfter: true,
	}
	return newProfilePlan(draft.Target, change, publication), nil
}

func (manager *Manager) profileCreationTarget(scope Scope, repository Repository, name string) (string, string, string, error) {
	entry, anchor, err := manager.profileEntry(scope, repository, name)
	if err != nil {
		return "", "", "", err
	}
	directory := filepath.Dir(entry.Path)
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return anchor, directory, "", nil
	}
	if err != nil {
		return "", "", "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return anchor, directory, fmt.Sprintf("Profile path %q is not a definition directory", directory), nil
	}
	conflict := fmt.Sprintf("Profile directory %q already exists; creation never overwrites complete or incomplete material", directory)
	return anchor, directory, conflict, nil
}

func (publication pendingProfilePublication) validateSize() error {
	if err := profileMetadataPayload.validate(publication.metadata); err != nil {
		return err
	}
	return profileInstructionsPayload.validate(publication.instructions)
}

func profilePlanSummary(profile Profile) string {
	summary := fmt.Sprintf("reviewer=%s model=%s effort=%s deadline=%s", profile.Reviewer, profile.Model, profile.ReasoningEffort, profile.AttemptDeadline)
	if profile.TemplateID != "" {
		summary += fmt.Sprintf(" template=%s@%s", profile.TemplateID, profile.TemplateRevision)
	}
	return summary
}

func (manager *Manager) profileFromDraft(draft ProfileDraft) (Profile, string, error) {
	profile := Profile{SchemaVersion: SchemaVersion, Name: draft.Name, Reviewer: draft.Reviewer, Model: draft.Model, ReasoningEffort: draft.ReasoningEffort, AttemptDeadline: draft.AttemptDeadline, TemplateID: draft.TemplateID, TemplateRevision: draft.TemplateRevision}
	if draft.TemplateID == "" {
		return profile, draft.Instructions, nil
	}
	template, found := manager.template(draft.TemplateID)
	if !found {
		return Profile{}, "", fmt.Errorf("unknown Review Profile Template %q", draft.TemplateID)
	}
	if draft.TemplateRevision != "" && draft.TemplateRevision != template.Revision {
		return Profile{}, "", fmt.Errorf("Template %q revision %q is unavailable", draft.TemplateID, draft.TemplateRevision)
	}
	profile.TemplateRevision = template.Revision
	if draft.Instructions != "" {
		return profile, draft.Instructions, nil
	}
	return profile, template.Instructions, nil
}

// LoadProfile reads one exact scoped Profile aggregate. It never falls back to
// a Template or another scope.
func (manager *Manager) LoadProfile(scope Scope, repository Repository, name string) (Profile, bool, error) {
	if err := manager.validateName(name); err != nil {
		return Profile{}, false, err
	}
	entry, anchor, err := manager.profileEntry(scope, repository, name)
	if errors.Is(err, ErrGlobalRootUnavailable) {
		return Profile{}, false, nil
	}
	if err != nil {
		return Profile{}, false, err
	}
	base := filepath.Dir(entry.Path)
	metadata, found, err := readRegularFile(anchor, filepath.Join(base, "profile.json"), "Profile metadata", MaximumDocumentBytes)
	if err != nil {
		return Profile{}, false, err
	}
	if !found {
		return manager.missingProfileMetadata(scope, repository, name)
	}
	instructions, err := readRequiredProfileInstructions(anchor, base, name)
	if err != nil {
		return Profile{}, false, err
	}
	return manager.decodeProfile(metadata, instructions, scope, name)
}

func (manager *Manager) missingProfileMetadata(scope Scope, repository Repository, name string) (Profile, bool, error) {
	entry, anchor, err := manager.profileEntry(scope, repository, name)
	if err != nil {
		return Profile{}, false, err
	}
	root, found, err := openConfigurationRoot(anchor, "Profile library")
	if err != nil || !found {
		return Profile{}, false, err
	}
	defer root.Close()
	directory := filepath.Dir(entry.Path)
	relative, err := filepath.Rel(anchor, directory)
	if err != nil {
		return Profile{}, false, err
	}
	_, found, err = readRootedDirectory(root, directoryReadRequest{relative: relative, path: directory, description: "Profile directory"})
	if err != nil || !found {
		return Profile{}, found, err
	}
	return Profile{}, true, fmt.Errorf("Profile %q is incomplete: profile.json is missing", name)
}

func readRequiredProfileInstructions(anchor, base, name string) ([]byte, error) {
	instructions, found, err := readRegularFile(anchor, filepath.Join(base, "instructions.md"), "Profile instructions", MaximumDocumentBytes)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("Profile %q is incomplete: instructions.md is missing", name)
	}
	return instructions, nil
}

func (manager *Manager) decodeProfile(metadata, instructions []byte, scope Scope, name string) (Profile, bool, error) {
	var profile Profile
	if err := strictDecode(metadata, &profile); err != nil {
		return Profile{}, false, err
	}
	if profile.Name != name {
		return Profile{}, false, fmt.Errorf("Profile name %q does not match directory %q", profile.Name, name)
	}
	if err := manager.validateProfile(profile, string(instructions)); err != nil {
		return Profile{}, false, err
	}
	profile.Instructions, profile.Scope = string(instructions), scope
	profile.Source, profile.SourceDigest = authoredProfileSource(scope, name), profileSourceRevision(profile, instructions)
	return profile, true, nil
}

func (manager *Manager) template(id string) (Template, bool) {
	for _, template := range manager.templates {
		if template.ID == id {
			return template, true
		}
	}
	return Template{}, false
}

func renderProfile(profile Profile) ([]byte, error) {
	profile.Instructions, profile.Scope, profile.Source, profile.SourceDigest = "", "", "", ""
	payload, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(payload, '\n'), nil
}

func authoredProfileSource(scope Scope, name string) string {
	if scope == ScopeRepository {
		return "repository:.reviewparty/profiles/" + name
	}
	return "global:profiles/" + name
}

// LoadParty reads and validates one exact scoped Party.
func (manager *Manager) LoadParty(scope Scope, repository Repository, name string) (Party, bool, error) {
	entry, anchor, err := manager.partyEntry(scope, repository, name)
	if errors.Is(err, ErrGlobalRootUnavailable) {
		return Party{}, false, nil
	}
	if err != nil {
		return Party{}, false, err
	}
	payload, found, err := readRegularFile(anchor, entry.Path, "Party", maximumPartyBytes)
	if err != nil || !found {
		return Party{}, found, err
	}
	return manager.decodeParty(payload, scope, name)
}

func (manager *Manager) decodeParty(payload []byte, scope Scope, name string) (Party, bool, error) {
	var party Party
	if err := strictDecode(payload, &party); err != nil {
		return Party{}, false, err
	}
	if party.Name != name {
		return Party{}, false, fmt.Errorf("Party name %q does not match file name %q", party.Name, name)
	}
	if err := manager.validateParty(party, scope); err != nil {
		return Party{}, false, err
	}
	party.Scope, party.Source = scope, authoredPartySource(scope, name)
	return party, true, nil
}

func authoredPartySource(scope Scope, name string) string {
	if scope == ScopeRepository {
		return "repository:.reviewparty/parties/" + name + ".json"
	}
	return "global:parties/" + name + ".json"
}

// EffectiveReviewSelection returns the repository-authored selection. Global
// availability is deliberately inert.
func (manager *Manager) EffectiveReviewSelection(repository Repository) (ReviewSelection, Value[ReviewSelection], error) {
	loaded, err := manager.Load(repository)
	if err != nil {
		return ReviewSelection{}, Value[ReviewSelection]{}, err
	}
	selection, value := manager.effectiveReviewSelection(loaded)
	return selection, value, nil
}

func (manager *Manager) effectiveReviewSelection(loaded Loaded) (ReviewSelection, Value[ReviewSelection]) {
	if !loaded.Repository.Present || loaded.Repository.Document.Reviews == nil {
		return ReviewSelection{}, Value[ReviewSelection]{Source: SourcePackaged}
	}
	selection := *loaded.Repository.Document.Reviews
	value := Value[ReviewSelection]{Value: selection, Authored: true, Source: SourceRepository, Path: loaded.Repository.Path}
	return selection, value
}
