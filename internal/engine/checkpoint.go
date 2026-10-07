package engine

// A Checkpoint decision layers the declared Checkpoint over Coverage: exempt
// paths narrow the content first, then each Profile's reviewed state is
// measured against the unreviewed-lines allowance and the review budget,
// under the judged requirement the Verdicts on the chain's Findings, then a
// Waiver of the exact unreviewed delta. An undeclared Checkpoint allows no
// unreviewed lines and has no budget.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

type CheckpointState string

const (
	CheckpointCovered  CheckpointState = "covered"
	CheckpointResidual CheckpointState = "residual"
	CheckpointRunning  CheckpointState = "running"
	CheckpointSpent    CheckpointState = "spent"
	CheckpointMissing  CheckpointState = "missing"
	CheckpointUnjudged CheckpointState = "unjudged"
	CheckpointExempt   CheckpointState = "exempt"
	CheckpointWaived   CheckpointState = "waived"
)

// Passes reports whether the Checkpoint accepts the change in this state.
func (state CheckpointState) Passes() bool {
	return state == CheckpointCovered || state == CheckpointResidual || state == CheckpointExempt || state == CheckpointWaived
}

// CheckpointExemption names the changed paths the declaration exempts.
type CheckpointExemption struct {
	ExemptPaths []string
}

// CheckpointRequest names the Checkpoint and the content it is about to accept.
type CheckpointRequest struct {
	Repository string
	Name       configuration.CheckpointName
	Content    []model.ContentChange
}

// CheckpointReport is the Checkpoint-level answer. Declaration is nil when the
// repository declares no such Checkpoint. Exemption is set when exempt paths
// removed content. Waiver is set when state is waived. Coverage is empty when
// exemptions alone decided the Checkpoint.
type CheckpointReport struct {
	Name        configuration.CheckpointName
	Declaration *configuration.Checkpoint
	State       CheckpointState
	Exemption   *CheckpointExemption
	Waiver      *model.CheckpointWaiver
	Coverage    CoverageReport
	WaiverKey   model.WaiverKey
}

type waiverLedger interface {
	CheckpointWaiver(model.WaiverKey) (model.CheckpointWaiver, bool, error)
	RecordCheckpointWaiver(model.CheckpointWaiver) error
	CheckpointWaiversSince(repository string, since time.Time) ([]model.CheckpointWaiver, error)
}

// CheckCheckpoint decides one Checkpoint for the content.
func (conductor *Conductor) CheckCheckpoint(_ context.Context, request CheckpointRequest) (CheckpointReport, error) {
	checkpoints, err := conductor.configuration.Checkpoints(configuration.Repository(request.Repository))
	if err != nil {
		return CheckpointReport{}, err
	}
	report := CheckpointReport{Name: request.Name}
	declaration, declared := checkpoints[request.Name]
	if declared {
		report.Declaration = &declaration
	}
	content, exempt := partitionExempt(declaration, request.Content)
	if len(exempt) > 0 {
		report.Exemption = &CheckpointExemption{ExemptPaths: exempt}
	}
	if len(content) == 0 && len(exempt) > 0 {
		report.State = CheckpointExempt
		return report, nil
	}
	report.Coverage, err = conductor.checkCoverage(request.Repository, content, declaration, conductor.newFindingJudge(report.Declaration))
	if err != nil {
		return CheckpointReport{}, err
	}
	report.State = checkpointStateOf(report.Coverage)
	report.WaiverKey = waiverKeyOf(request.Name, content, report.Coverage)
	return conductor.applyWaiver(report)
}

// checkpointStateOf takes the first Profile state in refusal order: a Profile
// over its allowance decides before one within it, so a Review still running
// or a spent budget is reported before a Finding without a verdict.
func checkpointStateOf(coverage CoverageReport) CheckpointState {
	for _, state := range []CoverageState{CoverageRunning, CoverageSpent, CoverageMissing, CoverageUnjudged, CoverageResidual} {
		if slices.ContainsFunc(coverage.Profiles, func(profile ProfileCoverage) bool { return profile.State == state }) {
			return CheckpointState(state)
		}
	}
	return CheckpointCovered
}

// waiverKeyOf keys a Waiver on the union of every Profile's unreviewed delta.
// With nothing unreviewed, as under the judged requirement, it is keyed on the
// reviewed content whose Findings the Waiver passes.
func waiverKeyOf(name configuration.CheckpointName, content []model.ContentChange, coverage CoverageReport) model.WaiverKey {
	var union []model.ContentChange
	for _, profile := range coverage.Profiles {
		union = append(union, profile.Unreviewed...)
	}
	model.SortContentChanges(union)
	union = slices.Compact(union)
	if len(union) == 0 {
		union = content
	}
	return model.WaiverKey{Checkpoint: string(name), ContentDigest: model.ContentChangesDigest(union)}
}

// applyWaiver turns a declared Checkpoint's refusal into waived when a
// recorded waiver matches the unreviewed delta. A pass or an undeclared
// Checkpoint has nothing to waive.
func (conductor *Conductor) applyWaiver(report CheckpointReport) (CheckpointReport, error) {
	if report.Declaration == nil || report.State.Passes() {
		return report, nil
	}
	ledger, ok := conductor.store.(waiverLedger)
	if !ok {
		return CheckpointReport{}, errors.New("checkpoint waivers require the SQLite ledger")
	}
	waiver, found, err := ledger.CheckpointWaiver(report.WaiverKey)
	if err != nil {
		return CheckpointReport{}, ledgerStateError(err)
	}
	if found && WaiverPermission(report.Declaration.Waivers, waiver.WaivedBy) == nil {
		report.State = CheckpointWaived
		report.Waiver = &waiver
	}
	return report, nil
}

// partitionExempt splits changes into those the Checkpoint still requires
// Reviews for and the paths it exempts.
func partitionExempt(declaration configuration.Checkpoint, changes []model.ContentChange) ([]model.ContentChange, []string) {
	var kept []model.ContentChange
	var exempt []string
	for _, change := range changes {
		if declaration.Exempts(change.Path) {
			exempt = append(exempt, change.Path)
			continue
		}
		kept = append(kept, change)
	}
	return kept, exempt
}

// ErrWaiversNotAllowed reports a Checkpoint whose policy forbids Waivers.
var ErrWaiversNotAllowed = errors.New("waivers are not allowed")

// ErrWaiverNeedsPerson reports a human-only policy waived without terminal
// confirmation.
var ErrWaiverNeedsPerson = errors.New("a person must confirm this waiver in a terminal")

// WaiverRequest waives the exact current content of one declared Checkpoint.
type WaiverRequest struct {
	Checkpoint CheckpointRequest
	Reason     string
	WaivedBy   model.WaivedBy
}

// WaiverPermission reports whether a policy accepts a Waiver confirmed this way.
func WaiverPermission(policy configuration.WaiverPolicy, by model.WaivedBy) error {
	switch {
	case policy == configuration.WaiversNone:
		return ErrWaiversNotAllowed
	case policy == configuration.WaiversHuman && by != model.WaivedByTerminal:
		return ErrWaiverNeedsPerson
	default:
		return nil
	}
}

// WaiveCheckpoint records a Waiver when the Checkpoint does not already pass.
// A Checkpoint that passes is returned unchanged before the waiver policy is
// consulted, so waiving twice records once from any terminal.
func (conductor *Conductor) WaiveCheckpoint(ctx context.Context, request WaiverRequest) (CheckpointReport, error) {
	if strings.TrimSpace(request.Reason) == "" {
		return CheckpointReport{}, errors.New("a waiver requires a reason")
	}
	report, err := conductor.CheckCheckpoint(ctx, request.Checkpoint)
	if err != nil {
		return CheckpointReport{}, err
	}
	if report.Declaration == nil {
		return CheckpointReport{}, fmt.Errorf("checkpoint %s is not declared; declare it with review-party config checkpoint set %s", report.Name, report.Name)
	}
	if report.State.Passes() {
		return report, nil
	}
	if err := WaiverPermission(report.Declaration.Waivers, request.WaivedBy); err != nil {
		return CheckpointReport{}, fmt.Errorf("checkpoint %s: %w", report.Name, err)
	}
	waiver, err := conductor.recordWaiver(report.WaiverKey, request)
	if err != nil {
		return CheckpointReport{}, err
	}
	report.State = CheckpointWaived
	report.Waiver = &waiver
	return report, nil
}

func (conductor *Conductor) recordWaiver(key model.WaiverKey, request WaiverRequest) (model.CheckpointWaiver, error) {
	ledger, ok := conductor.store.(waiverLedger)
	if !ok {
		return model.CheckpointWaiver{}, errors.New("checkpoint waivers require the SQLite ledger")
	}
	id, err := newDomainID("cw", conductor.now())
	if err != nil {
		return model.CheckpointWaiver{}, err
	}
	waiver := model.CheckpointWaiver{ID: model.WaiverID(id), Key: key, Repository: request.Checkpoint.Repository, Reason: strings.TrimSpace(request.Reason), WaivedBy: request.WaivedBy, CreatedAt: conductor.now().UTC()}
	if err := ledger.RecordCheckpointWaiver(waiver); err != nil {
		return model.CheckpointWaiver{}, ledgerStateError(err)
	}
	return waiver, nil
}

// RecentCheckpointWaivers lists the Waivers recorded from the repository root
// within the window, newest first. A ledger that does not exist yet holds
// none, and reading never creates one.
func (conductor *Conductor) RecentCheckpointWaivers(repository string, window time.Duration) ([]model.CheckpointWaiver, error) {
	ledger, ok := conductor.store.(waiverLedger)
	if !ok {
		return nil, errors.New("checkpoint waivers require the SQLite ledger")
	}
	waivers, err := ledger.CheckpointWaiversSince(repository, conductor.now().Add(-window))
	if errors.Is(err, store.ErrReviewRecordStateNotInitialized) {
		return []model.CheckpointWaiver{}, nil
	}
	return waivers, err
}
