package main

import (
	"reviewparty/internal/model"
)

func writeHumanBundle(output *commandOutput, bundle bundleSummary, entries []reviewEntry, configuration string) {
	output.write("bundle %s · %s · %d/%d review(s) completed\n", bundle.ID, bundle.Lifecycle, completedEntries(entries), len(entries))
	for _, entry := range entries {
		label := entry.Profile.Scope + ":" + entry.Profile.Name
		if entry.ID != "" {
			label += " · " + string(entry.ID)
		}
		output.write("\n")
		writeHumanEntry(output, label, entry)
	}
	output.write("\n")
	if partyName := explicitPartyName(bundle.Selection); partyName != "" {
		output.write("party: %s\n", partyName)
	}
	output.write("revision: %s\n", bundle.Revision)
	writeBundleSelection(output, bundle.Selection)
	for _, warning := range bundle.Warnings {
		output.write("warning: %s\n", warning.Message)
	}
	for _, duplicate := range bundle.Deduplicated {
		output.write("deduplicated: %s:%s selected again by %s; first run kept at %s\n", duplicate.Scope, duplicate.Profile, duplicate.Origin, duplicate.KeptOrigin)
	}
	output.write("subject: %s %s\n", bundle.SubjectKind, shortIdentity(bundle.SubjectIdentity))
	if bundle.Termination != nil {
		output.write("incomplete: %s: %s\n", bundle.Termination.Category, bundle.Termination.Message)
	}
	writeInspectHint(output, string(bundle.ID), configuration)
}

func writeBundleSelection(output *commandOutput, selection *model.BundleSelection) {
	if selection == nil || selection.Source == "" {
		return
	}
	output.write("selection: %s", selection.Kind)
	if selection.LimitSource != "" {
		output.write(" · limit %d (%s)", selection.ConcurrencyLimit, selection.LimitSource)
	}
	output.write("\n")
}

func explicitPartyName(selection *model.BundleSelection) string {
	if selection == nil || selection.Kind != "explicit_party" {
		return ""
	}
	if len(selection.Authored) == 0 {
		return ""
	}
	return selection.Authored[0].Name
}

func completedEntries(entries []reviewEntry) int {
	completed := 0
	for _, entry := range entries {
		if entry.Lifecycle == model.LifecycleCompleted {
			completed++
		}
	}
	return completed
}
