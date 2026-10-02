package configuration

import (
	"errors"
	"slices"
)

// DocumentationTemplateID is the packaged Template a documentation Profile is
// created from. Its ID, not a Profile's name, marks a documentation Review.
const DocumentationTemplateID = "documentation"

// MarkdownExemptions returns the exempt_paths patterns that exempt Markdown
// files, judged by the README files every documentation layout has.
func (checkpoint Checkpoint) MarkdownExemptions() []string {
	var patterns []string
	for _, pattern := range checkpoint.ExemptPaths {
		if matchPathPattern(pattern, "README.md") || matchPathPattern(pattern, "docs/README.md") {
			patterns = append(patterns, pattern)
		}
	}
	return patterns
}

// SelectedDocumentationProfiles names the Profiles in the repository's
// effective selection that were created from the documentation Template. A
// repository with no selection has none.
func (manager *Manager) SelectedDocumentationProfiles(repository Repository) ([]string, error) {
	resolved, err := manager.ResolveRun(RunRequest{Repository: repository})
	if errors.Is(err, ErrNoRepositorySelection) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	inventory, err := manager.ProfileInventory(repository)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, definition := range inventory {
		selected := slices.ContainsFunc(resolved.Expanded, func(profile ExpandedProfile) bool {
			return profile.Scope == definition.Scope && profile.Profile == definition.Name
		})
		if selected && definition.Value.TemplateID == DocumentationTemplateID {
			names = append(names, definition.Name)
		}
	}
	return names, nil
}
