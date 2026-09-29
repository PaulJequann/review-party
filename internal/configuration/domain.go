package configuration

import "errors"

// ProfileReference identifies an executable Profile without relying on name
// precedence. Parties always store the scope explicitly.
type ProfileReference struct {
	Scope   Scope  `json:"scope"`
	Profile string `json:"profile"`
}

// SelectionItem selects either a Profile or Party in one authored scope.
type SelectionItem struct {
	Profile string `json:"profile,omitempty"`
	Party   string `json:"party,omitempty"`
}

// Name returns the selected definition name and rejects an ambiguous item.
func (item SelectionItem) Name() (string, error) {
	if (item.Profile == "") == (item.Party == "") {
		return "", errors.New("must select exactly one profile or party")
	}
	if item.Profile != "" {
		return item.Profile, nil
	}
	return item.Party, nil
}

// ReviewSelection is the complete repository-owned default roll-up.
type ReviewSelection struct {
	ConcurrencyLimit int             `json:"concurrency_limit"`
	Global           []SelectionItem `json:"global"`
	Repository       []SelectionItem `json:"repository"`
}

// Profile is one complete executable review definition.
type Profile struct {
	SchemaVersion    int    `json:"schema_version"`
	Name             string `json:"name"`
	Reviewer         string `json:"reviewer"`
	Model            string `json:"model"`
	ReasoningEffort  string `json:"reasoning_effort"`
	AttemptDeadline  string `json:"attempt_deadline"`
	TemplateID       string `json:"template_id,omitempty"`
	TemplateRevision string `json:"template_revision,omitempty"`
	// TemplateInstructionsSHA256 records the source Template's instructions
	// hash at the last template-sourced write (creation or template update).
	// Drift compares current instructions against this baseline so untouched
	// Profiles are not reported as customized, while creation-time and later
	// edits still are. Empty for Profiles written before the baseline
	// existed; those fall back to byte comparison.
	TemplateInstructionsSHA256 string `json:"template_instructions_sha256,omitempty"`
	Instructions               string `json:"-"`
	Scope                      Scope  `json:"-"`
	Source                     string `json:"-"`
	SourceDigest               string `json:"-"`
}

type Template struct {
	ID           string   `json:"id"`
	Revision     string   `json:"revision"`
	Instructions string   `json:"instructions"`
	BundledFiles []string `json:"bundled_files,omitempty"`
}

type TemplateDrift struct {
	Scope             Scope  `json:"scope"`
	Profile           string `json:"profile"`
	TemplateID        string `json:"template_id"`
	TemplateRevision  string `json:"template_revision"`
	AvailableRevision string `json:"available_revision,omitempty"`
	SourceUnavailable bool   `json:"source_unavailable,omitempty"`
	Customized        bool   `json:"customized"`
}

// Party is an ordered, flat group of scoped Profile references.
type Party struct {
	SchemaVersion    int                `json:"schema_version"`
	Name             string             `json:"name"`
	Description      string             `json:"description,omitempty"`
	ConcurrencyLimit int                `json:"concurrency_limit"`
	Profiles         []ProfileReference `json:"profiles"`
	Scope            Scope              `json:"-"`
	Source           string             `json:"-"`
}
