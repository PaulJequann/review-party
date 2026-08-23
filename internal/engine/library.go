package engine

import (
	"embed"
	"fmt"
	"strings"

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

type profileRequest struct {
	repository string
	name       string
	reviewer   string
}

type resolvedProfile struct {
	name         string
	instructions string
	source       string
	path         string
	digest       string
	reviewer     string
}

type profileCandidate struct {
	source string
	path   string
	anchor string
}

// manager returns the owning Configuration Manager, constructing a default
// one for zero-value libraries used by focused tests.
func (library profileLibrary) manager() *configuration.Manager {
	if library.configuration != nil {
		return library.configuration
	}
	return newProfileLibrary("").configuration
}

func newProfileLibrary(personalRoot string) profileLibrary {
	return profileLibrary{configuration: configuration.NewManager(configuration.Options{
		PersonalRoot:            personalRoot,
		Reviewers:               supportedReviewerIDs(),
		PackagedDefaultReviewer: defaultReviewer,
		PackagedDefaultProfile:  packagedDefaultProfileName,
		ValidateProfileName:     validateProfileName,
	})}
}

func (conductor *Conductor) compileFilesystemProfile(selection ProfileSelection, repository string) (compiledProfile, error) {
	reviewerWasDefault := selection.Reviewer == ""
	resolved, err := conductor.profiles.resolve(conductor.reviewers, profileRequest{repository: repository, name: selection.Profile, reviewer: selection.Reviewer})
	if err != nil {
		return compiledProfile{}, err
	}
	selection.Profile = resolved.name
	if selection.Reviewer == "" {
		selection.Reviewer = resolved.reviewer
	}
	definition, err := profileDefinitionFor(resolved)
	if err != nil {
		return compiledProfile{}, err
	}
	profile, err := compileProfileDefinition(conductor.reviewers, selection, conductor.attemptDeadline, definition)
	if err != nil {
		return compiledProfile{}, err
	}
	profile.revision.Source = resolved.source
	profile.revision.SourceDigest = resolved.digest
	profile.revision.CompilerRevision = profileCompilerRevision
	profile.revision.Revision = profileRevisionIdentity(profile.revision)
	profile.snapshot = ProfileSnapshot{Name: resolved.name, Source: resolved.source, SourceDigest: resolved.digest, Instructions: resolved.instructions}
	profile.reviewerWasDefault = reviewerWasDefault
	profile.buildPrompt = func(subject ReviewSubject) string { return renderReviewPrompt(resolved, subject) }
	return profile, nil
}

func profileDefinitionFor(profile resolvedProfile) (profileDefinition, error) {
	if strings.HasPrefix(profile.source, "packaged:") {
		definition, err := findProfileDefinition(profile.name)
		if err == nil {
			return definition, nil
		}
	}
	return profileDefinition{
		name:                 profile.name,
		description:          "User-defined review Profile",
		purpose:              "Apply the authored review instructions to the Review Subject.",
		materialityThreshold: "A concrete actionable issue under the authored Profile instructions.",
		defaultReviewer:      defaultReviewer,
		pass:                 ReviewPassRevision{Name: filesystemPassName(profile.name), Required: true, Purpose: "Apply the authored Profile.", PromptRevision: profileCompilerRevision + ":" + profile.digest},
		requiredCapabilities: restrictedReviewCapabilities(),
	}, nil
}

func filesystemPassName(profileName string) string {
	if profileName == "bugs" {
		return "bug-review"
	}
	return profileName + "-review"
}

func (conductor *Conductor) profileSummaries(repository string) ([]ProfileSummary, error) {
	summaries, err := conductor.profiles.list(repository)
	if err != nil {
		return nil, err
	}
	for index := range summaries {
		if summaries[index].Error != "" {
			continue
		}
		resolved, resolveErr := conductor.profiles.resolve(conductor.reviewers, profileRequest{repository: repository, name: summaries[index].Name})
		if resolveErr != nil {
			summaries[index].Error = resolveErr.Error()
			continue
		}
		definition, _ := profileDefinitionFor(resolved)
		registration, resolveErr := conductor.reviewers.resolve(resolved.reviewer)
		if resolveErr != nil {
			summaries[index].Error = resolveErr.Error()
			continue
		}
		summaries[index].Description = definition.description
		summaries[index].DefaultReviewer = registration.candidate.provenance()
		summaries[index].Passes = []ReviewPassRevision{definition.pass}
		summaries[index].RequiredCapabilities = canonicalCapabilities(definition.requiredCapabilities)
	}
	return summaries, nil
}

func (library profileLibrary) resolve(catalog reviewerCatalog, request profileRequest) (resolvedProfile, error) {
	loaded, err := library.manager().Load(configuration.Repository(request.repository))
	if err != nil {
		return resolvedProfile{}, err
	}
	selection, err := selectProfileFromScopes(catalog, profileSelection{name: request.name, reviewer: request.reviewer}, loaded.Personal.Document, loaded.Repository.Document)
	if err != nil {
		return resolvedProfile{}, err
	}
	profile, err := library.findProfile(profileLookup{repository: request.repository, name: selection.name})
	if err != nil {
		return resolvedProfile{}, err
	}
	profile.reviewer = selection.reviewer
	return profile, nil
}

func renderReviewPrompt(profile resolvedProfile, subject ReviewSubject) string {
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
%s`, profile.instructions, canonicalReviewResultContract.Instructions(), subject.Identity, joinLines(subject.ChangedPaths), subject.Patch)
}
