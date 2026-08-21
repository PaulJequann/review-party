package main

import (
	"bytes"
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
		"eval":     func(arguments []string) int { return runEval(ctx, arguments, stdout, stderr) },
		"profiles": func(arguments []string) int { return runProfiles(ctx, arguments, stdout, stderr) },
		"explain":  func(arguments []string) int { return runExplain(ctx, arguments, stdout, stderr) },
		"config":   func(arguments []string) int { return runConfig(arguments, stdout, stderr) },
		"inspect":  func(arguments []string) int { return runInspect(ctx, arguments, stdout, stderr) },
		"history":  func(arguments []string) int { return runHistory(ctx, arguments, stdout, stderr) },
		"init":     func(arguments []string) int { return runInit(arguments, stdout, stderr) },
		"profile":  func(arguments []string) int { return runProfile(ctx, arguments, stdout, stderr) },
		"party":    func(arguments []string) int { return runParty(ctx, arguments, stdout, stderr) },
		"parties":  func(arguments []string) int { return runParties(ctx, arguments, stdout, stderr) },
		"help":     help,
		"-h":       help,
		"--help":   help,
	}
}

type experimentFile struct {
	SchemaVersion int                           `json:"schema_version"`
	Name          string                        `json:"name"`
	Experiment    model.ExperimentConfiguration `json:"experiment"`
}

func runEval(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	subcommand, remaining := takeLeadingValue(arguments)
	switch subcommand {
	case "run":
		return runEvalSuite(ctx, remaining, stdout, stderr)
	case "inspect":
		return runEvalInspect(ctx, remaining, stdout, stderr)
	case "adjudication":
		return runEvalAdjudication(ctx, remaining, stdout, stderr)
	case "score":
		return runEvalScore(ctx, remaining, stdout, stderr)
	case "compare":
		return runEvalCompare(ctx, remaining, stdout, stderr)
	default:
		fmt.Fprintln(stderr, "review-party: eval requires run, inspect, adjudication, score, or compare")
		return 2
	}
}

type evalCompareOptions struct {
	baseline      string
	candidate     string
	format        string
	configuration string
}

func runEvalCompare(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	options, ok := parseEvalCompareOptions(arguments, stderr)
	if !ok {
		return 2
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	comparison, err := conductor.CompareAdjudications(ctx, model.AdjudicationRevisionID(options.baseline), model.AdjudicationRevisionID(options.candidate))
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	return printEvalComparison(comparison, options.format, stdout, stderr)
}

func parseEvalCompareOptions(arguments []string, stderr io.Writer) (evalCompareOptions, bool) {
	flags := flag.NewFlagSet("eval compare", flag.ContinueOnError)
	flags.SetOutput(stderr)
	baseline := flags.String("baseline", "", "Baseline adjudication revision id")
	candidate := flags.String("candidate", "", "Candidate adjudication revision id")
	format := flags.String("format", "human", "Output format: human or json")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	if flags.Parse(arguments) != nil {
		return evalCompareOptions{}, false
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: eval compare requires --baseline AR_ID and --candidate AR_ID")
		return evalCompareOptions{}, false
	}
	if *baseline == "" {
		fmt.Fprintln(stderr, "review-party: eval compare requires --baseline AR_ID and --candidate AR_ID")
		return evalCompareOptions{}, false
	}
	if *candidate == "" {
		fmt.Fprintln(stderr, "review-party: eval compare requires --baseline AR_ID and --candidate AR_ID")
		return evalCompareOptions{}, false
	}
	if !strings.HasPrefix(*baseline, "ar_") || !strings.HasPrefix(*candidate, "ar_") {
		fmt.Fprintln(stderr, "review-party: eval compare requires adjudication revision ids beginning with ar_")
		return evalCompareOptions{}, false
	}
	return evalCompareOptions{baseline: *baseline, candidate: *candidate, format: *format, configuration: *configuration}, true
}

func printEvalComparison(comparison model.EvalComparison, format string, stdout, stderr io.Writer) int {
	if format == "json" {
		if err := json.NewEncoder(stdout).Encode(comparison); err != nil {
			return 1
		}
		return 0
	}
	if format != "human" {
		fmt.Fprintf(stderr, "review-party: unknown output format %q\n", format)
		return 1
	}
	fmt.Fprintf(stdout, "comparison %s vs %s · %d/%d shared cases\n", comparison.BaselineAdjudication, comparison.CandidateAdjudication, comparison.Coverage.ComparedCases, comparison.Coverage.BaselineCases)
	fmt.Fprintf(stdout, "baseline: %s %s · %s · %s\n", comparison.BaselineIdentity.Experiment.Reviewer, comparison.BaselineIdentity.Experiment.Model, comparison.BaselineIdentity.Experiment.Effort, comparison.BaselineIdentity.Runtime.VCSRevision)
	fmt.Fprintf(stdout, "candidate: %s %s · %s · %s\n", comparison.CandidateIdentity.Experiment.Reviewer, comparison.CandidateIdentity.Experiment.Model, comparison.CandidateIdentity.Experiment.Effort, comparison.CandidateIdentity.Runtime.VCSRevision)
	fmt.Fprintf(stdout, "recall %s → %s · precision %s → %s · clean accuracy %s → %s · completion %s → %s\n", formatRatio(comparison.DefectRecall.Baseline), formatRatio(comparison.DefectRecall.Candidate), formatRatio(comparison.FindingPrecision.Baseline), formatRatio(comparison.FindingPrecision.Candidate), formatRatio(comparison.CleanCaseAccuracy.Baseline), formatRatio(comparison.CleanCaseAccuracy.Candidate), formatRatio(comparison.CompletionRate.Baseline), formatRatio(comparison.CompletionRate.Candidate))
	fmt.Fprintf(stdout, "runtime %dms → %dms (%+dms)\n", comparison.BaselineRuntime.TotalMS, comparison.CandidateRuntime.TotalMS, comparison.RuntimeDeltaMS)
	if comparisonHasCoverageGaps(comparison) {
		fmt.Fprintf(stdout, "omitted baseline=%v candidate=%v mismatched=%v\n", comparison.Coverage.OmittedBaselineIDs, comparison.Coverage.OmittedCandidateIDs, comparison.Coverage.MismatchedCaseIDs)
	}
	return 0
}

func comparisonHasCoverageGaps(comparison model.EvalComparison) bool {
	coverage := comparison.Coverage
	return len(coverage.OmittedBaselineIDs) > 0 || len(coverage.OmittedCandidateIDs) > 0 || len(coverage.MismatchedCaseIDs) > 0
}

func runEvalSuite(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	suite, remaining := takeLeadingValue(arguments)
	if suite == "" {
		fmt.Fprintln(stderr, "review-party: eval run requires a suite")
		return 2
	}
	options, ok := parseEvalRunOptions(remaining, stderr)
	if !ok {
		return 2
	}
	experiment, effectiveDeadline, err := resolveEvalExperiment(options)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 2
	}
	conductor, err := engine.New(engine.Config{AttemptDeadline: effectiveDeadline, UserConfigurationPath: options.configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	run, err := conductor.RunEvalSuite(ctx, model.EvalSuiteSelection{Suite: suite, Experiment: experiment})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	return printEvalSuiteRun(run, options.format, stdout, stderr)
}

type evalRunOptions struct {
	experimentPath string
	profile        string
	reviewer       string
	model          string
	effort         string
	deadline       time.Duration
	attempts       int
	concurrency    int
	format         string
	configuration  string
	overrides      map[string]bool
}

func parseEvalRunOptions(arguments []string, stderr io.Writer) (evalRunOptions, bool) {
	flags := flag.NewFlagSet("eval run", flag.ContinueOnError)
	flags.SetOutput(stderr)
	experimentPath := flags.String("experiment", "", "Named Experiment Configuration JSON")
	profile := flags.String("profile", "", "Review Profile override")
	reviewer := flags.String("reviewer", "", "Explicit Reviewer")
	modelName := flags.String("model", "", "Explicit model")
	effort := flags.String("effort", "", "Explicit reasoning effort")
	deadline := flags.Duration("deadline", 0, "Execution deadline")
	attempts := flags.Int("attempts", 0, "Maximum attempts per Eval Case")
	concurrency := flags.Int("concurrency", 0, "Maximum active Eval Cases")
	format := flags.String("format", "human", "Output format: human or json")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	if err := flags.Parse(arguments); err != nil {
		return evalRunOptions{}, false
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: eval run accepts one suite")
		return evalRunOptions{}, false
	}
	overrides := map[string]bool{}
	flags.Visit(func(value *flag.Flag) { overrides[value.Name] = true })
	return evalRunOptions{experimentPath: *experimentPath, profile: *profile, reviewer: *reviewer, model: *modelName, effort: *effort, deadline: *deadline, attempts: *attempts, concurrency: *concurrency, format: *format, configuration: *configuration, overrides: overrides}, true
}

func resolveEvalExperiment(options evalRunOptions) (model.ExperimentConfiguration, time.Duration, error) {
	experiment, err := loadExperimentFile(options.experimentPath)
	if err != nil {
		return model.ExperimentConfiguration{}, 0, err
	}
	if err := engine.ApplyEvalConfigurationDefaults(options.configuration, &experiment); err != nil {
		return model.ExperimentConfiguration{}, 0, err
	}
	applyExperimentOverrides(&experiment, options)
	applyExperimentDefaults(&experiment)
	effectiveDeadline, err := time.ParseDuration(experiment.Deadline)
	if err != nil || effectiveDeadline <= 0 {
		return model.ExperimentConfiguration{}, 0, errors.New("eval deadline must be positive")
	}
	return experiment, effectiveDeadline, nil
}

func applyExperimentOverrides(experiment *model.ExperimentConfiguration, options evalRunOptions) {
	if options.overrides["profile"] {
		experiment.Profile = options.profile
	}
	if options.overrides["reviewer"] {
		experiment.Reviewer = options.reviewer
	}
	if options.overrides["model"] {
		experiment.Model = options.model
	}
	if options.overrides["effort"] {
		experiment.Effort = options.effort
	}
	if options.overrides["deadline"] {
		experiment.Deadline = options.deadline.String()
	}
	if options.overrides["attempts"] {
		experiment.RetryPolicy.MaxAttempts = options.attempts
	}
	if options.overrides["concurrency"] {
		experiment.ConcurrencyLimit = options.concurrency
	}
}

func applyExperimentDefaults(experiment *model.ExperimentConfiguration) {
	if experiment.Profile == "" {
		experiment.Profile = "bugs"
	}
	if experiment.Deadline == "" {
		experiment.Deadline = (3 * time.Minute).String()
	}
	if experiment.RetryPolicy.MaxAttempts == 0 {
		experiment.RetryPolicy = model.RetryPolicy{MaxAttempts: 3, InitialBackoff: "1s", MaxBackoff: "30s"}
	}
	if experiment.ConcurrencyLimit == 0 {
		experiment.ConcurrencyLimit = 1
	}
}

func loadExperimentFile(path string) (model.ExperimentConfiguration, error) {
	if path == "" {
		return model.ExperimentConfiguration{}, nil
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return model.ExperimentConfiguration{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var definition experimentFile
	if err := decoder.Decode(&definition); err != nil {
		return model.ExperimentConfiguration{}, err
	}
	if definition.SchemaVersion != 1 || definition.Name == "" {
		return model.ExperimentConfiguration{}, errors.New("experiment requires schema version 1 and a name")
	}
	return definition.Experiment, nil
}

func runEvalInspect(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	id, format, configuration, ok := parseEvalInspectOptions(arguments, stderr)
	if !ok {
		return 2
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if strings.HasPrefix(id, "esr_") {
		return inspectEvalSuiteRun(evalInspectCommand{ctx: ctx, conductor: conductor, id: id, format: format, stdout: stdout, stderr: stderr})
	}
	if strings.HasPrefix(id, "ar_") {
		return inspectAdjudication(evalInspectCommand{ctx: ctx, conductor: conductor, id: id, format: format, stdout: stdout, stderr: stderr})
	}
	return inspectEvalRun(evalInspectCommand{ctx: ctx, conductor: conductor, id: id, format: format, stdout: stdout, stderr: stderr})
}

func runEvalAdjudication(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	id, configuration, ok := parseAdjudicationExportOptions(arguments, stderr)
	if !ok {
		return 2
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	document, err := conductor.ExportAdjudication(ctx, model.EvalSuiteRunID(id))
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		return 1
	}
	return 0
}

func parseAdjudicationExportOptions(arguments []string, stderr io.Writer) (string, string, bool) {
	subcommand, remaining := takeLeadingValue(arguments)
	id, remaining := takeLeadingValue(remaining)
	flags := flag.NewFlagSet("eval adjudication export", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	if flags.Parse(remaining) != nil {
		return "", "", false
	}
	if subcommand != "export" {
		fmt.Fprintln(stderr, "review-party: eval adjudication export requires one Eval Suite Run id")
		return "", "", false
	}
	if id == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: eval adjudication export requires one Eval Suite Run id")
		return "", "", false
	}
	return id, *configuration, true
}

func runEvalScore(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	id, adjudicationPath, format, configuration, ok := parseEvalScoreOptions(arguments, stderr)
	if !ok {
		return 2
	}
	document, err := loadAdjudication(id, adjudicationPath)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 2
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	revision, err := conductor.PublishAdjudication(ctx, document)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	return printAdjudication(revision, format, stdout, stderr)
}

func parseEvalScoreOptions(arguments []string, stderr io.Writer) (string, string, string, string, bool) {
	id, remaining := takeLeadingValue(arguments)
	flags := flag.NewFlagSet("eval score", flag.ContinueOnError)
	flags.SetOutput(stderr)
	adjudicationPath := flags.String("adjudication", "", "Adjudication JSON document")
	format := flags.String("format", "human", "Output format: human or json")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	if flags.Parse(remaining) != nil {
		return "", "", "", "", false
	}
	if id == "" {
		fmt.Fprintln(stderr, "review-party: eval score requires one Eval Suite Run id and --adjudication PATH")
		return "", "", "", "", false
	}
	if *adjudicationPath == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: eval score requires one Eval Suite Run id and --adjudication PATH")
		return "", "", "", "", false
	}
	return id, *adjudicationPath, *format, *configuration, true
}

func loadAdjudication(id, path string) (model.AdjudicationDocument, error) {
	var document model.AdjudicationDocument
	if err := decodeJSONFile(path, &document); err != nil {
		return document, err
	}
	if document.SuiteRunID != model.EvalSuiteRunID(id) {
		return document, errors.New("adjudication suite_run_id does not match command")
	}
	return document, nil
}

func decodeJSONFile(path string, destination any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("adjudication file must contain one JSON document")
	}
	return nil
}

func inspectAdjudication(command evalInspectCommand) int {
	revision, err := command.conductor.InspectAdjudication(command.ctx, model.AdjudicationRevisionID(command.id))
	if err != nil {
		fmt.Fprintf(command.stderr, "review-party: %v\n", err)
		return 1
	}
	return printAdjudication(revision, command.format, command.stdout, command.stderr)
}

func printAdjudication(revision model.AdjudicationRevision, format string, stdout, stderr io.Writer) int {
	if format == "json" {
		if err := json.NewEncoder(stdout).Encode(revision); err != nil {
			return 1
		}
		return 0
	}
	if format != "human" {
		fmt.Fprintf(stderr, "review-party: unknown output format %q\n", format)
		return 1
	}
	fmt.Fprintf(stdout, "adjudication %s · suite %s · revision %d\n", revision.ID, revision.SuiteRunID, revision.RevisionNumber)
	for _, adjudication := range revision.Document.Cases {
		fmt.Fprintf(stdout, "case %s · %s · %d expected · %d reported\n", adjudication.CaseID, adjudication.ExecutionState, len(adjudication.ExpectedFindings), len(adjudication.ReportedFindings))
	}
	fmt.Fprintf(stdout, "recall %s · precision %s · clean accuracy %s · completion %s\n", formatRatio(revision.Score.DefectRecall), formatRatio(revision.Score.FindingPrecision), formatRatio(revision.Score.CleanCaseAccuracy), formatRatio(revision.Score.CompletionRate))
	return 0
}

func formatRatio(metric model.RatioMetric) string {
	if metric.Value == nil {
		return fmt.Sprintf("%d/%d", metric.Numerator, metric.Denominator)
	}
	return fmt.Sprintf("%d/%d (%.1f%%)", metric.Numerator, metric.Denominator, *metric.Value*100)
}

type evalInspectCommand struct {
	ctx       context.Context
	conductor *engine.Conductor
	id        string
	format    string
	stdout    io.Writer
	stderr    io.Writer
}

func parseEvalInspectOptions(arguments []string, stderr io.Writer) (string, string, string, bool) {
	id, remaining := takeLeadingValue(arguments)
	flags := flag.NewFlagSet("eval inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "human", "Output format: human or json")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	if id == "" {
		fmt.Fprintln(stderr, "review-party: eval inspect requires one Eval Run or Eval Suite Run id")
		return "", "", "", false
	}
	if flags.Parse(remaining) != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: eval inspect requires one Eval Run or Eval Suite Run id")
		return "", "", "", false
	}
	return id, *format, *configuration, true
}

func inspectEvalSuiteRun(command evalInspectCommand) int {
	run, err := command.conductor.InspectEvalSuiteRun(command.ctx, model.EvalSuiteRunID(command.id))
	if err != nil {
		fmt.Fprintf(command.stderr, "review-party: %v\n", err)
		return 1
	}
	return printEvalSuiteRun(run, command.format, command.stdout, command.stderr)
}

func inspectEvalRun(command evalInspectCommand) int {
	run, err := command.conductor.InspectEvalRun(command.ctx, model.EvalRunID(command.id))
	if err != nil {
		fmt.Fprintf(command.stderr, "review-party: %v\n", err)
		return 1
	}
	if command.format == "json" {
		if err := json.NewEncoder(command.stdout).Encode(run); err != nil {
			return 1
		}
		return 0
	}
	if command.format != "human" {
		fmt.Fprintf(command.stderr, "review-party: unknown output format %q\n", command.format)
		return 1
	}
	review := "not started"
	if run.ReviewID != "" {
		review = string(run.ReviewID)
	}
	fmt.Fprintf(command.stdout, "eval %s · case %s · %s · review %s · %s\n", run.ID, run.Case.ID, run.ExecutionState, review, run.AdjudicationState)
	return 0
}

func printEvalSuiteRun(run model.EvalSuiteRun, format string, stdout, stderr io.Writer) int {
	if format == "json" {
		if err := json.NewEncoder(stdout).Encode(run); err != nil {
			return 1
		}
		return 0
	}
	if format != "human" {
		fmt.Fprintf(stderr, "review-party: unknown output format %q\n", format)
		return 1
	}
	fmt.Fprintf(stdout, "eval suite %s · %s · %s@%s · %d clean · %d findings · %d incomplete\n", run.ID, run.Lifecycle, run.Suite, run.SuiteRevision, run.CompletedCleanCount, run.CompletedFindingCount, run.IncompleteCount)
	for _, id := range run.EvalRunIDs {
		fmt.Fprintf(stdout, "eval: review-party eval inspect %s\n", id)
	}
	return 0
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
	options, ok := parseInspectOptions(arguments, stderr)
	if !ok {
		return 2
	}
	if strings.HasPrefix(string(options.id), "rb_") {
		return runInspectBundle(ctx, options, stdout, stderr)
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	record, err := conductor.Inspect(ctx, options.id)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if options.verifyArtifacts {
		if err := conductor.VerifyArtifacts(record); err != nil {
			fmt.Fprintf(stderr, "review-party: verify artifacts: %v\n", err)
			return 1
		}
	}
	if err := printRecordWithConfiguration(stdout, record, options.format, options.configuration); err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	return 0
}

type inspectOptions struct {
	id              model.ReviewID
	format          string
	verifyArtifacts bool
	configuration   string
}

func runInspectBundle(ctx context.Context, options inspectOptions, stdout, stderr io.Writer) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	bundle, err := conductor.InspectBundle(ctx, model.ReviewBundleID(options.id))
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	if err := printBundle(stdout, bundle, options.format); err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	return 0
}

func parseInspectOptions(arguments []string, stderr io.Writer) (inspectOptions, bool) {
	idValue, remaining := takeLeadingValue(arguments)
	if idValue == "" {
		fmt.Fprintln(stderr, "review-party: inspect requires a review id")
		return inspectOptions{}, false
	}
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	format := flags.String("format", "human", "Output format: human or json")
	verifyArtifacts := flags.Bool("verify-artifacts", false, "Verify referenced artifact files")
	configuration := flags.String("config", defaultUserConfigurationPath(), "User configuration path")
	if err := flags.Parse(remaining); err != nil {
		return inspectOptions{}, false
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "review-party: inspect accepts one review id")
		return inspectOptions{}, false
	}
	return inspectOptions{id: model.ReviewID(idValue), format: *format, verifyArtifacts: *verifyArtifacts, configuration: *configuration}, true
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
	fmt.Fprintln(output, "  review-party parties [--repo PATH] [--format human|json]")
	fmt.Fprintln(output, "  review-party profile create NAME (--blank|--from-packaged PROFILE) [--repo PATH|--global]")
	fmt.Fprintln(output, "  review-party profile install-defaults [--repo PATH|--global]")
	fmt.Fprintln(output, "  review-party config path|show [--config PATH]")
	fmt.Fprintf(output, "  review-party explain PROFILE [--repo PATH] [--reviewer %s] [--model MODEL] [--effort EFFORT] [--format human|json]\n", strings.Join(engine.SupportedReviewers(), "|"))
	fmt.Fprintf(output, "  review-party review [PROFILE] [--reviewer %s] [--model MODEL] [--effort EFFORT] [--config PATH] [--repo PATH] [--base COMMIT --head COMMIT] [--format human|json]\n", strings.Join(engine.SupportedReviewers(), "|"))
	fmt.Fprintf(output, "  review-party party run PARTY [--reviewer %s] [--model MODEL] [--effort EFFORT] [--concurrency N] [--config PATH] [--repo PATH] [--base COMMIT --head COMMIT] [--deadline DURATION] [--format human|json]\n", strings.Join(engine.SupportedReviewers(), "|"))
	fmt.Fprintln(output, "  review-party replay REVIEW_ID [--reviewer ID] [--model MODEL] [--effort EFFORT] [--format human|json] [--config PATH]")
	fmt.Fprintln(output, "  review-party eval run SUITE [--experiment PATH] [--profile NAME] --reviewer ID --model MODEL [--effort EFFORT] [--deadline DURATION] [--format human|json]")
	fmt.Fprintln(output, "  review-party eval compare --baseline AR_ID --candidate AR_ID [--format human|json]")
	fmt.Fprintln(output, "  review-party eval inspect EVAL_ID [--format human|json] [--config PATH]")
	fmt.Fprintln(output, "  review-party eval adjudication export EVAL_SUITE_RUN_ID [--config PATH]")
	fmt.Fprintln(output, "  review-party eval score EVAL_SUITE_RUN_ID --adjudication PATH [--format human|json] [--config PATH]")
	fmt.Fprintln(output, "  review-party inspect REVIEW_ID [--format human|json] [--verify-artifacts] [--config PATH]")
	fmt.Fprintln(output, "  review-party history [--repo PATH] [--reviewer ID] [--profile NAME] [--lifecycle STATE] [--termination CATEGORY] [--subject ID] [--since RFC3339] [--limit N] [--format human|json] [--config PATH]")
	fmt.Fprintln(output, "  review-party init [--repo PATH] [--state-dir PATH] [--config PATH]")
}
