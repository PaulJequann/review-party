package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/user"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

func newMissCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use: "miss", Short: "Record bugs that completed Reviews missed",
		Long: "A miss names a location a completed Review should have reported. Misses are kept in the review ledger and removed only by tombstone.",
		Args: cobra.NoArgs, RunE: showCommandHelp,
	}
	cmd.AddCommand(newMissAddCommand(streams), newMissListCommand(streams), newMissRemoveCommand(streams))
	return cmd
}

func newMissAddCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use: "add", Short: "Record a miss against a completed Review or every member of a Review Bundle",
		Example: "  review-party miss add --review rp_... --path internal/store/store.go --line 42 --source codex-pr --description \"nil map write\"\n" +
			"  review-party miss add --review rb_... --profile bugs --path main.go --source human --description \"leaked file handle\"",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			request, err := parseMissAddRequest(cmd)
			if err != nil {
				return err
			}
			return commandResult(newMissExecution(cmd, streams).add(cmd.Context(), request))
		},
	}
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	cmd.Flags().String("review", "", "Review id (rp_...) or Review Bundle id (rb_...) that missed the bug")
	cmd.Flags().String("profile", "", "Bundle member Profile to attach the miss to; all members when omitted")
	cmd.Flags().String("path", "", "Repository-relative path of the missed bug")
	cmd.Flags().Int("line", 0, "Line of the missed bug")
	cmd.Flags().String("source", "", "Who found the miss: codex-pr, human, incident, or other")
	cmd.Flags().String("description", "", "What the Review missed")
	cmd.Flags().String("recorded-by", "", "Recorder name; defaults to the OS username")
	for _, name := range []string{"review", "path", "source", "description"} {
		mustMarkFlagRequired(cmd, name)
	}
	return cmd
}

func newMissListCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use: "list", Short: "List recorded misses",
		Example: "  review-party miss list --repo .\n  review-party miss list --profile bugs --include-removed --format json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			query := store.MissQuery{Repository: stringFlag(cmd, "repo"), Profile: stringFlag(cmd, "profile"), IncludeRemoved: boolFlag(cmd, "include-removed")}
			return commandResult(newMissExecution(cmd, streams).list(cmd.Context(), query))
		},
	}
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	cmd.Flags().String("repo", "", "Git repository whose Reviews' misses to list; all repositories when omitted")
	cmd.Flags().String("profile", "", "Review Profile to match")
	cmd.Flags().Bool("include-removed", false, "Include removed misses with their tombstones")
	return cmd
}

func newMissRemoveCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use: "remove MISS...", Short: "Remove misses, keeping each as a tombstone",
		Example: "  review-party miss remove ms_... --reason \"not a bug\"",
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			request, err := parseMissRemovalRequest(cmd, args)
			if err != nil {
				return err
			}
			return commandResult(newMissExecution(cmd, streams).remove(cmd.Context(), request))
		},
	}
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	cmd.Flags().String("reason", "", "Why the misses are removed")
	cmd.Flags().String("removed-by", "", "Remover name; defaults to the OS username")
	mustMarkFlagRequired(cmd, "reason")
	return cmd
}

func mustMarkFlagRequired(cmd *cobra.Command, name string) {
	if err := cmd.MarkFlagRequired(name); err != nil {
		panic(fmt.Sprintf("mark registered flag %q required: %v", name, err))
	}
}

func parseMissAddRequest(cmd *cobra.Command) (engine.MissRequest, error) {
	target, err := engine.ParseMissTarget(stringFlag(cmd, "review"), stringFlag(cmd, "profile"))
	if err != nil {
		return engine.MissRequest{}, err
	}
	source, err := model.ParseMissSource(stringFlag(cmd, "source"))
	if err != nil {
		return engine.MissRequest{}, err
	}
	line := intFlag(cmd, "line")
	if cmd.Flags().Changed("line") && line < 1 {
		return engine.MissRequest{}, fmt.Errorf("--line must be at least 1")
	}
	path, description := stringFlag(cmd, "path"), stringFlag(cmd, "description")
	if strings.TrimSpace(path) == "" || strings.TrimSpace(description) == "" {
		return engine.MissRequest{}, fmt.Errorf("miss add requires a non-empty --path and --description")
	}
	recordedBy, err := actorFlag(cmd, "recorded-by")
	if err != nil {
		return engine.MissRequest{}, err
	}
	report := model.MissReport{
		Location: model.MissLocation{Path: path, Line: line}, Source: source,
		Description: description, RecordedBy: recordedBy,
	}
	return engine.MissRequest{Target: target, Report: report}, nil
}

func parseMissRemovalRequest(cmd *cobra.Command, args []string) (engine.MissRemovalRequest, error) {
	ids := make([]model.MissID, 0, len(args))
	for _, arg := range args {
		id, err := engine.ParseMissID(arg)
		if err != nil {
			return engine.MissRemovalRequest{}, err
		}
		ids = append(ids, id)
	}
	reason := stringFlag(cmd, "reason")
	if strings.TrimSpace(reason) == "" {
		return engine.MissRemovalRequest{}, fmt.Errorf("miss remove requires a non-empty --reason")
	}
	removedBy, err := actorFlag(cmd, "removed-by")
	if err != nil {
		return engine.MissRemovalRequest{}, err
	}
	return engine.MissRemovalRequest{IDs: ids, Reason: reason, RemovedBy: removedBy}, nil
}

func actorFlag(cmd *cobra.Command, name string) (string, error) {
	if cmd.Flags().Changed(name) {
		actor := stringFlag(cmd, name)
		if strings.TrimSpace(actor) == "" {
			return "", fmt.Errorf("--%s must not be empty", name)
		}
		return actor, nil
	}
	current, err := user.Current()
	if err != nil || current.Username == "" {
		return "", errors.Join(fmt.Errorf("cannot resolve the OS username; pass --%s", name), err)
	}
	return current.Username, nil
}

type missExecution struct {
	format        string
	configuration string
	stdout        io.Writer
	stderr        io.Writer
}

func newMissExecution(cmd *cobra.Command, streams commandIO) missExecution {
	return missExecution{
		format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config"),
		stdout: streams.output, stderr: streams.errors,
	}
}

func (execution missExecution) add(ctx context.Context, request engine.MissRequest) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: execution.configuration})
	if err != nil {
		return printFailure(execution.stderr, err)
	}
	misses, err := conductor.RecordMiss(ctx, request)
	if err != nil {
		return printFailure(execution.stderr, err)
	}
	return execution.printMisses(misses, false)
}

func (execution missExecution) list(ctx context.Context, query store.MissQuery) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: execution.configuration})
	if err != nil {
		return printFailure(execution.stderr, err)
	}
	query.Repository, err = resolvedHistoryRepository(query.Repository)
	if err != nil {
		return printFailure(execution.stderr, err)
	}
	misses, err := conductor.Misses(ctx, query)
	if err != nil {
		return printFailure(execution.stderr, err)
	}
	return execution.printMisses(misses, true)
}

func (execution missExecution) remove(ctx context.Context, request engine.MissRemovalRequest) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: execution.configuration})
	if err != nil {
		return printFailure(execution.stderr, err)
	}
	outcomes, err := conductor.RemoveMisses(ctx, request)
	if err != nil {
		return printFailure(execution.stderr, err)
	}
	return renderHistoryOutput(execution.format, execution.stdout, execution.stderr, outcomes, func(output *commandOutput) {
		for _, outcome := range outcomes {
			output.write("%s %s\n", outcome.ID, strings.ReplaceAll(string(outcome.Status), "-", " "))
		}
	})
}

func (execution missExecution) printMisses(misses []model.Miss, listed bool) int {
	if misses == nil {
		misses = []model.Miss{}
	}
	return renderHistoryOutput(execution.format, execution.stdout, execution.stderr, misses, func(output *commandOutput) {
		for _, miss := range misses {
			output.write("%s\n", formatMiss(miss, listed))
		}
	})
}

func formatMiss(miss model.Miss, listed bool) string {
	parts := []string{string(miss.ID), string(miss.ReviewID), miss.Profile, miss.Location.String(), string(miss.Source), miss.Description}
	if listed {
		parts = append(parts, miss.RecordedAt.UTC().Format(time.RFC3339))
	}
	if miss.Removal != nil {
		parts = append(parts, "removed: "+miss.Removal.Reason)
	}
	return strings.Join(parts, " · ")
}

func attachReviewMisses(ctx context.Context, conductor *engine.Conductor, report reviewReport) error {
	for index, entry := range report.Reviews {
		if entry.ID == "" {
			continue
		}
		misses, err := conductor.Misses(ctx, store.MissQuery{ReviewID: entry.ID})
		if err != nil {
			return fmt.Errorf("load misses for review %s: %w", entry.ID, err)
		}
		report.Reviews[index].Misses = append(report.Reviews[index].Misses, misses...)
	}
	return nil
}
