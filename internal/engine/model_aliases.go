package engine

import "reviewparty/internal/model"

// Type aliases for model package — allows existing engine code to use
// unqualified names (ReviewRecord, Lifecycle, etc.) without changing every
// call site. The definitions live in internal/model; these are just aliases.
type ReviewID = model.ReviewID
type Lifecycle = model.Lifecycle
type SubjectKind = model.SubjectKind
type SubjectReference = model.SubjectReference
type ReviewSelection = model.ReviewSelection
type ReplaySelection = model.ReplaySelection
type ProfileSelection = model.ProfileSelection
type ReviewSubject = model.ReviewSubject
type SubjectFacts = model.SubjectFacts
type ProfileRevision = model.ProfileRevision
type ProfileSnapshot = model.ProfileSnapshot
type Capability = model.Capability
type ReviewPassRevision = model.ReviewPassRevision
type ProfileSummary = model.ProfileSummary
type ProfileExplanation = model.ProfileExplanation
type AttemptOutcome = model.AttemptOutcome
type TerminationCategory = model.TerminationCategory
type ExecutionPhase = model.ExecutionPhase
type ReviewTermination = model.ReviewTermination
type RuntimeProvenance = model.RuntimeProvenance
type ReviewTimings = model.ReviewTimings
type ReviewerProvenance = model.ReviewerProvenance
type AttemptRecord = model.AttemptRecord
type ArtifactReference = model.ArtifactReference
type PassRecord = model.PassRecord
type ResultStatus = model.ResultStatus
type ReviewResult = model.ReviewResult
type ReviewRecord = model.ReviewRecord

// Constants
const (
	LegacyReviewRecordSchemaVersion  = model.LegacyReviewRecordSchemaVersion
	CurrentReviewRecordSchemaVersion = model.CurrentReviewRecordSchemaVersion

	LifecyclePending    = model.LifecyclePending
	LifecycleRunning    = model.LifecycleRunning
	LifecycleCompleted  = model.LifecycleCompleted
	LifecycleIncomplete = model.LifecycleIncomplete

	SubjectWorkingChanges = model.SubjectWorkingChanges
	SubjectCommittedRange = model.SubjectCommittedRange

	CapabilityRepositoryRead           = model.CapabilityRepositoryRead
	CapabilityRepositorySearch         = model.CapabilityRepositorySearch
	CapabilityRepositoryMutationDenied = model.CapabilityRepositoryMutationDenied
	CapabilityShellDenied              = model.CapabilityShellDenied
	CapabilityWebDenied                = model.CapabilityWebDenied

	AttemptCompleted           = model.AttemptCompleted
	AttemptTransientFailure    = model.AttemptTransientFailure
	AttemptReviewerUnavailable = model.AttemptReviewerUnavailable
	AttemptInvalidResult       = model.AttemptInvalidResult
	AttemptCancelled           = model.AttemptCancelled
	AttemptUnknownFailure      = model.AttemptUnknownFailure

	TerminationReviewerUnavailable     = model.TerminationReviewerUnavailable
	TerminationAuthenticationFailure   = model.TerminationAuthenticationFailure
	TerminationDeadlineExceeded        = model.TerminationDeadlineExceeded
	TerminationCancelled               = model.TerminationCancelled
	TerminationTransportFailure        = model.TerminationTransportFailure
	TerminationMalformedOutput         = model.TerminationMalformedOutput
	TerminationResultValidationFailure = model.TerminationResultValidationFailure
	TerminationUnknownFailure          = model.TerminationUnknownFailure

	PhaseAvailabilityCheck = model.PhaseAvailabilityCheck
	PhaseHarnessLaunch     = model.PhaseHarnessLaunch
	PhaseReviewerExecution = model.PhaseReviewerExecution
	PhaseOutputCapture     = model.PhaseOutputCapture
	PhaseOutputDecode      = model.PhaseOutputDecode
	PhaseResultValidation  = model.PhaseResultValidation

	ResultClean    = model.ResultClean
	ResultFindings = model.ResultFindings
)

// Lower-case backward-compat aliases for code that still uses private constant names
const legacyReviewRecordSchemaVersion = model.LegacyReviewRecordSchemaVersion
const currentReviewRecordSchemaVersion = model.CurrentReviewRecordSchemaVersion

// Functions / vars that were in model
var WorkingChanges = model.WorkingChanges
var CommittedRange = model.CommittedRange

// Store aliases (model constants already covered, but also need store types)
// These will be defined in store_aliases.go

// Result aliases
// (maxResultSize is exported as MaxResultSize from result)
