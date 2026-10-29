package configurationhub

import (
	"fmt"
	"strings"

	"reviewparty/internal/configuration"
)

// The terminal confirmation is organized for a decision, rather than using
// the CLI's compact, line-oriented diff. Values come from the validated Plan;
// the complete creation draft supplies the individual Profile settings.
func renderPlanConfirmation(plan configuration.Plan, draft configuration.ProfileDraft) string {
	var output strings.Builder
	for index, change := range plan.Changes() {
		if index > 0 {
			output.WriteString("\n\n")
		}
		action := "Update"
		if !change.HadBefore {
			action = "Create"
		} else if !change.HadAfter {
			action = "Remove"
		}
		name := change.Field
		if strings.HasPrefix(name, "profiles.") {
			name = fmt.Sprintf("profile %q", strings.TrimPrefix(name, "profiles."))
		}
		fmt.Fprintf(&output, "%s %s\n\n", action, name)
		fmt.Fprintf(&output, "  Scope      %s\n", configurationTitle(change.Scope))
		if !change.HadBefore && change.HadAfter && change.Field == "profiles."+draft.Name {
			fmt.Fprintf(&output, "  Reviewer   %s\n  Model      %s\n  Effort     %s\n  Deadline   %s\n",
				draft.Reviewer, draft.Model, draft.ReasoningEffort, draft.AttemptDeadline)
			if draft.TemplateID != "" {
				fmt.Fprintf(&output, "  Template   %s\n", draft.TemplateID)
				revision := draft.TemplateRevision
				if revision != "" {
					fmt.Fprintf(&output, "  Revision   %s\n", revision)
				}
			} else {
				output.WriteString("  Instructions   Custom\n")
			}
		} else {
			if change.HadBefore {
				fmt.Fprintf(&output, "\nBefore\n  %s\n", change.Before)
			}
			if change.HadAfter {
				fmt.Fprintf(&output, "\nAfter\n  %s\n", change.After)
			}
		}
		fmt.Fprintf(&output, "\nSave location\n  %s", change.Path)
	}
	if len(plan.Changes()) == 0 {
		output.WriteString("No configuration changes.")
	}
	output.WriteString("\n\nNothing has been saved yet. Publish to apply these changes.\n")
	return output.String()
}
