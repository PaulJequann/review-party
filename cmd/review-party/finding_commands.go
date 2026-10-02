package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

const findingRecordLong = `Record verdicts on a Review's findings. Each stdin line judges one finding:

  N accept|reject|defer REASON

N is the finding number from the review report. REASON is one line of 1 to 240
bytes saying why. The lines record together or not at all: any bad line exits 2
and records nothing. Repeating a finding's current verdict records nothing; a
different verdict supersedes it. A verdict judges the finding's text, so it shows
as stale if that text later changes.`

func newFindingCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use: "finding", Short: "Record and list verdicts on Review findings",
		Args: cobra.NoArgs, RunE: showCommandHelp,
	}
	cmd.AddCommand(newFindingRecordCommand(streams), newFindingListCommand(streams))
	return cmd
}

func newFindingRecordCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use: "record REVIEW", Short: "Record verdicts on a Review's findings from stdin",
		Long:    findingRecordLong,
		Example: "  review-party finding record rp_... <<'EOF'\n  1 accept nil map write is reachable from the handler\n  2 reject the caller already checks the length\n  EOF",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			request, lines, err := parseFindingRecordRequest(cmd, args[0], streams.input)
			if err != nil {
				return err
			}
			return commandResult(newFindingExecution(cmd, streams).record(cmd.Context(), request, lines))
		},
	}
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	cmd.Flags().String("recorded-by", "", "Recorder name; defaults to the OS username")
	return cmd
}

func newFindingListCommand(streams commandIO) *cobra.Command {
	cmd := &cobra.Command{
		Use: "list [REVIEW]", Short: "List the current verdict on each judged finding",
		Example: "  review-party finding list rp_...\n  review-party finding list --repo . --profile bugs --format json",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateConfigurationFormat(stringFlag(cmd, "format")); err != nil {
				return err
			}
			query := store.VerdictQuery{Repository: stringFlag(cmd, "repo"), Profile: stringFlag(cmd, "profile")}
			for _, arg := range args {
				review, err := engine.ParseVerdictReview(arg)
				if err != nil {
					return err
				}
				query.ReviewIDs = append(query.ReviewIDs, review)
			}
			return commandResult(newFindingExecution(cmd, streams).list(cmd.Context(), query))
		},
	}
	addFormatFlag(cmd)
	addConfigurationFlag(cmd)
	cmd.Flags().String("repo", "", "Git repository whose Reviews' verdicts to list; all repositories when omitted")
	cmd.Flags().String("profile", "", "Review Profile to match")
	return cmd
}

// parseFindingRecordRequest settles everything decidable before the ledger
// opens. It also returns the stdin line of each judged finding, for errors.
func parseFindingRecordRequest(cmd *cobra.Command, id string, input io.Reader) (engine.VerdictRequest, map[int]int, error) {
	review, err := engine.ParseVerdictReview(id)
	if err != nil {
		return engine.VerdictRequest{}, nil, fmt.Errorf("%w; nothing recorded", err)
	}
	if err := validateConfigurationFormat(stringFlag(cmd, "format")); err != nil {
		return engine.VerdictRequest{}, nil, err
	}
	recordedBy, err := actorFlag(cmd, "recorded-by")
	if err != nil {
		return engine.VerdictRequest{}, nil, err
	}
	if isTerminalStream(input) {
		return engine.VerdictRequest{}, nil, errors.New(`finding record reads "N accept|reject|defer REASON" lines from stdin; pipe them in`)
	}
	judgments, lines, err := parseJudgmentLines(input)
	if err != nil {
		return engine.VerdictRequest{}, nil, fmt.Errorf("%w; nothing recorded", err)
	}
	return engine.VerdictRequest{Review: review, Judgments: judgments, RecordedBy: recordedBy}, lines, nil
}

func parseJudgmentLines(input io.Reader) ([]model.Judgment, map[int]int, error) {
	var judgments []model.Judgment
	lines := map[int]int{}
	scanner := bufio.NewScanner(input)
	for number := 1; scanner.Scan(); number++ {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		judgment, err := parseJudgmentLine(scanner.Text())
		if err != nil {
			return nil, nil, fmt.Errorf("line %d: %w", number, err)
		}
		if first, judged := lines[judgment.Ordinal]; judged {
			return nil, nil, fmt.Errorf("line %d: finding %d is already judged on line %d; keep one line per finding", number, judgment.Ordinal, first)
		}
		lines[judgment.Ordinal] = number
		judgments = append(judgments, judgment)
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("read stdin: %w", err)
	}
	if len(judgments) == 0 {
		return nil, nil, errors.New(`stdin has no "N accept|reject|defer REASON" lines`)
	}
	return judgments, lines, nil
}

func parseJudgmentLine(text string) (model.Judgment, error) {
	number, rest := cutField(text)
	verb, reasonText := cutField(rest)
	ordinal, err := strconv.Atoi(number)
	if err != nil || ordinal < 1 {
		return model.Judgment{}, fmt.Errorf("%q is not a finding number; start the line with N from the report", number)
	}
	verdict, ok := model.ParseVerdictVerb(verb)
	if !ok {
		return model.Judgment{}, fmt.Errorf("%q is not a verdict; use accept, reject, or defer", verb)
	}
	if strings.TrimSpace(reasonText) == "" {
		return model.Judgment{}, fmt.Errorf("finding %d needs a reason after %s", ordinal, verb)
	}
	reason, err := model.ParseVerdictReason(reasonText)
	if err != nil {
		return model.Judgment{}, err
	}
	return model.Judgment{Ordinal: ordinal, Verdict: verdict, Reason: reason}, nil
}

// cutField splits off the first whitespace-separated field of text.
func cutField(text string) (string, string) {
	text = strings.TrimLeftFunc(text, unicode.IsSpace)
	end := strings.IndexFunc(text, unicode.IsSpace)
	if end < 0 {
		return text, ""
	}
	return text[:end], text[end:]
}

type findingExecution struct {
	format        string
	configuration string
	stdout        io.Writer
	stderr        io.Writer
}

func newFindingExecution(cmd *cobra.Command, streams commandIO) findingExecution {
	return findingExecution{
		format: stringFlag(cmd, "format"), configuration: stringFlag(cmd, "config"),
		stdout: streams.output, stderr: streams.errors,
	}
}

func (execution findingExecution) record(ctx context.Context, request engine.VerdictRequest, lines map[int]int) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: execution.configuration})
	if err != nil {
		return printFailure(execution.stderr, err)
	}
	tally, err := conductor.RecordVerdicts(ctx, request)
	var missing store.MissingFindingError
	switch {
	case errors.As(err, &missing):
		return printFailure(execution.stderr, fmt.Errorf("line %d: %w; nothing recorded", lines[missing.Ordinal], err))
	case err != nil:
		return printFailure(execution.stderr, fmt.Errorf("%w; nothing recorded", err))
	}
	return renderLedgerOutput(execution.format, execution.stdout, execution.stderr, tally, func(output *commandOutput) {
		output.write("recorded %d", tally.Recorded)
		if tally.Unchanged > 0 {
			output.write(" (%d unchanged)", tally.Unchanged)
		}
		if tally.Changed > 0 {
			output.write(" (%d changed)", tally.Changed)
		}
		output.write("\n")
	})
}

func (execution findingExecution) list(ctx context.Context, query store.VerdictQuery) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: execution.configuration})
	if err != nil {
		return printFailure(execution.stderr, err)
	}
	query.Repository, err = resolvedHistoryRepository(query.Repository)
	if err != nil {
		return printFailure(execution.stderr, err)
	}
	verdicts, err := conductor.FindingVerdicts(ctx, query)
	if err != nil {
		return printFailure(execution.stderr, err)
	}
	listing := struct {
		Verdicts []model.FindingVerdict `json:"verdicts"`
	}{verdicts}
	return renderLedgerOutput(execution.format, execution.stdout, execution.stderr, listing, func(output *commandOutput) {
		for _, verdict := range verdicts {
			parts := []string{string(verdict.ReviewID), verdict.Profile, "finding " + strconv.Itoa(verdict.Ordinal), verdict.Location, string(verdict.Verdict) + ": " + verdict.Reason}
			if verdict.Stale {
				parts = append(parts, "stale")
			}
			output.write("%s\n", strings.Join(parts, " · "))
		}
	})
}
