package reviewparty

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type compiledProfile struct {
	revision  ProfileRevision
	candidate reviewerCandidate
	prompt    string
}

const defaultReviewer = "grok"

func compileProfile(name, reviewer string, subject ReviewSubject) (compiledProfile, error) {
	if name == "" {
		name = "bugs"
	}
	if name != "bugs" {
		return compiledProfile{}, fmt.Errorf("unknown review profile %q", name)
	}

	if reviewer == "" {
		reviewer = defaultReviewer
	}
	candidate, err := reviewerCandidateFor(reviewer)
	if err != nil {
		return compiledProfile{}, err
	}
	prompt := buildBugReviewPrompt(subject)
	hash := sha256.Sum256([]byte(name + "\x00" + candidate.ID + "\x00" + candidate.Model + "\x00" + candidate.Effort + "\x00" + candidate.Harness + "\x00" + candidate.Transport + "\x00" + promptTemplateVersion))

	return compiledProfile{
		revision: ProfileRevision{
			Name:       name,
			Revision:   hex.EncodeToString(hash[:]),
			ReviewerID: candidate.ID,
			Model:      candidate.Model,
			Effort:     candidate.Effort,
		},
		candidate: candidate,
		prompt:    prompt,
	}, nil
}

func reviewerCandidateFor(reviewer string) (reviewerCandidate, error) {
	switch reviewer {
	case "grok":
		return reviewerCandidate{ID: "grok", Model: "grok-4.5", Effort: "high", Harness: "grok-build-cli", Transport: "direct-cli"}, nil
	case "opencode":
		return reviewerCandidate{ID: "opencode", Model: "zai-coding-plan/glm-5.2", Effort: "default", Harness: "opencode-cli", Transport: "direct-cli"}, nil
	case "copilot":
		return reviewerCandidate{ID: "copilot", Model: "auto", Effort: "auto", Harness: "github-copilot-cli", Transport: "direct-cli"}, nil
	default:
		return reviewerCandidate{}, fmt.Errorf("unknown reviewer %q; expected grok, opencode, or copilot", reviewer)
	}
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

Return exactly one block and no text before or after it.

For no actionable findings:
BEGIN_REVIEW
status: clean
summary: No actionable findings.
END_REVIEW

For findings, return at most eight MEDIUM, HIGH, or CRITICAL items:
BEGIN_REVIEW
status: findings

1. HIGH | correctness | path/to/file.go:123
Failure: Concrete supported scenario that fails.
Evidence: Why the Subject permits the failure.
Fix: Smallest safe correction.
Test: Regression that fails before the correction.
END_REVIEW

Review Subject identity: %s
Changed paths:
%s

--- PATCH ---
%s`, subject.Identity, joinLines(subject.ChangedPaths), subject.Patch)
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
