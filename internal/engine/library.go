package engine

import (
	"embed"
	"errors"
	"fmt"
	"path"
	"strings"

	"reviewparty/internal/configuration"
)

const (
	profileConfigSchema     = 1
	profileCompilerRevision = "markdown-v1"
)

//go:embed profiles/*.md
var packagedProfileFiles embed.FS

type profileLocation struct {
	name   string
	path   string
	source string
}

func profileLocationFrom(entry configuration.AuthoredEntry) profileLocation {
	return profileLocation{name: entry.Name, path: entry.Path, source: entry.Source}
}

func packagedProfileEntries() ([]profileLocation, error) {
	entries, err := packagedProfileFiles.ReadDir("profiles")
	if err != nil {
		return nil, fmt.Errorf("read packaged profile directory: %w", err)
	}
	profiles := make([]profileLocation, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".md" {
			continue
		}
		profilePath := path.Join("profiles", entry.Name())
		profiles = append(profiles, profileLocation{
			name:   strings.TrimSuffix(entry.Name(), ".md"),
			path:   profilePath,
			source: "packaged:" + profilePath,
		})
	}
	return profiles, nil
}

type profileLibrary struct {
	configuration *configuration.Manager
}

var errProfileLibraryNotConfigured = errors.New("profile library requires an explicit configuration manager")

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
	effective    configuration.Effective
}

// manager returns the Configuration Manager that owns this Profile library.
func (library profileLibrary) manager() *configuration.Manager {
	return library.configuration
}

func (library profileLibrary) authoredProfileLibrary(repository configuration.Repository) (configuration.AuthoredLibrary, error) {
	manager := library.manager()
	if manager == nil {
		return configuration.AuthoredLibrary{}, errProfileLibraryNotConfigured
	}
	return manager.AuthoredLibrary(configuration.LibraryProfiles, repository)
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
	resolved, err := conductor.profiles.resolve(profileRequest{repository: repository, name: selection.Profile, reviewer: selection.Reviewer})
	if err != nil {
		return compiledProfile{}, err
	}
	return conductor.compileProfile(selection, resolved)
}

func (conductor *Conductor) compileProfile(selection ProfileSelection, resolved resolvedProfile) (compiledProfile, error) {
	reviewers, err := applyEffectiveReviewerPolicies(conductor.reviewers, resolved.effective)
	if err != nil {
		return compiledProfile{}, err
	}
	reviewerWasDefault := selection.Reviewer == ""
	selection.Profile = resolved.name
	if selection.Reviewer == "" {
		selection.Reviewer = resolved.reviewer
	}
	definition, err := profileDefinitionFor(resolved)
	if err != nil {
		return compiledProfile{}, err
	}
	profile, err := compileProfileDefinition(reviewers, selection, conductor.attemptDeadline, definition)
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
		resolved, resolveErr := conductor.profiles.resolve(profileRequest{repository: repository, name: summaries[index].Name})
		if resolveErr != nil {
			summaries[index].Error = resolveErr.Error()
			continue
		}
		reviewers, resolveErr := applyEffectiveReviewerPolicies(conductor.reviewers, resolved.effective)
		if resolveErr != nil {
			summaries[index].Error = resolveErr.Error()
			continue
		}
		definition, _ := profileDefinitionFor(resolved)
		registration, resolveErr := reviewers.resolve(resolved.reviewer)
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

func (library profileLibrary) resolveEffective(request profileRequest) (configuration.Effective, error) {
	return library.manager().Resolve(configuration.Request{
		Repository: configuration.Repository(request.repository),
		Overrides:  configuration.Overrides{Profile: request.name},
	})
}

func (library profileLibrary) resolve(request profileRequest) (resolvedProfile, error) {
	effective, err := library.resolveEffective(request)
	if err != nil {
		return resolvedProfile{}, err
	}
	return library.resolveWithEffective(effective, request)
}

func (library profileLibrary) resolveWithEffective(effective configuration.Effective, request profileRequest) (resolvedProfile, error) {
	selection, err := selectProfileFromEffective(effective, request.reviewer)
	if err != nil {
		return resolvedProfile{}, err
	}
	profile, err := library.findProfile(profileLookup{repository: request.repository, name: selection.name})
	if err != nil {
		return resolvedProfile{}, err
	}
	profile.reviewer = selection.reviewer
	profile.effective = effective
	return profile, nil
}

func (library profileLibrary) validateExplicitReviewer(selection ProfileSelection, repository string, catalog reviewerCatalog) error {
	request := profileRequest{repository: repository, name: selection.Profile, reviewer: selection.Reviewer}
	effective, err := library.resolveEffective(request)
	if err != nil {
		return err
	}
	reviewers, err := applyEffectiveReviewerPolicies(catalog, effective)
	if err != nil {
		return err
	}
	required := restrictedReviewCapabilities()
	profileName := firstNonempty(selection.Profile, effective.DefaultProfile.Value, "bugs")
	if resolved, resolveErr := library.resolveWithEffective(effective, request); resolveErr == nil {
		if definition, definitionErr := profileDefinitionFor(resolved); definitionErr == nil {
			required = definition.requiredCapabilities
			profileName = definition.name
		}
	}
	registration, err := reviewers.resolve(selection.Reviewer)
	if err != nil {
		return err
	}
	_, err = validateReviewerSelection(registration, selection, required, profileName)
	return err
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
