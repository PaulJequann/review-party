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
