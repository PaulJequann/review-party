package reviewparty

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	profileConfigSchema     = 1
	profileCompilerRevision = "markdown-v1"
)

//go:embed profiles/*.md
var packagedProfileFiles embed.FS

type profileLibrary struct {
	globalDirectory string
}

// ProfileCatalog provides read-only access to resolved review Profiles without
// initializing review-record persistence.
type ProfileCatalog struct {
	library   profileLibrary
	reviewers reviewerCatalog
}

type ProfileExplanationRequest struct {
	Repository string
	Name       string
	Reviewer   string
}

type profileRequest struct {
	repository string
	name       string
	reviewer   string
}

func NewProfileCatalog(globalDirectory string) *ProfileCatalog {
	return &ProfileCatalog{library: newProfileLibrary(globalDirectory), reviewers: defaultReviewerCatalog()}
}

func (catalog *ProfileCatalog) Profiles(repository string) ([]ProfileSummary, error) {
	root, err := resolveRepositoryRoot(repository)
	if err != nil {
		return nil, err
	}
	return catalog.library.list(root)
}

func (catalog *ProfileCatalog) Explain(request ProfileExplanationRequest) (ProfileExplanation, error) {
	root, err := resolveRepositoryRoot(request.Repository)
	if err != nil {
		return ProfileExplanation{}, err
	}
	profile, err := catalog.library.compile(catalog.reviewers, profileRequest{name: request.Name, reviewer: request.Reviewer}, ReviewSubject{Repository: root})
	if err != nil {
		return ProfileExplanation{}, err
	}
	passes := make([]ProfilePassSummary, 0, len(profile.passes))
	for _, pass := range profile.passes {
		passes = append(passes, ProfilePassSummary{Name: pass.name, Required: pass.required})
	}
	return ProfileExplanation{Revision: profile.revision, Instructions: profile.snapshot.Instructions, Passes: passes}, nil
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

func newProfileLibrary(globalDirectory string) profileLibrary {
	if globalDirectory == "" {
		globalDirectory = defaultGlobalProfileDirectory()
	}
	return profileLibrary{globalDirectory: globalDirectory}
}

func (library profileLibrary) compile(catalog reviewerCatalog, request profileRequest, subject ReviewSubject) (compiledProfile, error) {
	request.repository = subject.Repository
	resolved, err := library.resolve(catalog, request)
	if err != nil {
		return compiledProfile{}, err
	}
	registration, err := catalog.resolve(resolved.reviewer)
	if err != nil {
		return compiledProfile{}, err
	}

	passName := resolved.name + "-review"
	if resolved.name == "bugs" {
		passName = "bug-review"
	}
	passes := []passPlan{{name: passName, required: true}}
	candidate := registration.candidate
	revisionInput := strings.Join([]string{
		resolved.name,
		resolved.instructions,
		candidate.ID,
		candidate.Model,
		candidate.Effort,
		candidate.Harness,
		candidate.Transport,
		profileCompilerRevision,
		canonicalReviewResultContract.revision(),
	}, "\x00")
	for _, pass := range passes {
		revisionInput += fmt.Sprintf("\x00%s\x00%t", pass.name, pass.required)
	}
	revisionHash := sha256.Sum256([]byte(revisionInput))

	return compiledProfile{
		revision: ProfileRevision{
			Name:             resolved.name,
			Revision:         hex.EncodeToString(revisionHash[:]),
			ReviewerID:       candidate.ID,
			Model:            candidate.Model,
			Effort:           candidate.Effort,
			Source:           resolved.source,
			SourceDigest:     resolved.digest,
			CompilerRevision: profileCompilerRevision,
		},
		snapshot: ProfileSnapshot{
			Name:         resolved.name,
			Source:       resolved.source,
			SourceDigest: resolved.digest,
			Instructions: resolved.instructions,
		},
		reviewer: registration,
		prompt:   renderReviewPrompt(resolved, subject),
		passes:   passes,
	}, nil
}

func (library profileLibrary) resolve(catalog reviewerCatalog, request profileRequest) (resolvedProfile, error) {
	settings, err := library.loadSettings(request.repository)
	if err != nil {
		return resolvedProfile{}, err
	}
	selection, err := settings.selectProfile(catalog, profileSelection{name: request.name, reviewer: request.reviewer})
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
%s`, profile.instructions, canonicalReviewResultContract.instructions(), subject.Identity, joinLines(subject.ChangedPaths), subject.Patch)
}
