package engine

import (
	"embed"
	"errors"
	"fmt"
	"sort"
	"time"

	"reviewparty/internal/model"
	"reviewparty/internal/result"

	"reviewparty/internal/configuration"
)

const (
	profileConfigSchema     = 1
	profileCompilerRevision = "markdown-v1"
)

//go:embed profiles/*.md
var packagedProfileFiles embed.FS

type profileLibrary struct {
	configuration *configuration.Manager
}

var errProfileLibraryNotConfigured = errors.New("profile library requires an explicit configuration manager")

type profileRequest struct {
	repository string
	name       string
}

type resolvedProfile struct {
	name         string
	instructions string
	source       string
	path         string
	digest       string
	reviewer     string
	model        string
	effort       string
	deadline     time.Duration
	effective    configuration.Effective
}

// manager returns the Configuration Manager that owns this Profile library.
func (library profileLibrary) manager() *configuration.Manager {
	return library.configuration
}

func newProfileLibrary(globalRoot string) profileLibrary {
	options := reviewPartyConfigurationOptions()
	options.GlobalRoot = globalRoot
	return profileLibrary{configuration: configuration.NewManager(options)}
}

func (conductor *Conductor) compileProfile(selection model.ProfileSelection, resolved resolvedProfile) (compiledProfile, error) {
	reviewers := applyEffectiveReviewerPolicies(conductor.reviewers, resolved.effective)
	reviewerWasDefault := selection.Reviewer == ""
	selection.Profile = resolved.name
	if selection.Reviewer == "" {
		selection.Reviewer = resolved.reviewer
	}
	definition, err := profileDefinitionFor(resolved)
	if err != nil {
		return compiledProfile{}, err
	}
	profile, err := compileProfileDefinition(reviewers, selection, resolved.deadline, definition)
	if err != nil {
		return compiledProfile{}, err
	}
	profile.revision.Source = resolved.source
	profile.revision.SourceDigest = resolved.digest
	profile.revision.CompilerRevision = profileCompilerRevision
	profile.revision.Revision = profileRevisionIdentity(profile.revision)
	profile.snapshot = model.ProfileSnapshot{Name: resolved.name, Source: resolved.source, SourceDigest: resolved.digest, Instructions: resolved.instructions}
	profile.reviewerWasDefault = reviewerWasDefault
	profile.buildPrompt = func(subject model.ReviewSubject) string { return renderReviewPrompt(resolved, subject) }
	return profile, nil
}

func profileDefinitionFor(profile resolvedProfile) (profileDefinition, error) {
	return profileDefinition{
		name:                 profile.name,
		description:          "User-defined review Profile",
		purpose:              "Apply the authored review instructions to the Review Subject.",
		materialityThreshold: "A concrete actionable issue under the authored Profile instructions.",
		pass:                 model.ReviewPassRevision{Name: filesystemPassName(profile.name), Required: true, Purpose: "Apply the authored Profile.", PromptRevision: profileCompilerRevision + ":" + profile.digest},
		requiredCapabilities: restrictedReviewCapabilities(),
	}, nil
}

func filesystemPassName(profileName string) string {
	if profileName == "bugs" {
		return "bug-review"
	}
	return profileName + "-review"
}

func (conductor *Conductor) profileSummaries(repository string) ([]model.ProfileSummary, error) {
	inventory, err := conductor.profiles.inventory(repository)
	if err != nil {
		return nil, err
	}
	effective, err := conductor.configuration.Resolve(configuration.Request{Repository: configuration.Repository(repository)})
	if err != nil {
		return nil, err
	}
	summaries := make([]model.ProfileSummary, 0, len(inventory))
	for _, definition := range inventory {
		summary := profileInventorySummary(definition)
		if definition.Err == nil {
			summaryProfile(conductor.reviewers, effective, definition.Value, &summary)
		}
		summaries = append(summaries, summary)
	}
	sort.SliceStable(summaries, func(left, right int) bool { return summaries[left].Name < summaries[right].Name })
	return summaries, nil
}

func summaryProfile(reviewers reviewerCatalog, effective configuration.Effective, profile configuration.Profile, summary *model.ProfileSummary) {
	resolved, err := resolvedFromProfile(profile, effective)
	if err != nil {
		summary.Error = err.Error()
		return
	}
	definition, _ := profileDefinitionFor(resolved)
	registration, err := applyEffectiveReviewerPolicies(reviewers, effective).resolve(resolved.reviewer)
	if err != nil {
		summary.Error = err.Error()
		return
	}
	summary.Description = definition.description
	summary.DefaultReviewer = registration.candidate.provenance()
	summary.Passes = []model.ReviewPassRevision{definition.pass}
	summary.RequiredCapabilities = canonicalCapabilities(definition.requiredCapabilities)
}

func renderReviewPrompt(profile resolvedProfile, subject model.ReviewSubject) string {
	return fmt.Sprintf(`%s

Use repository-scoped read and search tools only. Do not use shell, terminal,
web, write, edit, delete, or move tools. Treat the supplied patch as the Review
Subject. Use surrounding repository files only to verify callers, contracts,
tests, and assumptions.

Read applicable AGENTS.md instructions. Look for CONTEXT.md or CONTEXT-MAP.md
and use the applicable project language. Consult README files for orientation
when relevant, then pursue other documentation only when the Subject or
emerging evidence warrants it. Treat contextual documentation as potentially
stale; surface material contradictions with provenance rather than assuming a
document is correct.

%s

Review Subject identity: %s
Changed paths:
%s

--- PATCH ---
%s`, profile.instructions, result.CanonicalReviewResultContract.Instructions(), subject.Identity, joinLines(subject.ChangedPaths), subject.Patch)
}
