package reviewparty

import "time"

type ReviewID string

const (
	legacyReviewRecordSchemaVersion  = 1
	currentReviewRecordSchemaVersion = 2
)

type Lifecycle string

const (
	LifecyclePending    Lifecycle = "pending"
	LifecycleRunning    Lifecycle = "running"
	LifecycleCompleted  Lifecycle = "completed"
	LifecycleIncomplete Lifecycle = "incomplete"
)

type SubjectKind string

const SubjectWorkingChanges SubjectKind = "working-changes"

type SubjectReference struct {
	Kind SubjectKind
}

func WorkingChanges() SubjectReference {
	return SubjectReference{Kind: SubjectWorkingChanges}
}

type ReviewSelection struct {
	Repository string
	Subject    SubjectReference
	Profile    string
	Reviewer   string
	Model      string
	Effort     string
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

type ReviewSubject struct {
	Kind         SubjectKind   `json:"kind"`
	Repository   string        `json:"repository"`
	Identity     string        `json:"identity"`
	ChangedPaths []string      `json:"changed_paths"`
	Patch        string        `json:"patch"`
	Facts        *SubjectFacts `json:"facts,omitempty"`
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
	Number      int                `json:"number"`
	Outcome     AttemptOutcome     `json:"outcome"`
	Provenance  ReviewerProvenance `json:"provenance"`
	Diagnostic  string             `json:"diagnostic,omitempty"`
	RawOutput   string             `json:"raw_output,omitempty"`
	StartedAt   time.Time          `json:"started_at"`
	CompletedAt time.Time          `json:"completed_at"`
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
	Status       ResultStatus `json:"status"`
	Summary      string       `json:"summary"`
	FindingCount int          `json:"finding_count"`
	Raw          string       `json:"raw"`
}

type ReviewRecord struct {
	SchemaVersion   int                `json:"schema_version"`
	ID              ReviewID           `json:"id"`
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
