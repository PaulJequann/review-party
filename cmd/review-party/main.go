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

	"reviewparty"
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
	switch arguments[0] {
	case "review":
		return runReview(ctx, arguments[1:], stdout, stderr)
	case "inspect":
		return runInspect(ctx, arguments[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "review-party: unknown command %q\n", arguments[0])
		printUsage(stderr)
		return 2
	}
}

func runReview(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	profile := "bugs"
	if len(arguments) > 0 && arguments[0] != "" && arguments[0][0] != '-' {
		profile = arguments[0]
		arguments = arguments[1:]
	}
	flags := flag.NewFlagSet("review", flag.ContinueOnError)
	flags.SetOutput(stderr)
	repository := flags.String("repo", ".", "Git repository to review")
	format := flags.String("format", "human", "Output format: human or json")
	records := flags.String("records", "", "Review Record directory")
	deadline := flags.Duration("deadline", 10*time.Minute, "Attempt deadline")
	reviewer := flags.String("reviewer", "grok", "Reviewer adapter: "+strings.Join(reviewparty.SupportedReviewers(), ", "))
	if err := flags.Parse(arguments); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: review accepts one optional profile name")
		return 2
	}

	conductor, err := reviewparty.New(reviewparty.Config{
		RecordDirectory: *records,
		AttemptDeadline: *deadline,
	})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	record, err := conductor.Review(ctx, reviewparty.ReviewSelection{
		Repository: *repository,
		Subject:    reviewparty.WorkingChanges(),
		Profile:    profile,
		Reviewer:   *reviewer,
	})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if err := printRecord(stdout, record, *format); err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if record.Lifecycle == reviewparty.LifecycleIncomplete {
		return 2
	}
	return 0
}

func runInspect(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 || arguments[0] == "" || arguments[0][0] == '-' {
		fmt.Fprintln(stderr, "review-party: inspect requires a review id")
		return 2
	}
	id := reviewparty.ReviewID(arguments[0])
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "human", "Output format: human or json")
	records := flags.String("records", "", "Review Record directory")
	if err := flags.Parse(arguments[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: inspect accepts one review id")
		return 2
	}

	conductor, err := reviewparty.New(reviewparty.Config{RecordDirectory: *records})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	record, err := conductor.Inspect(ctx, id)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if err := printRecord(stdout, record, *format); err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	return 0
}

func printRecord(output io.Writer, record reviewparty.ReviewRecord, format string) error {
	if format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(record)
	}
	if format != "human" {
		return fmt.Errorf("unknown output format %q", format)
	}
	findings := 0
	if record.Result != nil {
		findings = record.Result.FindingCount
	}
	fmt.Fprintf(output, "review %s\n", record.ID)
	provenance := reviewparty.ReviewerProvenance{
		ReviewerID: record.ProfileRevision.ReviewerID,
		Model:      record.ProfileRevision.Model,
		Effort:     record.ProfileRevision.Effort,
	}
	for _, pass := range record.Passes {
		if len(pass.Attempts) > 0 {
			provenance = pass.Attempts[len(pass.Attempts)-1].Provenance
		}
	}
	fmt.Fprintf(output, "%s · %d finding(s) · %s/%s (%s)\n", record.Lifecycle, findings, provenance.ReviewerID, provenance.Model, provenance.Effort)
	if record.Result != nil {
		fmt.Fprintln(output, record.Result.Raw)
	}
	if record.IncompleteCause != "" {
		fmt.Fprintf(output, "incomplete: %s\n", record.IncompleteCause)
	}
	fmt.Fprintf(output, "inspect: review-party inspect %s\n", record.ID)
	return nil
}

func printUsage(output io.Writer) {
	fmt.Fprintln(output, "usage:")
	fmt.Fprintf(output, "  review-party review [bugs] [--reviewer %s] [--repo PATH] [--format human|json]\n", strings.Join(reviewparty.SupportedReviewers(), "|"))
	fmt.Fprintln(output, "  review-party inspect REVIEW_ID [--format human|json]")
}
