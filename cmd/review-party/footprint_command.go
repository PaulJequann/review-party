package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newFootprintCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "footprint",
		Short: "List everything Review Party keeps on this host",
		Long: `List every file and directory Review Party owns on this host: runs under
the runtime root, the state directory's ledger, backups, and artifacts, the
model discovery cache, and leftovers earlier releases put in the host temp
directory. Each item shows its size and whether review-party clean can
remove it now.

Footprint changes nothing. It exits 1 when a location or an item could not
be fully read.`,
		Args:        cobra.NoArgs,
		Annotations: map[string]string{withoutRun: "reads the runtime root without claiming dead runs"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return commandResult(executeFootprint(stringFlag(cmd, "format"), stringFlag(cmd, "config"), streams))
		},
	}
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	return cmd
}

func executeFootprint(format, configurationPath string, streams commandIO) int {
	if err := validateConfigurationFormat(format); err != nil {
		return printConfigFailure(format, streams.output, streams.errors, err)
	}
	result := takeFootprint(streams.host, configurationPath)
	render := result.writeText
	if format == "json" {
		render = func(output *commandOutput) { output.err = writeJSON(output.writer, result) }
	}
	if code := printCommandOutput(streams.output, streams.errors, render); code != 0 {
		return code
	}
	return reportUnreadable(streams, append(result.Unreadable, result.itemProblems()...))
}

// itemProblems are the items listed despite an error inspecting them.
func (result footprint) itemProblems() []footprintProblem {
	var problems []footprintProblem
	for _, item := range result.Items {
		if item.Error != "" {
			problems = append(problems, footprintProblem{Path: item.Path, Error: item.Error})
		}
	}
	return problems
}

// reportUnreadable puts each location an inventory could not read on
// stderr, and exits 1 when there is one, because a partial inventory is
// never a clean one.
func reportUnreadable(streams commandIO, problems []footprintProblem) int {
	if len(problems) == 0 {
		return 0
	}
	for _, problem := range problems {
		printFailure(streams.errors, fmt.Errorf("could not read %s", problem))
	}
	return 1
}

func (problem footprintProblem) String() string {
	if problem.Path == "" {
		return "a location: " + problem.Error
	}
	return problem.Path + ": " + problem.Error
}

func (result footprint) writeText(output *commandOutput) {
	for _, item := range result.Items {
		output.write("%s\n", item.line())
	}
	output.write("%s\n", result.summary().line())
}

func (item footprintItem) line() string {
	parts := []string{fmt.Sprintf("%s %s %s", item.Kind, item.Path, humanBytes(item.Bytes))}
	if item.Run != nil {
		parts = append(parts, item.Run.description())
	}
	if item.Error != "" {
		parts = append(parts, "error: "+item.Error)
	}
	if item.Removable {
		parts = append(parts, "remove with: "+item.Remove)
	} else {
		parts = append(parts, "kept: "+item.Reason)
	}
	return strings.Join(parts, "; ")
}

func (run footprintRun) description() string {
	text := "run " + string(run.State)
	if run.Command != "" {
		text += " of " + run.Command
	}
	if run.PID != 0 {
		text += fmt.Sprintf(" by pid %d", run.PID)
	}
	if run.Version != "" {
		text += " version " + run.Version
	}
	if !run.Created.IsZero() {
		text += " since " + run.Created.Local().Format(time.RFC3339)
	}
	return text + fmt.Sprintf(", %d Reviewers, %d views", len(run.Reviewers), run.Views)
}

type footprintSummary struct {
	Items         int    `json:"items"`
	Bytes         int64  `json:"bytes"`
	Removable     int    `json:"removable"`
	Leftovers     int    `json:"leftovers"`
	LeftoverBytes int64  `json:"leftover_bytes"`
	Unreadable    int    `json:"unreadable"`
	Fix           string `json:"fix,omitempty"`
}

// summary counts the footprint. Leftovers are removable items no process
// needs; Fix is the clean invocation that removes all of them.
func (result footprint) summary() footprintSummary {
	summary := footprintSummary{Items: len(result.Items), Unreadable: len(result.Unreadable)}
	fix := footprintRule{removal: removedNever}
	for _, item := range result.Items {
		summary.Bytes += item.Bytes
		if !item.Removable {
			continue
		}
		summary.Removable++
		if rule := footprintRules[item.Kind]; rule.leftover {
			summary.Leftovers++
			summary.LeftoverBytes += item.Bytes
			fix.removal = max(fix.removal, rule.removal)
		}
	}
	summary.Fix = fix.command(result.configuration)
	return summary
}

func (summary footprintSummary) line() string {
	text := fmt.Sprintf("Footprint: %d items, %s; %d removable now", summary.Items, humanBytes(summary.Bytes), summary.Removable)
	if summary.Leftovers > 0 {
		text += fmt.Sprintf("; %d left over (%s); fix: %s", summary.Leftovers, humanBytes(summary.LeftoverBytes), summary.Fix)
	}
	if summary.Unreadable > 0 {
		text += fmt.Sprintf("; %d locations unreadable; see review-party footprint", summary.Unreadable)
	}
	return text
}

func humanBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	value, prefix := float64(bytes)/unit, 0
	for value >= unit && prefix < 3 {
		value /= unit
		prefix++
	}
	return fmt.Sprintf("%.1f %ciB", value, "KMGT"[prefix])
}
