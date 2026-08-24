package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
)

type experimentFile struct {
	SchemaVersion int                           `json:"schema_version"`
	Name          string                        `json:"name"`
	Experiment    model.ExperimentConfiguration `json:"experiment"`
}

type evalCompareOptions struct {
	baseline      string
	candidate     string
	format        string
	configuration string
}

func executeEvalCompare(ctx context.Context, options evalCompareOptions, stdout, stderr io.Writer) int {
	if options.baseline == "" || options.candidate == "" {
		fmt.Fprintln(stderr, "review-party: eval compare requires --baseline AR_ID and --candidate AR_ID")
		return usageExitCode
	}
	if !strings.HasPrefix(options.baseline, "ar_") || !strings.HasPrefix(options.candidate, "ar_") {
		fmt.Fprintln(stderr, "review-party: eval compare requires adjudication revision ids beginning with ar_")
		return usageExitCode
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

func executeEvalSuite(ctx context.Context, options evalRunOptions, stdout, stderr io.Writer) int {
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
	run, err := conductor.RunEvalSuite(ctx, model.EvalSuiteSelection{Suite: options.suite, Experiment: experiment})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	return printEvalSuiteRun(run, options.format, stdout, stderr)
}

type evalRunOptions struct {
	suite          string
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

type evalInspectOptions struct {
	id            string
	format        string
	configuration string
}

func executeEvalInspect(ctx context.Context, options evalInspectOptions, stdout, stderr io.Writer) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	command := evalInspectCommand{ctx: ctx, conductor: conductor, id: options.id, format: options.format, stdout: stdout, stderr: stderr}
	if strings.HasPrefix(options.id, "esr_") {
		return inspectEvalSuiteRun(command)
	}
	if strings.HasPrefix(options.id, "ar_") {
		return inspectAdjudication(command)
	}
	return inspectEvalRun(command)
}

type evalAdjudicationOptions struct {
	id            string
	configuration string
}

func executeEvalAdjudication(ctx context.Context, options evalAdjudicationOptions, stdout, stderr io.Writer) int {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	document, err := conductor.ExportAdjudication(ctx, model.EvalSuiteRunID(options.id))
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

type evalScoreOptions struct {
	id               string
	adjudicationPath string
	format           string
	configuration    string
}

func executeEvalScore(ctx context.Context, options evalScoreOptions, stdout, stderr io.Writer) int {
	if options.adjudicationPath == "" {
		fmt.Fprintln(stderr, "review-party: eval score requires one Eval Suite Run id and --adjudication PATH")
		return usageExitCode
	}
	document, err := loadAdjudication(options.id, options.adjudicationPath)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 2
	}
	conductor, err := engine.New(engine.Config{UserConfigurationPath: options.configuration})
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	revision, err := conductor.PublishAdjudication(ctx, document)
	if err != nil {
		fmt.Fprintf(stderr, "review-party: %v\n", err)
		return 1
	}
	return printAdjudication(revision, options.format, stdout, stderr)
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
