package configuration

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

func validateReviewSelection(selection *ReviewSelection, scope Scope, manager *Manager) error {
	if selection == nil {
		return nil
	}
	if scope != ScopeRepository {
		return errors.New("reviews is a Repository Configuration field")
	}
	if selection.ConcurrencyLimit < 1 {
		return errors.New("reviews.concurrency_limit must be positive")
	}
	if err := validateSelectionItems("global", selection.Global, manager); err != nil {
		return err
	}
	return validateSelectionItems("repository", selection.Repository, manager)
}

func validateSelectionItems(group string, items []SelectionItem, manager *Manager) error {
	for index, item := range items {
		name, err := item.Name()
		if err != nil {
			return fmt.Errorf("reviews.%s[%d]: %w", group, index, err)
		}
		if err := manager.validateName(name); err != nil {
			return fmt.Errorf("reviews.%s[%d]: %w", group, index, err)
		}
	}
	return nil
}

func (manager *Manager) validateProfile(profile Profile, instructions string) error {
	if err := validateProfileIdentity(profile, manager); err != nil {
		return err
	}
	if err := validateProfileExecution(profile); err != nil {
		return err
	}
	if strings.TrimSpace(instructions) == "" {
		return errors.New("instructions.md must not be empty")
	}
	return validateTemplateProvenance(profile)
}

func validateProfileIdentity(profile Profile, manager *Manager) error {
	if profile.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d; expected %d", profile.SchemaVersion, SchemaVersion)
	}
	if err := manager.validateName(profile.Name); err != nil {
		return fmt.Errorf("name: %w", err)
	}
	return manager.validateReviewer(profile.Reviewer)
}

const maximumAttemptDeadline = 24 * time.Hour

func validateProfileExecution(profile Profile) error {
	if strings.TrimSpace(profile.Model) == "" || strings.TrimSpace(profile.ReasoningEffort) == "" {
		return errors.New("model and reasoning_effort are required")
	}
	deadline, err := time.ParseDuration(profile.AttemptDeadline)
	if err != nil || deadline <= 0 {
		return errors.New("attempt_deadline must be a positive duration")
	}
	if deadline > maximumAttemptDeadline {
		return fmt.Errorf("attempt_deadline must not exceed %s", maximumAttemptDeadline)
	}
	return nil
}

func validateTemplateProvenance(profile Profile) error {
	if (profile.TemplateID == "") != (profile.TemplateRevision == "") {
		return errors.New("template_id and template_revision must be supplied together")
	}
	return nil
}

func (manager *Manager) validateParty(party Party, scope Scope) error {
	if err := validatePartyHeader(party, manager); err != nil {
		return err
	}
	for index, reference := range party.Profiles {
		if err := manager.validatePartyReference(reference, scope); err != nil {
			return fmt.Errorf("profiles[%d]: %w", index, err)
		}
	}
	return nil
}

func validatePartyHeader(party Party, manager *Manager) error {
	if party.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d; expected %d", party.SchemaVersion, SchemaVersion)
	}
	if err := manager.validateName(party.Name); err != nil {
		return fmt.Errorf("name: %w", err)
	}
	if party.ConcurrencyLimit < 1 {
		return errors.New("concurrency_limit must be positive")
	}
	if len(party.Profiles) == 0 {
		return errors.New("profiles must not be empty")
	}
	return nil
}

func (manager *Manager) validatePartyReference(reference ProfileReference, partyScope Scope) error {
	if reference.Scope != ScopeGlobal && reference.Scope != ScopeRepository {
		return errors.New("scope must be global or repository")
	}
	if partyScope == ScopeGlobal && reference.Scope != ScopeGlobal {
		return errors.New("a Global Party may reference only Global Profiles")
	}
	return manager.validateName(reference.Profile)
}

func profileSourceRevision(profile Profile, instructions []byte) string {
	profile.Instructions, profile.Scope, profile.Source, profile.SourceDigest = "", "", "", ""
	payload := fmt.Sprintf("%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s", profile.SchemaVersion, profile.Name, profile.Reviewer, profile.Model, profile.ReasoningEffort, profile.AttemptDeadline, profile.TemplateID, profile.TemplateRevision, instructions)
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}
