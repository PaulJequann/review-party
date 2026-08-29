package configuration

import (
	"fmt"
	"io"
)

// RenderPlanHuman writes the canonical human preview for a Configuration Plan.
func RenderPlanHuman(output io.Writer, plan Plan) error {
	if _, err := fmt.Fprintln(output, "configuration plan:"); err != nil {
		return err
	}
	for _, change := range plan.Changes() {
		before, after := "<absent>", "<absent>"
		if change.HadBefore {
			before = change.Before
		}
		if change.HadAfter {
			after = change.After
		}
		if _, err := fmt.Fprintf(output, "  %s %s %q: %s -> %s\n", change.Scope, change.Field, change.Path, before, after); err != nil {
			return err
		}
	}
	return nil
}

// RenderPlanWarningsHuman writes canonical human warning lines.
func RenderPlanWarningsHuman(output io.Writer, warnings []string) error {
	for _, warning := range warnings {
		if _, err := fmt.Fprintf(output, "warning: %s\n", warning); err != nil {
			return err
		}
	}
	return nil
}

// RenderResolvedReviewsHuman writes the canonical human preview for resolved
// review selection.
func RenderResolvedReviewsHuman(output io.Writer, resolved ResolvedReviews) error {
	if _, err := fmt.Fprintln(output, "resolved review selection:"); err != nil {
		return err
	}
	for _, line := range RenderResolvedReviewLinesHuman(resolved) {
		if _, err := fmt.Fprintln(output, line); err != nil {
			return err
		}
	}
	for _, warning := range resolved.Warnings {
		if _, err := fmt.Fprintf(output, "  warning: %s\n", warning.Message); err != nil {
			return err
		}
	}
	return nil
}

// RenderResolvedReviewLinesHuman returns the canonical line-oriented preview
// for expanded and deduplicated review entries. Warnings remain structured so
// callers such as the Configuration Hub can place them in their warning area.
func RenderResolvedReviewLinesHuman(resolved ResolvedReviews) []string {
	lines := make([]string, 0, len(resolved.Expanded)+len(resolved.Deduplicated))
	for index, profile := range resolved.Expanded {
		lines = append(lines, fmt.Sprintf("  %d. [%s] %s (%s)", index+1, profile.Scope, profile.Profile, profile.Origin))
	}
	for _, skipped := range resolved.Deduplicated {
		line := fmt.Sprintf("  deduplicated [%s] %s from %s", skipped.Scope, skipped.Profile, skipped.Origin)
		if skipped.KeptOrigin != skipped.Origin {
			line += fmt.Sprintf(" (kept by %s)", skipped.KeptOrigin)
		}
		lines = append(lines, line)
	}
	return lines
}
