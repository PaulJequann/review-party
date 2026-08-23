package model

import (
	"encoding/json"
	"time"
)

type ReviewID string

const (
	LegacyReviewRecordSchemaVersion  = 1
	CurrentReviewRecordSchemaVersion = 3
)

type Lifecycle string

const (
	LifecyclePending    Lifecycle = "pending"
	LifecycleRunning    Lifecycle = "running"
	LifecycleCompleted  Lifecycle = "completed"
	LifecycleIncomplete Lifecycle = "incomplete"
)

type SubjectKind string

const (
	SubjectWorkingChanges SubjectKind = "working-changes"
	SubjectCommittedRange SubjectKind = "committed-range"
	SubjectCapturedChange SubjectKind = "captured-change"
)

type SubjectReference struct {
	Kind         SubjectKind
	Base         string
	Head         string
	CapturedBase string
	CapturedHead string
}

func WorkingChanges() SubjectReference {
	return SubjectReference{Kind: SubjectWorkingChanges}
}

func CommittedRange(base, head string) SubjectReference {
	return SubjectReference{Kind: SubjectCommittedRange, Base: base, Head: head}
}

func CapturedChange(baseDirectory, headDirectory string) SubjectReference {
	return SubjectReference{Kind: SubjectCapturedChange, CapturedBase: baseDirectory, CapturedHead: headDirectory}
}

type ReviewSelection struct {
	Repository string
	Subject    SubjectReference
	Profile    string
	Reviewer   string
	Model      string
	Effort     string
}

type ReplaySelection struct {
	SourceReviewID ReviewID
	Reviewer       string
	Model          string
	Effort         string
}

type EvalRunID string
type EvalSuiteRunID string

type EvalExecutionState string
type EvalAdjudicationState string

const (
	EvalPending           EvalExecutionState = "pending"
	EvalRunning           EvalExecutionState = "running"
	EvalCompletedClean    EvalExecutionState = "completed_clean"
	EvalCompletedFindings EvalExecutionState = "completed_findings"
	EvalIncomplete        EvalExecutionState = "incomplete"
)

const (
	EvalAdjudicationNotReady EvalAdjudicationState = "not_ready"
	EvalAwaitingAdjudication EvalAdjudicationState = "awaiting_adjudication"
)

type ExpectedFinding struct {
	ID        string   `json:"id"`
	Behavior  string   `json:"behavior"`
	Impact    string   `json:"impact"`
	Evidence  []string `json:"evidence"`
	Locations []string `json:"locations,omitempty"`
}

type EvalCaseRevision struct {
	ID               string            `json:"id"`
	SchemaVersion    int               `json:"schema_version"`
	Digest           string            `json:"digest"`
	Mode             string            `json:"mode"`
	Classification   string            `json:"classification"`
	ExpectedFindings []ExpectedFinding `json:"expected_findings"`
	CleanEvidence    string            `json:"clean_evidence,omitempty"`
	Seed             *SeedRevision     `json:"seed,omitempty"`
}

type SeedRevision struct {
	ID            string   `json:"id"`
	SourceCommit  string   `json:"source_commit"`
	PatchDigest   string   `json:"patch_digest"`
	ExpectedFiles []string `json:"expected_files"`
}

type ExperimentConfiguration struct {
	Profile          string      `json:"profile"`
	Reviewer         string      `json:"reviewer"`
	Model            string      `json:"model"`
	Effort           string      `json:"effort"`
	Deadline         string      `json:"deadline"`
	RetryPolicy      RetryPolicy `json:"retry_policy"`
	ConcurrencyLimit int         `json:"concurrency_limit"`
}

type RetryPolicy struct {
	MaxAttempts    int    `json:"max_attempts"`
	InitialBackoff string `json:"initial_backoff"`
	MaxBackoff     string `json:"max_backoff"`
}

type EvalSuiteSelection struct {
	Suite      string                  `json:"suite"`
	Experiment ExperimentConfiguration `json:"experiment"`
}

type EvalRun struct {
	ID                EvalRunID             `json:"id"`
	SuiteRunID        EvalSuiteRunID        `json:"suite_run_id"`
	Case              EvalCaseRevision      `json:"case_revision"`
	ReviewID          ReviewID              `json:"review_id"`
	ExecutionState    EvalExecutionState    `json:"execution_state"`
	AdjudicationState EvalAdjudicationState `json:"adjudication_state"`
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
}

type EvalSuiteRun struct {
	ID                    EvalSuiteRunID          `json:"id"`
	Suite                 string                  `json:"suite"`
	SuiteRevision         string                  `json:"suite_revision"`
	SuiteDigest           string                  `json:"suite_digest"`
	Experiment            ExperimentConfiguration `json:"experiment"`
	Lifecycle             Lifecycle               `json:"lifecycle"`
	Termination           *EvalSuiteTermination   `json:"termination,omitempty"`
	EvalRunIDs            []EvalRunID             `json:"eval_run_ids"`
	CompletedCleanCount   int                     `json:"completed_clean_count"`
	CompletedFindingCount int                     `json:"completed_findings_count"`
	IncompleteCount       int                     `json:"incomplete_count"`
	StartedAt             time.Time               `json:"started_at"`
	CompletedAt           time.Time               `json:"completed_at"`
}

type EvalSuiteTermination struct {
	Category TerminationCategory `json:"category"`
	Message  string              `json:"message"`
}

type ReviewBundleID string

type PartyMember struct {
	Profile  string `json:"profile"`
	Reviewer string `json:"reviewer,omitempty"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
}

type PartyDefinition struct {
	SchemaVersion    int           `json:"schema_version"`
	Name             string        `json:"name"`
	Description      string        `json:"description,omitempty"`
	Extends          []string      `json:"extends,omitempty"`
	Profiles         []PartyMember `json:"profiles"`
	ConcurrencyLimit int           `json:"concurrency_limit,omitempty"`
}

type PartySelection struct {
	Name             string
	Repository       string
	Subject          SubjectReference
	Reviewer         string
	Model            string
	Effort           string
	ConcurrencyLimit int
}

type PartySummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Extends     []string `json:"extends,omitempty"`
	Members     []string `json:"members"`
	Source      string   `json:"source"`
	Error       string   `json:"error,omitempty"`
}

type BundleMember struct {
	Profile      string    `json:"profile"`
	ReviewID     ReviewID  `json:"review_id,omitempty"`
	Lifecycle    Lifecycle `json:"lifecycle"`
	Status       string    `json:"status,omitempty"`
	FindingCount int       `json:"finding_count,omitempty"`
}

type BundleTermination struct {
	Category TerminationCategory `json:"category"`
	Message  string              `json:"message"`
}

type ReviewBundle struct {
	ID               ReviewBundleID     `json:"id"`
	Party            string             `json:"party"`
	Description      string             `json:"description,omitempty"`
	PartyRevision    string             `json:"party_revision"`
	Repository       string             `json:"repository"`
	SubjectKind      SubjectKind        `json:"subject_kind"`
	SubjectIdentity  string             `json:"subject_identity"`
	Lifecycle        Lifecycle          `json:"lifecycle"`
	Termination      *BundleTermination `json:"termination,omitempty"`
	Members          []BundleMember     `json:"members"`
	ConcurrencyLimit int                `json:"concurrency_limit"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
	CompletedAt      time.Time          `json:"completed_at,omitempty"`
}

type AdjudicationRevisionID string

type ExpectedDisposition string
type ReportedDisposition string

const (
	ExpectedMatched   ExpectedDisposition = "matched"
	ExpectedMissed    ExpectedDisposition = "missed"
	ExpectedUncertain ExpectedDisposition = "uncertain"

	ReportedMatchedExpected ReportedDisposition = "matched_expected"
	ReportedNovelValid      ReportedDisposition = "novel_valid"
	ReportedFalsePositive   ReportedDisposition = "false_positive"
	ReportedUncertain       ReportedDisposition = "uncertain"
)

type ExpectedFindingAdjudication struct {
	Finding         ExpectedFinding     `json:"finding"`
	Disposition     ExpectedDisposition `json:"disposition"`
	ReportedOrdinal int                 `json:"reported_ordinal,omitempty"`
	Notes           string              `json:"notes,omitempty"`
}

type ReportedFindingAdjudication struct {
	Finding           Finding             `json:"finding"`
	Disposition       ReportedDisposition `json:"disposition"`
	ExpectedFindingID string              `json:"expected_finding_id,omitempty"`
	Notes             string              `json:"notes,omitempty"`
}

type EvalCaseAdjudication struct {
	EvalRunID        EvalRunID                     `json:"eval_run_id"`
	CaseID           string                        `json:"case_id"`
	ExecutionState   EvalExecutionState            `json:"execution_state"`
	Termination      *ReviewTermination            `json:"termination,omitempty"`
	ExpectedFindings []ExpectedFindingAdjudication `json:"expected_findings"`
	ReportedFindings []ReportedFindingAdjudication `json:"reported_findings"`
}

type AdjudicationDocument struct {
	SchemaVersion int                    `json:"schema_version"`
	SuiteRunID    EvalSuiteRunID         `json:"suite_run_id"`
	Cases         []EvalCaseAdjudication `json:"cases"`
}

type RatioMetric struct {
	Numerator   int      `json:"numerator"`
	Denominator int      `json:"denominator"`
	Value       *float64 `json:"value,omitempty"`
}

type EvalScore struct {
	DefectRecall           RatioMetric                 `json:"defect_recall"`
	FindingPrecision       RatioMetric                 `json:"finding_precision"`
	CleanCaseAccuracy      RatioMetric                 `json:"clean_case_accuracy"`
	CleanFalsePositiveRate RatioMetric                 `json:"clean_false_positive_rate"`
	CompletionRate         RatioMetric                 `json:"completion_rate"`
	UncertainExpected      int                         `json:"uncertain_expected"`
	UncertainReported      int                         `json:"uncertain_reported"`
	IncompleteCases        int                         `json:"incomplete_cases"`
	TerminationCounts      map[TerminationCategory]int `json:"termination_counts"`
}

type ComparisonMetric struct {
	Baseline  RatioMetric `json:"baseline"`
	Candidate RatioMetric `json:"candidate"`
	Delta     *float64    `json:"delta,omitempty"`
}

type ComparisonRuntime struct {
	TotalMS       int64 `json:"total_ms"`
	AverageMS     int64 `json:"average_ms"`
	ComparedCases int   `json:"compared_cases"`
}

type ComparisonIdentity struct {
	Suite                 string                  `json:"suite"`
	SuiteRevision         string                  `json:"suite_revision"`
	SuiteDigest           string                  `json:"suite_digest"`
	Experiment            ExperimentConfiguration `json:"experiment"`
	ProfileRevisionDigest string                  `json:"profile_revision_digest"`
	Reviewer              ReviewerProvenance      `json:"reviewer"`
	Runtime               RuntimeProvenance       `json:"runtime"`
}

type ComparisonCoverage struct {
	BaselineCases       int      `json:"baseline_cases"`
	CandidateCases      int      `json:"candidate_cases"`
	ComparedCases       int      `json:"compared_cases"`
	ComparedCaseIDs     []string `json:"compared_case_ids"`
	OmittedBaselineIDs  []string `json:"omitted_baseline_case_ids"`
	OmittedCandidateIDs []string `json:"omitted_candidate_case_ids"`
	MismatchedCaseIDs   []string `json:"mismatched_case_ids"`
}

type EvalComparison struct {
	SchemaVersion          int                         `json:"schema_version"`
	BaselineAdjudication   AdjudicationRevisionID      `json:"baseline_adjudication"`
	CandidateAdjudication  AdjudicationRevisionID      `json:"candidate_adjudication"`
	BaselineIdentity       ComparisonIdentity          `json:"baseline_identity"`
	CandidateIdentity      ComparisonIdentity          `json:"candidate_identity"`
	Coverage               ComparisonCoverage          `json:"coverage"`
	DefectRecall           ComparisonMetric            `json:"defect_recall"`
	FindingPrecision       ComparisonMetric            `json:"finding_precision"`
	CleanCaseAccuracy      ComparisonMetric            `json:"clean_case_accuracy"`
	CleanFalsePositiveRate ComparisonMetric            `json:"clean_false_positive_rate"`
	CompletionRate         ComparisonMetric            `json:"completion_rate"`
	BaselineTerminations   map[TerminationCategory]int `json:"baseline_terminations"`
	CandidateTerminations  map[TerminationCategory]int `json:"candidate_terminations"`
	BaselineRuntime        ComparisonRuntime           `json:"baseline_runtime"`
	CandidateRuntime       ComparisonRuntime           `json:"candidate_runtime"`
	RuntimeDeltaMS         int64                       `json:"runtime_delta_ms"`
}

type AdjudicationRevision struct {
	ID             AdjudicationRevisionID `json:"id"`
	SuiteRunID     EvalSuiteRunID         `json:"suite_run_id"`
	RevisionNumber int                    `json:"revision_number"`
	Document       AdjudicationDocument   `json:"document"`
	Score          EvalScore              `json:"score"`
	CreatedAt      time.Time              `json:"created_at"`
}

type ProfileSelection struct {
	Profile  string
	Reviewer string
	Model    string
	Effort   string
}

func (selection ReviewSelection) profileSelection() ProfileSelection {
	return ProfileSelection{
		Profile:  selection.Profile,
		Reviewer: selection.Reviewer,
		Model:    selection.Model,
		Effort:   selection.Effort,
	}
}

func (selection ReviewSelection) ProfileSelection() ProfileSelection {
	return selection.profileSelection()
}

type ReviewSubject struct {
	Kind                SubjectKind   `json:"kind"`
	Repository          string        `json:"repository"`
	Identity            string        `json:"identity"`
	BaseObject          string        `json:"base_object,omitempty"`
	HeadObject          string        `json:"head_object,omitempty"`
	ChangedPaths        []string      `json:"changed_paths"`
	Patch               string        `json:"patch"`
	Facts               *SubjectFacts `json:"facts,omitempty"`
	ExecutionRepository string        `json:"-"`
}

type SubjectFacts struct {
	ChangedFiles int `json:"changed_files"`
	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
	BinaryFiles  int `json:"binary_files"`
}

type ProfileRevision struct {
	Name                 string               `json:"name"`
	Revision             string               `json:"revision"`
	Description          string               `json:"description"`
	Purpose              string               `json:"purpose"`
	MaterialityThreshold string               `json:"materiality_threshold"`
	ReviewerID           string               `json:"reviewer_id"`
	Model                string               `json:"model"`
	Effort               string               `json:"effort"`
	Reviewer             ReviewerProvenance   `json:"reviewer"`
	Passes               []ReviewPassRevision `json:"passes"`
	RequiredCapabilities []Capability         `json:"required_capabilities"`
	AttemptLimit         int                  `json:"attempt_limit"`
	ExecutionDeadline    string               `json:"execution_deadline"`
	ResultContract       string               `json:"result_contract_revision"`
	Source               string               `json:"source"`
	SourceDigest         string               `json:"source_digest"`
	CompilerRevision     string               `json:"compiler_revision"`
}

type ProfileSnapshot struct {
	Name         string `json:"name"`
	Source       string `json:"source"`
	SourceDigest string `json:"source_digest"`
	Instructions string `json:"instructions"`
}

type Capability string

const (
	CapabilityRepositoryRead           Capability = "repository-read"
	CapabilityRepositorySearch         Capability = "repository-search"
	CapabilityRepositoryMutationDenied Capability = "repository-mutation-denied"
	CapabilityShellDenied              Capability = "shell-denied"
	CapabilityWebDenied                Capability = "web-denied"
)

type ReviewPassRevision struct {
	Name           string `json:"name"`
	Required       bool   `json:"required"`
	Purpose        string `json:"purpose"`
	PromptRevision string `json:"prompt_revision"`
}

type ProfileSummary struct {
	Name                 string               `json:"name"`
	Description          string               `json:"description"`
	DefaultReviewer      ReviewerProvenance   `json:"default_reviewer"`
	Passes               []ReviewPassRevision `json:"passes"`
	RequiredCapabilities []Capability         `json:"required_capabilities"`
	Source               string               `json:"source"`
	Path                 string               `json:"path"`
	Error                string               `json:"error,omitempty"`
}

type ProfileExplanation struct {
	ProfileRevision    ProfileRevision `json:"profile_revision"`
	ReviewerWasDefault bool            `json:"reviewer_was_default"`
	Instructions       string          `json:"instructions"`
}

type AttemptOutcome string

const (
	AttemptCompleted           AttemptOutcome = "completed"
	AttemptTransientFailure    AttemptOutcome = "transient_failure"
	AttemptReviewerUnavailable AttemptOutcome = "reviewer_unavailable"
	AttemptInvalidResult       AttemptOutcome = "invalid_result"
	AttemptCancelled           AttemptOutcome = "cancelled"
	AttemptUnknownFailure      AttemptOutcome = "unknown_failure"
)

type TerminationCategory string

const (
	TerminationReviewerUnavailable     TerminationCategory = "reviewer_unavailable"
	TerminationAuthenticationFailure   TerminationCategory = "authentication_failure"
	TerminationDeadlineExceeded        TerminationCategory = "deadline_exceeded"
	TerminationCancelled               TerminationCategory = "cancelled"
	TerminationTransportFailure        TerminationCategory = "transport_failure"
	TerminationMalformedOutput         TerminationCategory = "malformed_output"
	TerminationResultValidationFailure TerminationCategory = "result_validation_failure"
	TerminationUnknownFailure          TerminationCategory = "unknown_failure"
)

type ExecutionPhase string

const (
	PhaseAvailabilityCheck ExecutionPhase = "availability_check"
	PhaseHarnessLaunch     ExecutionPhase = "harness_launch"
	PhaseReviewerExecution ExecutionPhase = "reviewer_execution"
	PhaseOutputCapture     ExecutionPhase = "output_capture"
	PhaseOutputDecode      ExecutionPhase = "output_decode"
	PhaseResultValidation  ExecutionPhase = "result_validation"
)

type ReviewTermination struct {
	Category TerminationCategory `json:"category"`
	Phase    ExecutionPhase      `json:"phase"`
	Message  string              `json:"message"`
}

type RuntimeProvenance struct {
	Version     string `json:"version,omitempty"`
	VCSRevision string `json:"vcs_revision,omitempty"`
	VCSModified *bool  `json:"vcs_modified,omitempty"`
}

type ReviewTimings struct {
	SubjectResolutionMS  int64 `json:"subject_resolution_ms"`
	ProfileCompilationMS int64 `json:"profile_compilation_ms"`
	AvailabilityCheckMS  int64 `json:"availability_check_ms"`
	AttemptExecutionMS   int64 `json:"attempt_execution_ms"`
	ResultValidationMS   int64 `json:"result_validation_ms"`
	TotalMS              int64 `json:"total_ms"`
}

type ReviewerProvenance struct {
	ReviewerID string `json:"reviewer_id"`
	Model      string `json:"model"`
	Effort     string `json:"effort"`
	Harness    string `json:"harness"`
	Transport  string `json:"transport"`
}

type AttemptRecord struct {
	Number       int                 `json:"number"`
	Outcome      AttemptOutcome      `json:"outcome"`
	Provenance   ReviewerProvenance  `json:"provenance"`
	Diagnostic   string              `json:"diagnostic,omitempty"`
	RawOutput    string              `json:"raw_output,omitempty"`
	RetryAfterMS int64               `json:"retry_after_ms,omitempty"`
	Artifacts    []ArtifactReference `json:"artifacts,omitempty"`
	StartedAt    time.Time           `json:"started_at"`
	CompletedAt  time.Time           `json:"completed_at"`
}

type ArtifactReference struct {
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Digest    string `json:"digest"`
	Truncated bool   `json:"truncated"`
}

type PassRecord struct {
	Name     string          `json:"name"`
	Required bool            `json:"required"`
	Attempts []AttemptRecord `json:"attempts"`
}

type ResultStatus string

const (
	ResultClean    ResultStatus = "clean"
	ResultFindings ResultStatus = "findings"
)

type ReviewResult struct {
	Status   ResultStatus `json:"status"`
	Summary  string       `json:"summary"`
	Findings []Finding    `json:"findings,omitempty"`
	Raw      string       `json:"raw"`

	legacyFindingCount int
}

type Finding struct {
	Ordinal  int    `json:"ordinal"`
	Severity string `json:"severity"`
	Category string `json:"category"`
	Location string `json:"location"`
	Failure  string `json:"failure"`
	Evidence string `json:"evidence"`
	Fix      string `json:"fix"`
	Test     string `json:"test"`
}

func (result ReviewResult) FindingCount() int {
	if result.Findings != nil {
		return len(result.Findings)
	}
	return result.legacyFindingCount
}

func (result ReviewResult) MarshalJSON() ([]byte, error) {
	type reviewResultJSON struct {
		Status       ResultStatus `json:"status"`
		Summary      string       `json:"summary"`
		FindingCount int          `json:"finding_count"`
		Findings     *[]Finding   `json:"findings,omitempty"`
		Raw          string       `json:"raw"`
	}
	var findings *[]Finding
	if result.Findings != nil {
		findings = &result.Findings
	}
	return json.Marshal(reviewResultJSON{
		Status:       result.Status,
		Summary:      result.Summary,
		FindingCount: result.FindingCount(),
		Findings:     findings,
		Raw:          result.Raw,
	})
}

func (result *ReviewResult) UnmarshalJSON(payload []byte) error {
	type reviewResultJSON struct {
		Status       ResultStatus    `json:"status"`
		Summary      string          `json:"summary"`
		FindingCount int             `json:"finding_count"`
		Findings     json.RawMessage `json:"findings"`
		Raw          string          `json:"raw"`
	}
	var decoded reviewResultJSON
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return err
	}
	result.Status = decoded.Status
	result.Summary = decoded.Summary
	result.Raw = decoded.Raw
	if decoded.Findings == nil || string(decoded.Findings) == "null" {
		result.Findings = nil
		result.legacyFindingCount = decoded.FindingCount
		return nil
	}
	if err := json.Unmarshal(decoded.Findings, &result.Findings); err != nil {
		return err
	}
	result.legacyFindingCount = 0
	return nil
}

type ReviewRecord struct {
	SchemaVersion   int                `json:"schema_version"`
	ID              ReviewID           `json:"id"`
	ReplaysReviewID *ReviewID          `json:"replays_review_id,omitempty"`
	Lifecycle       Lifecycle          `json:"lifecycle"`
	Subject         ReviewSubject      `json:"subject"`
	ProfileRevision ProfileRevision    `json:"profile_revision"`
	ProfileSnapshot ProfileSnapshot    `json:"profile_snapshot"`
	Passes          []PassRecord       `json:"passes"`
	Result          *ReviewResult      `json:"result,omitempty"`
	Termination     *ReviewTermination `json:"termination,omitempty"`
	Runtime         *RuntimeProvenance `json:"runtime,omitempty"`
	Timings         *ReviewTimings     `json:"timings,omitempty"`
	// IncompleteCause is retained only when loading schema-v1 Review Records.
	IncompleteCause string    `json:"incomplete_cause,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (r ReviewRecord) AttemptCount() int {
	count := 0
	for _, pass := range r.Passes {
		count += len(pass.Attempts)
	}
	return count
}
