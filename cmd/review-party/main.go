package main

import (
	"context"
	"encoding/json"
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
	entries, err := conductor.History(ctx, options.limit)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	return printHistory(entries, options.format, stdout, stderr)
}

type historyOptions struct {
	limit         int
	format        string
	configuration string
}

func parseHistoryOptions(arguments []string, stderr io.Writer) (historyOptions, bool) {
	flags := flag.NewFlagSet("history", flag.ContinueOnError)
	flags.SetOutput(stderr)
	limit := flags.Int("limit", 20, "Maximum Reviews to show")
	format := flags.String("format", "human", "Output format: human or json")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	if err := flags.Parse(arguments); err != nil {
		fmt.Fprintln(stderr, "review-party: history accepts --limit N and --format human|json")
		return historyOptions{}, false
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: history accepts --limit N and --format human|json")
		return historyOptions{}, false
	}
	if *limit < 1 {
		fmt.Fprintln(stderr, "review-party: history accepts --limit N and --format human|json")
		return historyOptions{}, false
	}
	return historyOptions{limit: *limit, format: *format, configuration: *configuration}, true
}

func printHistory(entries []store.HistoryEntry, format string, stdout, stderr io.Writer) int {
	if format == "json" {
		if entries == nil {
			entries = []store.HistoryEntry{}
		}
		if err := json.NewEncoder(stdout).Encode(entries); err != nil {
			fmt.Fprintf(stderr, "review-party: %v\n", err)
			return 1
		}
		return 0
	}
	if format != "human" {
		fmt.Fprintf(stderr, "review-party: unknown output format %q\n", format)
		return 1
	}
	for _, entry := range entries {
		fmt.Fprintf(stdout, "%s · %s · %s\n", entry.ID, entry.Lifecycle, entry.CreatedAt.UTC().Format(time.RFC3339))
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
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: review accepts one optional profile name")
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
		Subject:    model.WorkingChanges(),
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
	fmt.Fprintf(output, "  review-party review [PROFILE] [--reviewer %s] [--model MODEL] [--effort EFFORT] [--config PATH] [--repo PATH] [--format human|json]\n", strings.Join(engine.SupportedReviewers(), "|"))
	fmt.Fprintln(output, "  review-party inspect REVIEW_ID [--format human|json] [--verify-artifacts] [--config PATH]")
	fmt.Fprintln(output, "  review-party history [--limit N] [--format human|json] [--config PATH]")
	fmt.Fprintln(output, "  review-party init [--repo PATH] [--state-dir PATH] [--config PATH]")
}
