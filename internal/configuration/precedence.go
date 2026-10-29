package configuration

import (
	"fmt"
	"strings"
)

// precedenceWarnings states what a plan changes about which file supplies the
// Review selection and the Checkpoints in force for the repository, which the
// edited fields alone do not show.
func precedenceWarnings(before, after Loaded) []string {
	var warnings []string
	for _, warning := range []string{reviewSelectionPrecedence(before, after), checkpointPrecedence(before, after)} {
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	return warnings
}

func reviewSelectionPrecedence(before, after Loaded) string {
	was, now := before.ReviewSelection(), after.ReviewSelection()
	switch {
	case globalFieldChanged(before, after, readReviewSelection) && now.Source == SourceRepository:
		return "Repository Configuration replaces the Global Review selection here, so this change to it applies only to repositories without their own."
	case was.Source == SourceGlobal && now.Source == SourceRepository:
		return fmt.Sprintf("The Review selection in force here moves from Global Configuration (%s) to Repository Configuration (%s), which replaces the Global one as a whole; later edits to the Global selection no longer reach this repository.", selectionSize(was.Value), selectionSize(now.Value))
	case was.Source == SourceRepository && now.Source == SourceGlobal:
		return fmt.Sprintf("The Review selection in force here moves from Repository Configuration (%s) to Global Configuration (%s); edits to the Global selection now reach this repository.", selectionSize(was.Value), selectionSize(now.Value))
	case was.Authored && !now.Authored:
		return "Neither Repository nor Global Configuration now declares a Review selection, so none is in force here."
	}
	return ""
}

func checkpointPrecedence(before, after Loaded) string {
	was, now := before.Checkpoints(), after.Checkpoints()
	if globalFieldChanged(before, after, readCheckpoints) {
		if now.Source == SourceRepository {
			return "Repository Configuration replaces the Global Checkpoints here, so this change to them applies only to enrolled repositories without their own."
		}
		return ""
	}
	wasInForce, nowInForce := globalCheckpointsInForce(was), globalCheckpointsInForce(now)
	switch {
	case nowInForce && !wasInForce && !before.Repository.Present:
		subject, verb := globalCheckpointsPhrase(now.Value)
		return fmt.Sprintf("Creating Repository Configuration enrolls this repository, so %s now %s here.", subject, verb)
	case nowInForce && !wasInForce:
		subject, verb := globalCheckpointsPhrase(now.Value)
		return fmt.Sprintf("Repository Configuration no longer declares Checkpoints of its own, so %s now %s here.", subject, verb)
	case wasInForce && !nowInForce && len(now.Value) == 0:
		subject, verb := globalCheckpointsPhrase(was.Value)
		return fmt.Sprintf("Repository Configuration declares no Checkpoints, so %s no longer %s here.", subject, verb)
	case wasInForce && !nowInForce:
		subject, _ := globalCheckpointsPhrase(was.Value)
		return fmt.Sprintf("Repository Configuration now declares its own Checkpoints, which replace %s here.", subject)
	}
	return ""
}

func globalFieldChanged(before, after Loaded, read func(Document) (string, bool)) bool {
	was, wasAuthored := read(before.Global.Document)
	now, nowAuthored := read(after.Global.Document)
	return was != now || wasAuthored != nowAuthored
}

func globalCheckpointsInForce(checkpoints Value[map[CheckpointName]Checkpoint]) bool {
	return checkpoints.Source == SourceGlobal && len(checkpoints.Value) > 0
}

func globalCheckpointsPhrase(checkpoints map[CheckpointName]Checkpoint) (subject, verb string) {
	names := SortedCheckpointNames(checkpoints)
	if len(names) == 1 {
		return "the Global " + string(names[0]) + " Checkpoint", "applies"
	}
	listed := make([]string, len(names))
	for index, name := range names {
		listed[index] = string(name)
	}
	last := len(listed) - 1
	return "the Global " + strings.Join(listed[:last], ", ") + " and " + listed[last] + " Checkpoints", "apply"
}

func selectionSize(selection ReviewSelection) string {
	if count := len(selection.Global) + len(selection.Repository); count != 1 {
		return fmt.Sprintf("%d items", count)
	}
	return "1 item"
}
