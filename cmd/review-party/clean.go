package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"

	"github.com/spf13/cobra"

	"reviewparty/internal/hostrun"
)

type cleanRequest struct {
	yes           bool
	selected      map[string]bool
	format        string
	configuration string
}

func (request cleanRequest) asks(rule footprintRule) bool {
	switch rule.removal {
	case removedByReap:
		return true
	case removedWithYes:
		return request.yes
	case removedWhenSelected:
		return request.yes && request.selected[rule.flag]
	case removedNever:
	}
	return false
}

type cleanResult struct {
	Removed    []footprintItem    `json:"removed"`
	LeftBehind []footprintItem    `json:"left_behind"`
	Pending    []footprintItem    `json:"pending"`
	Kept       []footprintItem    `json:"kept"`
	Unreadable []footprintProblem `json:"unreadable"`
	Notes      []string           `json:"notes"`
}

var historySelectors = []string{"artifacts", "backups", "ledger"}

func newCleanCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove what Review Party left on this host",
		Long: `Remove runs whose owner is gone from the runtime root, together with their
recorded Reviewer processes, exactly as every invocation's automatic reap
does. Runs whose owner is still running are never touched.

Everything else is only previewed unless --yes is given. With --yes, clean
also removes the model discovery cache, unpublished artifact writes, and
directories earlier releases left in the host temp directory. Review history
needs its own flag as well: --artifacts, --backups, and --ledger each remove
that part of the state directory, and only while no run is live.

Clean exits 0 when everything it was asked to remove is gone, including a
preview with pending items. It exits 1 when anything it was asked to remove
is left behind, and names each one with the reason.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			request := cleanRequest{yes: boolFlag(cmd, "yes"), selected: map[string]bool{}, format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config")}
			for _, name := range historySelectors {
				request.selected[name] = boolFlag(cmd, name)
			}
			return commandResult(executeClean(cmd.Context(), request, streams))
		},
	}
	cmd.Flags().Bool("yes", false, "Remove the cache, unpublished artifact writes, and earlier releases' temp leftovers")
	cmd.Flags().Bool("artifacts", false, "With --yes, also remove published review artifacts")
	cmd.Flags().Bool("backups", false, "With --yes, also remove backed-up ledgers")
	cmd.Flags().Bool("ledger", false, "With --yes, also remove the review ledger")
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

func executeClean(ctx context.Context, request cleanRequest, streams commandIO) int {
	if err := validateConfigurationFormat(request.format); err != nil {
		return printConfigFailure(request.format, streams.output, streams.errors, err)
	}
	result := cleanResult{Removed: []footprintItem{}, LeftBehind: []footprintItem{}, Pending: []footprintItem{}, Kept: []footprintItem{}, Notes: []string{}}
	result.reap(ctx, streams.host.withDefaults().runtimeRoot)
	after := takeFootprint(streams.host, request.configuration)
	result.Unreadable = append(result.Unreadable, after.Unreadable...)
	for _, item := range after.Items {
		result.settle(item, request)
	}
	render := result.writeText
	if request.format == "json" {
		render = func(output *commandOutput) { output.err = writeJSON(output.writer, result) }
	}
	if code := printCommandOutput(streams.output, streams.errors, render); code != 0 {
		return code
	}
	if code := reportUnreadable(streams, result.Unreadable); code != 0 || len(result.LeftBehind) > 0 {
		return 1
	}
	return 0
}

// reap removes dead runs through the invocation's run, the same pass every
// command's Open starts, so clean never races the automatic reap.
func (result *cleanResult) reap(ctx context.Context, root string) {
	run, err := hostrun.From(ctx)
	if err != nil {
		result.Unreadable = append(result.Unreadable, footprintProblem{Path: root, Error: err.Error()})
		return
	}
	report := run.Reap()
	for _, path := range report.Removed {
		result.Removed = append(result.Removed, footprintItem{Path: path, Kind: kindOrphanedRun, Class: classRuntime, Removable: true})
	}
	for _, left := range report.Leftovers {
		result.LeftBehind = append(result.LeftBehind, footprintItem{Path: left.Path, Kind: kindOrphanedRun, Class: classRuntime, Error: left.Err.Error()})
	}
}

func (result *cleanResult) settle(item footprintItem, request cleanRequest) {
	rule := footprintRules[item.Kind]
	switch {
	case rule.removal == removedNever:
		result.Kept = append(result.Kept, item)
	case !request.asks(rule):
		result.Pending = append(result.Pending, item)
	case rule.removal == removedByReap:
		result.leaveUnreaped(item)
	case !item.Removable:
		result.LeftBehind = append(result.LeftBehind, item)
	default:
		result.remove(item)
	}
}

// leaveUnreaped reports a run the reap pass did not remove, unless the
// pass already reported it as a leftover.
func (result *cleanResult) leaveUnreaped(item footprintItem) {
	if slices.ContainsFunc(result.LeftBehind, func(left footprintItem) bool { return left.Path == item.Path }) {
		return
	}
	if item.Reason == "" {
		item.Reason = "the reap pass did not remove it"
	}
	result.LeftBehind = append(result.LeftBehind, item)
}

func (result *cleanResult) remove(item footprintItem) {
	if err := os.RemoveAll(item.Path); err != nil {
		item.Error = err.Error()
		result.LeftBehind = append(result.LeftBehind, item)
		return
	}
	result.Removed = append(result.Removed, item)
	if item.Kind == kindLegacy && filepath.Base(item.Path) == "review-party-worktrees" {
		result.Notes = append(result.Notes, "repositories reviewed by an earlier release may still list worktrees under "+item.Path+"; run `git worktree prune` in each to clear them")
	}
}

func (result cleanResult) writeText(output *commandOutput) {
	var removedBytes int64
	for _, item := range result.Removed {
		removedBytes += item.Bytes
		output.write("removed %s %s %s\n", item.Kind, item.Path, humanBytes(item.Bytes))
	}
	for _, item := range result.LeftBehind {
		output.write("left behind %s %s: %s\n", item.Kind, item.Path, item.problem())
	}
	for _, item := range result.Pending {
		output.write("pending %s %s %s; remove with: %s%s\n", item.Kind, item.Path, humanBytes(item.Bytes), item.Remove, item.blocker())
	}
	for _, item := range result.Kept {
		output.write("kept %s %s: %s\n", item.Kind, item.Path, item.Reason)
	}
	for _, note := range result.Notes {
		output.write("note: %s\n", note)
	}
	output.write("Removed %d items (%s); %d pending; %d left behind\n", len(result.Removed), humanBytes(removedBytes), len(result.Pending), len(result.LeftBehind))
}

func (item footprintItem) problem() string {
	if item.Error != "" {
		return item.Error
	}
	return item.Reason
}

func (item footprintItem) blocker() string {
	if item.Removable {
		return ""
	}
	return "; not now: " + item.Reason
}
