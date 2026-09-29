package main

import (
	"context"
	"fmt"

	"reviewparty/internal/model"
)

// reviewReport is the one result shape run, inspect, and replay print: every
// review the command produced, with its findings inline, plus the bundle that
// grouped them when there was one.
type reviewReport struct {
	Bundle  *bundleSummary `json:"bundle,omitempty"`
	Reviews []reviewEntry  `json:"reviews"`
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

// reviewEntry is one review. A bundle member that never started has no ID,
// reviewer, or subject. Record is the complete ledger record, present only
// when the caller asked for --full.
type reviewEntry struct {
	ID              model.ReviewID            `json:"id,omitempty"`
	Lifecycle       model.Lifecycle           `json:"lifecycle"`
	Status          model.ResultStatus        `json:"status,omitempty"`
	Termination     *model.ReviewTermination  `json:"termination,omitempty"`
	Summary         string                    `json:"summary,omitempty"`
	Findings        []model.Finding           `json:"findings"`
	Profile         profileSummary            `json:"profile"`
	Origin          string                    `json:"origin,omitempty"`
	Reviewer        *model.ReviewerProvenance `json:"reviewer,omitempty"`
	Subject         *subjectSummary           `json:"subject,omitempty"`
	ReplaysReviewID *model.ReviewID           `json:"replays_review_id,omitempty"`
	Record          *model.ReviewRecord       `json:"record,omitempty"`
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

type reportOptions struct {
	format        string
	configuration string
}

func recordReport(record model.ReviewRecord, full bool) reviewReport {
	return reviewReport{Reviews: []reviewEntry{recordEntry(record, full)}}
}

func bundleReport(ctx context.Context, loader reviewLoader, bundle model.ReviewBundle, full bool) (reviewReport, error) {
	entries := make([]reviewEntry, 0, len(bundle.Members))
	for _, member := range bundle.Members {
		if member.ReviewID == "" {
			entries = append(entries, unstartedEntry(member))
			continue
		}
		record, err := loader.Inspect(ctx, member.ReviewID)
		if err != nil {
			return reviewReport{}, fmt.Errorf("load bundle member %s:%s review %s: %w", member.Scope, member.Profile, member.ReviewID, err)
		}
		entry := recordEntry(record, full)
		entry.Profile.Scope = member.Scope
		entry.Origin = member.Origin
		entries = append(entries, entry)
	}
	return reviewReport{Bundle: summarizeBundle(bundle), Reviews: entries}, nil
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

func recordEntry(record model.ReviewRecord, full bool) reviewEntry {
	provenance := latestProvenance(record)
	entry := reviewEntry{
		ID:          record.ID,
		Lifecycle:   record.Lifecycle,
		Termination: record.Termination,
		Findings:    []model.Finding{},
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
		if record.Result.Findings != nil {
			entry.Findings = record.Result.Findings
		}
	}
	if full {
		entry.Record = &record
	}
	return entry
}

func unstartedEntry(member model.BundleMember) reviewEntry {
	return reviewEntry{
		Lifecycle: member.Lifecycle,
		Findings:  []model.Finding{},
		Profile:   profileSummary{Name: member.Profile, Scope: member.Scope, Revision: member.ProfileRevision},
		Origin:    member.Origin,
	}
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
