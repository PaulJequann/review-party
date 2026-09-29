package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"reviewparty/internal/engine"
	"reviewparty/internal/model"
	"reviewparty/internal/store"
)

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

type reviewEntry struct {
	ID              model.ReviewID            `json:"id"`
	Lifecycle       model.Lifecycle           `json:"lifecycle"`
	Status          model.ResultStatus        `json:"status,omitempty"`
	Termination     *model.ReviewTermination  `json:"termination,omitempty"`
	Summary         string                    `json:"summary,omitempty"`
	Findings        []model.Finding           `json:"findings"`
	Misses          []model.Miss              `json:"misses"`
	Profile         profileSummary            `json:"profile"`
	Origin          string                    `json:"origin,omitempty"`
	Reviewer        *model.ReviewerProvenance `json:"reviewer,omitempty"`
	Subject         *subjectSummary           `json:"subject,omitempty"`
	ReplaysReviewID *model.ReviewID           `json:"replays_review_id,omitempty"`
	Record          *model.ReviewRecord       `json:"record,omitempty"`
	ReadError       string                    `json:"read_error,omitempty"`
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

type missLoader interface {
	Misses(context.Context, store.MissQuery) ([]model.Miss, error)
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
			Findings:  []model.Finding{},
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

func (report reviewReport) attachMisses(ctx context.Context, loader missLoader) error {
	for index, entry := range report.Reviews {
		misses, err := loader.Misses(ctx, store.MissQuery{ReviewID: entry.ID})
		if err != nil && entry.Lifecycle == engine.LifecycleUnreadable {
			report.Reviews[index].ReadError += "; load misses: " + err.Error()
			continue
		}
		if err != nil {
			return fmt.Errorf("load misses for review %s: %w", entry.ID, err)
		}
		report.Reviews[index].Misses = append(report.Reviews[index].Misses, misses...)
	}
	return nil
}

func printReport(output io.Writer, report reviewReport, options reportOptions) error {
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
		Findings:    []model.Finding{},
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
		if record.Result.Findings != nil {
			entry.Findings = record.Result.Findings
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
