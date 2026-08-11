package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
	"reviewparty/internal/subject"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		printUsage(stderr)
		return 2
	}
	handler, exists := commandHandlers(ctx, stdout, stderr)[arguments[0]]
	if !exists {
		fmt.Fprintf(stderr, "review-party: unknown command %q\n", arguments[0])
		printUsage(stderr)
		return 2
	}
	return handler(arguments[1:])
}

func commandHandlers(ctx context.Context, stdout, stderr io.Writer) map[string]func([]string) int {
	help := func([]string) int {
		printUsage(stdout)
		return 0
	}
	return map[string]func([]string) int{
		"review":   func(arguments []string) int { return runReview(ctx, arguments, stdout, stderr) },
		"replay":   func(arguments []string) int { return runReplay(ctx, arguments, stdout, stderr) },
		"profiles": func(arguments []string) int { return runProfiles(ctx, arguments, stdout, stderr) },
		"explain":  func(arguments []string) int { return runExplain(ctx, arguments, stdout, stderr) },
		"config":   func(arguments []string) int { return runConfig(arguments, stdout, stderr) },
		"inspect":  func(arguments []string) int { return runInspect(ctx, arguments, stdout, stderr) },
		"history":  func(arguments []string) int { return runHistory(ctx, arguments, stdout, stderr) },
		"init":     func(arguments []string) int { return runInit(arguments, stdout, stderr) },
		"profile":  func(arguments []string) int { return runProfile(ctx, arguments, stdout, stderr) },
		"help":     help,
		"-h":       help,
		"--help":   help,
	}
}

func runHistory(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	options, ok := parseHistoryOptions(arguments, stderr)
	if !ok {
		return 2
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	query := options.query
	if query.Repository != "" {
		query.Repository, err = subject.ResolveRepositoryRoot(query.Repository)
		if err != nil {
			fmt.Fprintf(stderr, "review-party: %v\n", err)
			return 1
		}
	}
	page, err := conductor.History(ctx, query)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	return printHistory(page, options.format, stdout, stderr)
}

type historyOptions struct {
	query         store.HistoryQuery
	format        string
	configuration string
}

func parseHistoryOptions(arguments []string, stderr io.Writer) (historyOptions, bool) {
	flags := flag.NewFlagSet("history", flag.ContinueOnError)
	flags.SetOutput(stderr)
	limit := flags.Int("limit", 20, "Maximum Reviews to show")
	format := flags.String("format", "human", "Output format: human or json")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	repository := flags.String("repo", "", "Git repository identity to match")
	reviewer := flags.String("reviewer", "", "Recorded Reviewer to match")
	profile := flags.String("profile", "", "Recorded Profile to match")
	lifecycle := flags.String("lifecycle", "", "Lifecycle to match")
	termination := flags.String("termination", "", "Termination category to match")
	subjectIdentity := flags.String("subject", "", "Subject identity to match")
	sinceText := flags.String("since", "", "Include Reviews created at or after this RFC3339 timestamp")
	if err := flags.Parse(arguments); err != nil {
		fmt.Fprintln(stderr, "review-party: history accepts --limit N and --format human|json")
		return historyOptions{}, false
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: history accepts --limit N and --format human|json")
		return historyOptions{}, false
	}
	if *limit < 1 || *limit > store.MaxHistoryLimit {
		fmt.Fprintln(stderr, "review-party: history accepts --limit N and --format human|json")
		return historyOptions{}, false
	}
	query := store.HistoryQuery{Repository: *repository, Reviewer: *reviewer, Profile: *profile, Lifecycle: model.Lifecycle(*lifecycle), Termination: model.TerminationCategory(*termination), Subject: *subjectIdentity, Limit: *limit}
	if *sinceText != "" {
		since, err := time.Parse(time.RFC3339, *sinceText)
		if err != nil {
			fmt.Fprintln(stderr, "review-party: --since must be an RFC3339 timestamp")
			return historyOptions{}, false
		}
		query.Since = &since
	}
	return historyOptions{query: query, format: *format, configuration: *configuration}, true
}

func printHistory(page store.HistoryPage, format string, stdout, stderr io.Writer) int {
	if format == "json" {
		if page.Entries == nil {
			page.Entries = []store.HistoryEntry{}
		}
		if err := json.NewEncoder(stdout).Encode(page); err != nil {
			fmt.Fprintf(stderr, "review-party: %v\n", err)
			return 1
		}
		return 0
	}
	if format != "human" {
		fmt.Fprintf(stderr, "review-party: unknown output format %q\n", format)
		return 1
	}
	for _, entry := range page.Entries {
		fmt.Fprintln(stdout, formatHistoryEntry(entry))
	}
	return 0
}

func formatHistoryEntry(entry store.HistoryEntry) string {
	parts := []string{string(entry.ID), string(entry.Lifecycle), entry.Profile, entry.Reviewer, entry.Subject}
	if entry.ReplaysReviewID != nil {
		parts = append(parts, "replays "+string(*entry.ReplaysReviewID))
	}
	if entry.Termination != "" {
		parts = append(parts, string(entry.Termination))
	}
	parts = append(parts, entry.CreatedAt.UTC().Format(time.RFC3339))
	return strings.Join(parts, " · ")
}

func runReplay(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	idValue, remaining := takeLeadingValue(arguments)
	if idValue == "" {
		fmt.Fprintln(stderr, "review-party: replay requires a source review id")
		return 2
	}
	flags := flag.NewFlagSet("replay", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "human", "Output format: human or json")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	reviewer := flags.String("reviewer", "", "Explicit Reviewer override")
	modelName := flags.String("model", "", "Explicit model override")
	effort := flags.String("effort", "", "Explicit reasoning effort override")
	if err := flags.Parse(remaining); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: replay accepts one source review id")
		return 2
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: *configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	record, err := conductor.Replay(ctx, model.ReplaySelection{SourceReviewID: model.ReviewID(idValue), Reviewer: *reviewer, Model: *modelName, Effort: *effort})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if err := printRecordWithConfiguration(stdout, record, *format, *configuration); err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if record.Lifecycle == model.LifecycleIncomplete {
		return 2
	}
	return 0
}

func runReview(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	profile, arguments := takeLeadingValue(arguments)
	flags := flag.NewFlagSet("review", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repo", ".", "Git repository to review")
	format := flags.String("format", "human", "Output format: human or json")
	deadline := flags.Duration("deadline", 10*time.Minute, "Attempt deadline")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	reviewer := flags.String("reviewer", "", "Reviewer adapter: "+strings.Join(engine.SupportedReviewers(), ", ")+"; empty uses configured/Profile default")
	modelName := flags.String("model", "", "Explicit model for the selected Reviewer")
	effort := flags.String("effort", "", "Explicit reasoning effort for the selected Reviewer")
	base := flags.String("base", "", "Committed-range base revision")
	head := flags.String("head", "", "Committed-range head revision")
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: review accepts one optional profile name")
		return 2
	}
	subjectReference, err := reviewSubjectReference(*base, *head)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 2
	}

	conductor, err := engine.New(engine.Config{
		AttemptDeadline:       *deadline,
		UserConfigurationPath: *configuration,
	})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	record, err := conductor.Review(ctx, model.ReviewSelection{
		Repository: *repository,
		Subject:    subjectReference,
		Profile:    profile,
		Reviewer:   *reviewer,
		Model:      *modelName,
		Effort:     *effort,
	})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if err := printRecordWithConfiguration(stdout, record, *format, *configuration); err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if record.Lifecycle == model.LifecycleIncomplete {
		return 2
	}
	return 0
}

func reviewSubjectReference(base, head string) (model.SubjectReference, error) {
	if (base == "") != (head == "") {
		return model.SubjectReference{}, errors.New("--base and --head must be provided together")
	}
	if base != "" {
		return model.CommittedRange(base, head), nil
	}
	return model.WorkingChanges(), nil
}

func runInspect(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	idValue, remaining := takeLeadingValue(arguments)
	if idValue == "" {
		fmt.Fprintln(stderr, "review-party: inspect requires a review id")
		return 2
	}
	id := model.ReviewID(idValue)
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "human", "Output format: human or json")
	verifyArtifacts := flags.Bool("verify-artifacts", false, "Verify referenced artifact files")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	if err := flags.Parse(remaining); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: inspect accepts one review id")
		return 2
	}

	conductor, err := engine.New(engine.Config{UserConfigurationPath: *configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	record, err := conductor.Inspect(ctx, id)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if *verifyArtifacts {
		if err := conductor.VerifyArtifacts(record); err != nil {
			fmt.Fprintf(stderr, "review-party: verify artifacts: %v\n", err)
			return 1
		}
	}
	if err := printRecordWithConfiguration(stdout, record, *format, *configuration); err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	return 0
}

func printRecord(output io.Writer, record model.ReviewRecord, format string) error {
	return printRecordWithConfiguration(output, record, format, defaultUserConfigurationPath())
}

func printRecordWithConfiguration(output io.Writer, record model.ReviewRecord, format, configuration string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(record)
	}
	if format != "human" {
		return fmt.Errorf("unknown output format %q", format)
	}
	printHumanRecord(output, record, configuration)
	return nil
}

func printHumanRecord(output io.Writer, record model.ReviewRecord, configuration string) {
	findings := 0
	if record.Result != nil {
		findings = record.Result.FindingCount()
	}
	fmt.Fprintf(output, "review %s\n", record.ID)
	printReplayLineage(output, record.ReplaysReviewID)
	provenance := latestProvenance(record)
	fmt.Fprintf(output, "%s · %d finding(s) · %s/%s (%s)\n", record.Lifecycle, findings, provenance.ReviewerID, provenance.Model, provenance.Effort)
	if record.ProfileRevision.Source != "" {
		fmt.Fprintf(output, "profile: %s · %s\n", record.ProfileRevision.Name, record.ProfileRevision.Source)
	}
	if record.Result != nil {
		fmt.Fprintln(output, record.Result.Raw)
	}
	printArtifactReferences(output, record)
	if record.Termination != nil {
		fmt.Fprintf(output, "incomplete: %s at %s: %s\n", record.Termination.Category, record.Termination.Phase, record.Termination.Message)
	} else if record.IncompleteCause != "" {
		fmt.Fprintf(output, "incomplete: %s\n", record.IncompleteCause)
	}
	fmt.Fprintf(output, "inspect: review-party inspect %s", record.ID)
	if configuration != defaultUserConfigurationPath() {
		fmt.Fprintf(output, " --config %s", shellQuoteArgument(configuration))
	}
	fmt.Fprintln(output)
}

func printReplayLineage(output io.Writer, source *model.ReviewID) {
	if source != nil {
		fmt.Fprintf(output, "replays: %s\n", *source)
	}
}

func shellQuoteArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func latestProvenance(record model.ReviewRecord) model.ReviewerProvenance {
	provenance := model.ReviewerProvenance{
		ReviewerID: record.ProfileRevision.ReviewerID,
		Model:      record.ProfileRevision.Model,
		Effort:     record.ProfileRevision.Effort,
	}
	for _, pass := range record.Passes {
		if len(pass.Attempts) > 0 {
			provenance = pass.Attempts[len(pass.Attempts)-1].Provenance
		}
	}
	return provenance
}

func takeLeadingValue(arguments []string) (string, []string) {
	if len(arguments) == 0 {
		return "", arguments
	}
	if arguments[0] == "" {
		return "", arguments
	}
	if strings.HasPrefix(arguments[0], "-") {
		return "", arguments
	}
	return arguments[0], arguments[1:]
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, "usage:")
	fmt.Fprintln(output, "  review-party profiles [--repo PATH] [--format human|json]")
	fmt.Fprintln(output, "  review-party profile create NAME (--blank|--from-packaged PROFILE) [--repo PATH|--global]")
	fmt.Fprintln(output, "  review-party profile install-defaults [--repo PATH|--global]")
	fmt.Fprintln(output, "  review-party config path|show [--config PATH]")
	fmt.Fprintf(output, "  review-party explain PROFILE [--repo PATH] [--reviewer %s] [--model MODEL] [--effort EFFORT] [--format human|json]\n", strings.Join(engine.SupportedReviewers(), "|"))
	fmt.Fprintf(output, "  review-party review [PROFILE] [--reviewer %s] [--model MODEL] [--effort EFFORT] [--config PATH] [--repo PATH] [--base COMMIT --head COMMIT] [--format human|json]\n", strings.Join(engine.SupportedReviewers(), "|"))
	fmt.Fprintln(output, "  review-party replay REVIEW_ID [--reviewer ID] [--model MODEL] [--effort EFFORT] [--format human|json] [--config PATH]")
	fmt.Fprintln(output, "  review-party inspect REVIEW_ID [--format human|json] [--verify-artifacts] [--config PATH]")
	fmt.Fprintln(output, "  review-party history [--repo PATH] [--reviewer ID] [--profile NAME] [--lifecycle STATE] [--termination CATEGORY] [--subject ID] [--since RFC3339] [--limit N] [--format human|json] [--config PATH]")
	fmt.Fprintln(output, "  review-party init [--repo PATH] [--state-dir PATH] [--config PATH]")
}
