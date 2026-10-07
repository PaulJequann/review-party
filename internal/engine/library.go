package engine

import (
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
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

var errConfigurationNotConfigured = errors.New("configuration manager is not configured")

type resolvedProfile struct {
	name         string
	instructions string
	source       string
	digest       string
	reviewer     string
	model        string
	effort       string
	deadline     time.Duration
	effective    configuration.Effective
}

func (conductor *Conductor) compileProfile(request profileCompileRequest) (compiledProfile, error) {
	profile := request.profile
	selection := request.selection
	selection.Profile = profile.Name
	if selection.Reviewer == "" {
		selection.Reviewer = profile.Reviewer
	}
	if selection.Model == "" {
		selection.Model = profile.Model
	}
	if selection.Effort == "" {
		selection.Effort = profile.ReasoningEffort
	}
	definition, err := profileDefinitionFor(profile)
	if err != nil {
		return compiledProfile{}, err
	}
	compiled, err := compileProfileDefinition(applyEffectiveReviewerPolicies(conductor.reviewers, request.effective), selection, request.deadline, definition)
	if err != nil {
		return compiledProfile{}, err
	}
	compiled.revision.AttemptLimit = request.attemptLimit
	if compiled.revision.AttemptLimit == 0 {
		compiled.revision.AttemptLimit = 1
	}
	compiled.revision.Source = profile.Source
	compiled.revision.SourceDigest = profile.SourceDigest
	compiled.revision.CompilerRevision = profileCompilerRevision
	compiled.revision.Revision = profileRevisionIdentity(compiled.revision)
	compiled.snapshot = model.ProfileSnapshot{Name: profile.Name, Source: profile.Source, SourceDigest: profile.SourceDigest, Instructions: profile.Instructions}
	compiled.reviewerWasDefault = request.selection.Reviewer == ""
	compiled.buildPrompt = func(subject model.ReviewSubject) string { return renderReviewPrompt(compiled.snapshot, subject) }
	return compiled, nil
}

func profileDefinitionFor(profile configuration.Profile) (profileDefinition, error) {
	return profileDefinition{
		name:                 profile.Name,
		description:          "User-defined review Profile",
		purpose:              "Apply the authored review instructions to the Review Subject.",
		materialityThreshold: "A concrete actionable issue under the authored Profile instructions.",
		pass:                 model.ReviewPassRevision{Name: filesystemPassName(profile.Name), Required: true, Purpose: "Apply the authored Profile.", PromptRevision: profileCompilerRevision + ":" + profile.SourceDigest},
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
	inventory, err := conductor.configuration.ProfileInventory(configuration.Repository(repository))
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
			summaryProfile(conductor, effective, definition.Value, &summary)
		}
		summaries = append(summaries, summary)
	}
	sort.SliceStable(summaries, func(left, right int) bool { return summaries[left].Name < summaries[right].Name })
	return summaries, nil
}

func summaryProfile(conductor *Conductor, effective configuration.Effective, profile configuration.Profile, summary *model.ProfileSummary) {
	deadline, err := time.ParseDuration(profile.AttemptDeadline)
	if err != nil {
		summary.Error = err.Error()
		return
	}
	compiled, err := conductor.compileProfile(profileCompileRequest{profile: profile, effective: effective, deadline: deadline})
	if err != nil {
		summary.Error = err.Error()
		return
	}
	summary.Description = compiled.revision.Description
	summary.DefaultReviewer = compiled.revision.Reviewer
	summary.Passes = compiled.revision.Passes
	summary.RequiredCapabilities = compiled.revision.RequiredCapabilities
}

const reviewToolRules = `Use repository-scoped read and search tools only. Do not use shell, terminal,
web, write, edit, delete, or move tools. Treat the supplied patch as the Review
Subject. Use surrounding repository files only to verify callers, contracts,
tests, and assumptions.

Read applicable AGENTS.md instructions. Look for CONTEXT.md or CONTEXT-MAP.md
and use the applicable project language. Consult README files for orientation
when relevant, then pursue other documentation only when the Subject or
emerging evidence warrants it. Treat contextual documentation as potentially
stale; surface material contradictions with provenance rather than assuming a
document is correct.`

const deltaFraming = `This Profile reviewed earlier states of these paths. This Review Subject
includes the content written since, after those Reviews' Findings. Code
written to address an earlier Finding is a claim to verify, not evidence that
the Finding is resolved. Flag a fix that over-builds beyond what the Finding
needed, and a fix that treats the symptom and misses the root cause.

Prior Findings on the changed paths, from this Profile's earlier Reviews:`

func renderReviewPrompt(profile model.ProfileSnapshot, subject model.ReviewSubject) string {
	return renderPrompt(profile, subject, "")
}

// renderDeltaPrompt frames a Review that follows earlier Reviews of the same
// Profile with their Findings on the paths it touches, so a fix is reviewed as
// a claim.
func renderDeltaPrompt(profile model.ProfileSnapshot, subject model.ReviewSubject, prior []priorFinding) string {
	lines := make([]string, 0, len(prior))
	for _, entry := range prior {
		lines = append(lines, fmt.Sprintf("- %s #%d %s %s: %s", entry.review, entry.finding.Ordinal, entry.finding.Severity, entry.finding.Location, entry.finding.Failure))
	}
	return renderPrompt(profile, subject, deltaFraming+"\n"+joinLines(lines))
}

func renderPrompt(profile model.ProfileSnapshot, subject model.ReviewSubject, framing string) string {
	sections := []string{profile.Instructions, reviewToolRules, result.CanonicalReviewResultContract.Instructions()}
	if framing != "" {
		sections = append(sections, framing)
	}
	sections = append(sections, fmt.Sprintf("Review Subject identity: %s\nChanged paths:\n%s\n\n--- PATCH ---\n%s", subject.Identity, joinLines(subject.ChangedPaths), subject.Patch))
	return strings.Join(sections, "\n\n")
}
