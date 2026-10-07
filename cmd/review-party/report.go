package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

type reviewReport struct {
	Bundle   *bundleSummary `json:"bundle,omitempty"`
	Reviews  []reviewEntry  `json:"reviews"`
	Feedback string         `json:"feedback,omitempty"`
}

type bundleSummary struct {
	ID               model.ReviewBundleID     `json:"id"`
	Lifecycle        model.Lifecycle          `json:"lifecycle"`
	Termination      *model.BundleTermination `json:"termination,omitempty"`
	Revision         string                   `json:"revision"`
	Repository       string                   `json:"repository"`
	SubjectKind      model.SubjectKind        `json:"subject_kind"`
	SubjectIdentity  string                   `json:"subject_identity"`
	Selection        *model.BundleSelection   `json:"selection,omitempty"`
	Warnings         []model.BundleWarning    `json:"warnings,omitempty"`
	Deduplicated     []model.SkippedDuplicate `json:"deduplicated,omitempty"`
	ConcurrencyLimit int                      `json:"concurrency_limit"`
}

type reviewEntry struct {
	ID              model.ReviewID            `json:"id"`
	Lifecycle       model.Lifecycle           `json:"lifecycle"`
	Status          model.ResultStatus        `json:"status,omitempty"`
	Termination     *model.ReviewTermination  `json:"termination,omitempty"`
	Summary         string                    `json:"summary,omitempty"`
	Findings        []reportFinding           `json:"findings"`
	Misses          []model.Miss              `json:"misses"`
	Profile         profileSummary            `json:"profile"`
	Origin          string                    `json:"origin,omitempty"`
	Reviewer        *model.ReviewerProvenance `json:"reviewer,omitempty"`
	Subject         *subjectSummary           `json:"subject,omitempty"`
	ReplaysReviewID *model.ReviewID           `json:"replays_review_id,omitempty"`
	Record          *model.ReviewRecord       `json:"record,omitempty"`
	ReadError       string                    `json:"read_error,omitempty"`
}

// reportFinding is a Finding with the Caller's current Verdict on it, if any.
type reportFinding struct {
	model.Finding
	Verdict *verdictView `json:"verdict,omitempty"`
}

type verdictView struct {
	Value  model.Verdict `json:"value"`
	Reason string        `json:"reason"`
}

type profileSummary struct {
	Name                   string `json:"name"`
	Scope                  string `json:"scope,omitempty"`
	Revision               string `json:"revision,omitempty"`
	Source                 string `json:"source,omitempty"`
	ResultContractRevision string `json:"result_contract_revision,omitempty"`
}

type subjectSummary struct {
	Kind             model.SubjectKind   `json:"kind"`
	Repository       string              `json:"repository"`
	Identity         string              `json:"identity"`
	BaseObject       string              `json:"base_object,omitempty"`
	HeadObject       string              `json:"head_object,omitempty"`
	ChangedPathCount int                 `json:"changed_path_count"`
	Facts            *model.SubjectFacts `json:"facts,omitempty"`
}

type reviewLoader interface {
	Inspect(context.Context, model.ReviewID) (model.ReviewRecord, error)
}

type annotationLoader interface {
	Misses(context.Context, store.MissQuery) ([]model.Miss, error)
	FindingVerdicts(context.Context, store.VerdictQuery) ([]model.FindingVerdict, error)
}

type reportOptions struct {
	format        string
	configuration string
}

func recordReport(record model.ReviewRecord, full bool) reviewReport {
	return reviewReport{Reviews: []reviewEntry{recordEntry(record, full)}}
}

func bundleReport(ctx context.Context, loader reviewLoader, bundle model.ReviewBundle, full bool) reviewReport {
	entries := make([]reviewEntry, 0, len(bundle.Members))
	for _, member := range bundle.Members {
		entries = append(entries, memberEntry(ctx, loader, member, full))
	}
	return reviewReport{Bundle: summarizeBundle(bundle), Reviews: entries}
}

func memberEntry(ctx context.Context, loader reviewLoader, member model.BundleMember, full bool) reviewEntry {
	record, err := loader.Inspect(ctx, member.ReviewID)
	if err != nil {
		return reviewEntry{
			ID:        member.ReviewID,
			Lifecycle: engine.LifecycleUnreadable,
			Findings:  []reportFinding{},
			Misses:    []model.Miss{},
			Profile:   profileSummary{Name: member.Profile, Scope: member.Scope, Revision: member.ProfileRevision},
			Origin:    member.Origin,
			ReadError: err.Error(),
		}
	}
	entry := recordEntry(record, full)
	entry.Profile.Scope = member.Scope
	entry.Origin = member.Origin
	return entry
}

// annotate attaches each review's misses and the current Verdict on each of its
// findings. It reads both before changing the report, so a failure leaves the
// report unannotated.
func (report reviewReport) annotate(ctx context.Context, loader annotationLoader) error {
	verdicts, err := report.loadVerdicts(ctx, loader)
	if err != nil {
		return err
	}
	misses, missFailures, err := report.loadMisses(ctx, loader)
	if err != nil {
		return err
	}
	for index := range report.Reviews {
		entry := &report.Reviews[index]
		entry.attachVerdicts(verdicts)
		for _, miss := range misses {
			if miss.ReviewID == entry.ID {
				entry.Misses = append(entry.Misses, miss)
			}
		}
		if failure, failed := missFailures[entry.ID]; failed {
			entry.ReadError += "; load misses: " + failure
		}
	}
	return nil
}

// loadVerdicts reads the Verdicts on every readable review in one query. An
// unreadable review shows no findings, and an empty query would read every
// review's Verdicts, so neither is asked for.
func (report reviewReport) loadVerdicts(ctx context.Context, loader annotationLoader) ([]model.FindingVerdict, error) {
	var readable []model.ReviewID
	for _, entry := range report.Reviews {
		if entry.Lifecycle != engine.LifecycleUnreadable {
			readable = append(readable, entry.ID)
		}
	}
	if len(readable) == 0 {
		return nil, nil
	}
	verdicts, err := loader.FindingVerdicts(ctx, store.VerdictQuery{ReviewIDs: readable})
	if err != nil {
		return nil, fmt.Errorf("load finding verdicts: %w", err)
	}
	return verdicts, nil
}

// loadMisses reads every review's misses in one query. That query fails as a
// whole when one bundle member's ledger row is corrupt, so on failure it reads
// each review alone: an unreadable member's failure is returned by review ID
// to join its read error, and any other failure fails the report.
func (report reviewReport) loadMisses(ctx context.Context, loader annotationLoader) ([]model.Miss, map[model.ReviewID]string, error) {
	ids := make([]model.ReviewID, 0, len(report.Reviews))
	for _, entry := range report.Reviews {
		ids = append(ids, entry.ID)
	}
	if misses, err := loader.Misses(ctx, store.MissQuery{ReviewIDs: ids}); err == nil {
		return misses, nil, nil
	}
	var misses []model.Miss
	failures := map[model.ReviewID]string{}
	for _, entry := range report.Reviews {
		own, err := loader.Misses(ctx, store.MissQuery{ReviewIDs: []model.ReviewID{entry.ID}})
		switch {
		case err == nil:
			misses = append(misses, own...)
		case entry.Lifecycle == engine.LifecycleUnreadable:
			failures[entry.ID] = err.Error()
		default:
			return nil, nil, fmt.Errorf("load misses for review %s: %w", entry.ID, err)
		}
	}
	return misses, failures, nil
}

// attachVerdicts shows each current Verdict on its finding. A stale Verdict
// judged text the finding no longer holds, so that finding shows as unjudged.
func (entry *reviewEntry) attachVerdicts(verdicts []model.FindingVerdict) {
	current := map[int]*verdictView{}
	for _, verdict := range verdicts {
		if verdict.ReviewID != entry.ID || verdict.Stale {
			continue
		}
		current[verdict.Ordinal] = &verdictView{Value: verdict.Verdict, Reason: verdict.Reason}
	}
	for index := range entry.Findings {
		entry.Findings[index].Verdict = current[entry.Findings[index].Ordinal]
	}
}

// feedbackHint tells the Caller how to judge the report's findings: one
// finding record command per Review with an unjudged finding, since bundle
// members number their findings separately.
func feedbackHint(report reviewReport, configuration string) string {
	var commands []string
	for _, entry := range report.Reviews {
		if entry.hasUnjudgedFinding() {
			commands = append(commands, "review-party finding record "+string(entry.ID)+configurationArgument(configuration)+" <<'EOF'\nN accept|reject|defer REASON\nEOF")
		}
	}
	return strings.Join(commands, "\n")
}

func (entry reviewEntry) hasUnjudgedFinding() bool {
	for _, finding := range entry.Findings {
		if finding.Verdict == nil {
			return true
		}
	}
	return false
}

func printReport(output io.Writer, report reviewReport, options reportOptions) error {
	report.Feedback = feedbackHint(report, options.configuration)
	switch options.format {
	case "json":
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	case "human":
		return printHumanReport(output, report, options.configuration)
	default:
		return fmt.Errorf("unknown output format %q", options.format)
	}
}

func (report reviewReport) incomplete() bool {
	if report.Bundle != nil {
		return report.Bundle.Lifecycle == model.LifecycleIncomplete
	}
	for _, entry := range report.Reviews {
		if entry.Lifecycle == model.LifecycleIncomplete {
			return true
		}
	}
	return false
}

func (report reviewReport) readFailure() error {
	var failures []error
	for _, entry := range report.Reviews {
		if entry.Lifecycle == engine.LifecycleUnreadable {
			failures = append(failures, fmt.Errorf("read bundle member %s:%s review %s: %s", entry.Profile.Scope, entry.Profile.Name, entry.ID, entry.ReadError))
		}
	}
	return errors.Join(failures...)
}

func recordEntry(record model.ReviewRecord, full bool) reviewEntry {
	provenance := latestProvenance(record)
	entry := reviewEntry{
		ID:          record.ID,
		Lifecycle:   record.Lifecycle,
		Termination: record.Termination,
		Findings:    []reportFinding{},
		Misses:      []model.Miss{},
		Profile: profileSummary{
			Name:                   record.ProfileRevision.Name,
			Revision:               record.ProfileRevision.Revision,
			Source:                 record.ProfileRevision.Source,
			ResultContractRevision: record.ProfileRevision.ResultContract,
		},
		Reviewer: &provenance,
		Subject: &subjectSummary{
			Kind:             record.Subject.Kind,
			Repository:       record.Subject.Repository,
			Identity:         record.Subject.Identity,
			BaseObject:       record.Subject.BaseObject,
			HeadObject:       record.Subject.HeadObject,
			ChangedPathCount: len(record.Subject.ChangedPaths),
			Facts:            record.Subject.Facts,
		},
		ReplaysReviewID: record.ReplaysReviewID,
	}
	if record.Result != nil {
		entry.Status = record.Result.Status
		entry.Summary = record.Result.Summary
		for _, finding := range record.Result.Findings {
			entry.Findings = append(entry.Findings, reportFinding{Finding: finding})
		}
	}
	if full {
		entry.Record = &record
	}
	return entry
}

func summarizeBundle(bundle model.ReviewBundle) *bundleSummary {
	return &bundleSummary{
		ID:               bundle.ID,
		Lifecycle:        bundle.Lifecycle,
		Termination:      bundle.Termination,
		Revision:         bundle.Revision,
		Repository:       bundle.Repository,
		SubjectKind:      bundle.SubjectKind,
		SubjectIdentity:  bundle.SubjectIdentity,
		Selection:        bundle.Selection,
		Warnings:         bundle.Warnings,
		Deduplicated:     bundle.Deduplicated,
		ConcurrencyLimit: bundle.ConcurrencyLimit,
	}
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
