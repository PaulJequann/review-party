package reviewparty

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type compiledProfile struct {
	revision ProfileRevision
	reviewer reviewerRegistration
	prompt   string
	passes   []passPlan
}

type passPlan struct {
	name     string
	required bool
}

func compileProfile(catalog reviewerCatalog, name, reviewer string, subject ReviewSubject) (compiledProfile, error) {
	if name == "" {
		name = "bugs"
	}
	if name != "bugs" {
		return compiledProfile{}, fmt.Errorf("unknown review profile %q", name)
	}

	if reviewer == "" {
		reviewer = defaultReviewer
	}
	registration, err := catalog.resolve(reviewer)
	if err != nil {
		return compiledProfile{}, err
	}
	candidate := registration.candidate
	passes := []passPlan{{name: "bug-review", required: true}}
	prompt := buildBugReviewPrompt(subject)
	revisionInput := name + "\x00" + candidate.ID + "\x00" + candidate.Model + "\x00" + candidate.Effort + "\x00" + candidate.Harness + "\x00" + candidate.Transport + "\x00" + promptTemplateVersion + "\x00" + canonicalReviewResultContract.revision()
	for _, pass := range passes {
		revisionInput += fmt.Sprintf("\x00%s\x00%t", pass.name, pass.required)
	}
	hash := sha256.Sum256([]byte(revisionInput))

	return compiledProfile{
		revision: ProfileRevision{
			Name:       name,
			Revision:   hex.EncodeToString(hash[:]),
			ReviewerID: candidate.ID,
			Model:      candidate.Model,
			Effort:     candidate.Effort,
		},
		reviewer: registration,
		prompt:   prompt,
		passes:   passes,
	}, nil
}

const promptTemplateVersion = "bugs-v3"

func buildBugReviewPrompt(subject ReviewSubject) string {
	return fmt.Sprintf(`Act as a senior code reviewer. Be terse and skip praise.

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

Review for material bugs: concrete regressions in correctness, security,
privacy, data integrity, concurrency, failure handling, public contracts,
operability, test validity, and applicable Project Rules. Report an unresolved
approval requirement precisely; absence of approval evidence is not proof that
approval was denied.

%s

Review Subject identity: %s
Changed paths:
%s

--- PATCH ---
%s`, canonicalReviewResultContract.instructions(), subject.Identity, joinLines(subject.ChangedPaths), subject.Patch)
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return "(none)"
	}
	result := lines[0]
	for _, line := range lines[1:] {
		result += "\n" + line
	}
	return result
}
