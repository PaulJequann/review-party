package engine

// A Checkpoint decision layers the declared Checkpoint over Coverage:
// exemptions narrow the content first, then Coverage, then a Waiver of the
// exact remaining content. An undeclared Checkpoint is plain Coverage.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/subject"
)

type CheckpointState string

const (
	CheckpointCovered CheckpointState = "covered"
	CheckpointRunning CheckpointState = "running"
	CheckpointMissing CheckpointState = "missing"
	CheckpointExempt  CheckpointState = "exempt"
	CheckpointWaived  CheckpointState = "waived"
)

// Passes reports whether the Checkpoint accepts the change in this state.
func (state CheckpointState) Passes() bool {
	return state == CheckpointCovered || state == CheckpointExempt || state == CheckpointWaived
}

type ExemptionReason string

const (
	ExemptByPaths       ExemptionReason = "paths"
	ExemptBySmallChange ExemptionReason = "small_change"
)

// CheckpointExemption explains which content the declared exemptions removed
// and, when the change is small, how many lines it changes.
type CheckpointExemption struct {
	Reason       ExemptionReason
	ExemptPaths  []string
	ChangedLines int
}

// CheckpointRequest names the Checkpoint and the content it is about to accept.
type CheckpointRequest struct {
	Repository string
	Name       configuration.CheckpointName
	Content    CoverageSubject
}

// CheckpointReport is the Checkpoint-level answer. Declaration is nil when the
// repository declares no such Checkpoint. Exemption is set when exempt paths
// removed content or the change passed as small. Waiver is set when state is
// waived. Coverage is empty when exemptions alone decided the Checkpoint.
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
	content, exemption := applyExemptions(declaration, request.Content)
	report.Exemption = exemption
	report.WaiverKey = model.WaiverKey{Checkpoint: string(request.Name), ContentDigest: model.ContentChangesDigest(content.Changes)}
	if exemption != nil && exemption.Reason != "" {
		report.State = CheckpointExempt
		return report, nil
	}
	report.Coverage, err = conductor.checkCoverage(request.Repository, content, declaration)
	if err != nil {
		return CheckpointReport{}, err
	}
	report.State = checkpointStateOf(report.Coverage)
	if !declared || report.State.Passes() {
		return report, nil
	}
	return conductor.applyWaiver(report)
}

func checkpointStateOf(coverage CoverageReport) CheckpointState {
	if coverage.Covered {
		return CheckpointCovered
	}
	for _, profile := range coverage.Profiles {
		if profile.State == CoverageMissing {
			return CheckpointMissing
		}
	}
	return CheckpointRunning
}

func (conductor *Conductor) applyWaiver(report CheckpointReport) (CheckpointReport, error) {
	ledger, ok := conductor.store.(waiverLedger)
	if !ok {
		return CheckpointReport{}, errors.New("checkpoint waivers require the SQLite ledger")
	}
	waiver, found, err := ledger.CheckpointWaiver(report.WaiverKey)
	if err != nil {
		return CheckpointReport{}, ledgerStateError(err)
	}
	if found {
		report.State = CheckpointWaived
		report.Waiver = &waiver
	}
	return report, nil
}

// applyExemptions removes exempt paths from the whole set and from each
// commit, dropping commits left empty. The exemption is nil when the
// declaration has nothing to report for this content; a non-empty Reason
// means the exemption alone passes the Checkpoint. Empty content needs no
// exemption: Coverage already accepts it.
func applyExemptions(declaration configuration.Checkpoint, content CoverageSubject) (CoverageSubject, *CheckpointExemption) {
	if len(content.Changes) == 0 {
		return content, nil
	}
	kept, exempt := partitionExempt(declaration, content.Changes)
	filtered := CoverageSubject{Changes: kept}
	for _, commit := range content.Commits {
		if changes, _ := partitionExempt(declaration, commit.Changes); len(changes) > 0 {
			filtered.Commits = append(filtered.Commits, subject.CommitContentChanges{Commit: commit.Commit, Changes: changes})
		}
	}
	exemption := CheckpointExemption{ExemptPaths: exempt}
	switch {
	case len(filtered.Changes) == 0:
		exemption.Reason = ExemptByPaths
	case declaration.SmallChangeLines > 0:
		exemption.measure(declaration.SmallChangeLines, filtered.Changes, content.Lines)
	}
	if !exemption.reportable() {
		return filtered, nil
	}
	return filtered, &exemption
}

// measure records the size of the non-exempt change, and passes it when it
// is within the limit. An unmeasurable change records nothing.
func (exemption *CheckpointExemption) measure(limit int, changes []model.ContentChange, lines subject.LineCounts) {
	total, measurable := changedLines(changes, lines)
	if !measurable {
		return
	}
	exemption.ChangedLines = total
	if total <= limit {
		exemption.Reason = ExemptBySmallChange
	}
}

func (exemption CheckpointExemption) reportable() bool {
	return len(exemption.ExemptPaths) > 0 || exemption.Reason != "" || exemption.ChangedLines > 0
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

// changedLines totals added and deleted lines over the given paths. A binary
// path, or a path git did not count, makes the change unmeasurable.
func changedLines(changes []model.ContentChange, lines subject.LineCounts) (int, bool) {
	total := 0
	for _, change := range changes {
		count, counted := lines[change.Path]
		if !counted || count.Binary {
			return 0, false
		}
		total += count.Added + count.Deleted
	}
	return total, true
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
	waiver := model.CheckpointWaiver{ID: model.WaiverID(id), Key: key, Reason: strings.TrimSpace(request.Reason), WaivedBy: request.WaivedBy, CreatedAt: conductor.now().UTC()}
	if err := ledger.RecordCheckpointWaiver(waiver); err != nil {
		return model.CheckpointWaiver{}, ledgerStateError(err)
	}
	return waiver, nil
}
