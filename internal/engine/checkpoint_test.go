package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/subject"
)

var (
	readmeOld = []model.ContentChange{{Path: "README.md", Before: model.ZeroObjectID, After: "3333333333333333333333333333333333333333"}}
	readmeNew = []model.ContentChange{{Path: "README.md", Before: model.ZeroObjectID, After: "4444444444444444444444444444444444444444"}}
)

const bugsSource = "global:profiles/bugs"

func joined(sets ...[]model.ContentChange) []model.ContentChange {
	var all []model.ContentChange
	for _, set := range sets {
		all = append(all, set...)
	}
	return all
}

func newCheckpointConductor(t *testing.T, fake *coverageStore, checkpoint *configuration.Checkpoint) (*Conductor, string) {
	t.Helper()
	conductor, repository := newCoverageConductor(t, fake, nil)
	document := map[string]any{
		"schema_version": 1,
		"reviews":        configuration.ReviewSelection{ConcurrencyLimit: 1, Global: []configuration.SelectionItem{{Profile: "bugs"}}, Repository: []configuration.SelectionItem{}},
	}
	if checkpoint != nil {
		document["checkpoints"] = map[configuration.CheckpointName]configuration.Checkpoint{configuration.CheckpointPrePush: *checkpoint}
	}
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repository, ".reviewparty", "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	return conductor, repository
}

func exemptMarkdown() *configuration.Checkpoint {
	checkpoint := configuration.NewCheckpoint()
	checkpoint.ExemptPaths = []string{"*.md"}
	return &checkpoint
}

func checkPrePush(t *testing.T, conductor *Conductor, repository string, content CoverageSubject) CheckpointReport {
	t.Helper()
	report, err := conductor.CheckCheckpoint(context.Background(), CheckpointRequest{Repository: repository, Name: configuration.CheckpointPrePush, Content: content})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// checkpointOutcome is the part of a CheckpointReport a Caller acts on.
type checkpointOutcome struct {
	State     CheckpointState
	Declared  bool
	Exemption *CheckpointExemption
	ReviewIDs []model.ReviewID
	Waiver    string
}

func outcomeOf(report CheckpointReport) checkpointOutcome {
	outcome := checkpointOutcome{State: report.State, Declared: report.Declaration != nil, Exemption: report.Exemption}
	for _, profile := range report.Coverage.Profiles {
		outcome.ReviewIDs = append(outcome.ReviewIDs, profile.ReviewIDs...)
	}
	if report.Waiver != nil {
		outcome.Waiver = string(report.Waiver.WaivedBy) + ": " + report.Waiver.Reason
	}
	return outcome
}

func assertOutcome(t *testing.T, report CheckpointReport, want checkpointOutcome) {
	t.Helper()
	if got := outcomeOf(report); !reflect.DeepEqual(got, want) {
		t.Fatalf("outcome = %+v (exemption %+v), want %+v (exemption %+v)", got, got.Exemption, want, want.Exemption)
	}
}

func TestUndeclaredCheckpointComparesWholeSets(t *testing.T) {
	fake := &coverageStore{reviews: []recordedCoverage{recorded(bugsSource, "rp_code", model.LifecycleCompleted, coverageFirst)}}
	conductor, repository := newCheckpointConductor(t, fake, nil)

	report := checkPrePush(t, conductor, repository, CoverageSubject{Changes: joined(coverageFirst, readmeNew)})
	assertOutcome(t, report, checkpointOutcome{State: CheckpointMissing})
}

func TestExemptPathsFilterTheCheckpointAndTheReview(t *testing.T) {
	for _, test := range []struct {
		name   string
		review []model.ContentChange
	}{
		{"review saw an older exempt file", joined(coverageFirst, readmeOld)},
		{"review saw no exempt file", coverageFirst},
		{"review saw the same exempt file", joined(coverageFirst, readmeNew)},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &coverageStore{reviews: []recordedCoverage{recorded(bugsSource, "rp_code", model.LifecycleCompleted, test.review)}}
			conductor, repository := newCheckpointConductor(t, fake, exemptMarkdown())

			report := checkPrePush(t, conductor, repository, CoverageSubject{Changes: joined(coverageFirst, readmeNew)})
			assertOutcome(t, report, checkpointOutcome{
				State: CheckpointCovered, Declared: true, ReviewIDs: []model.ReviewID{"rp_code"},
				Exemption: &CheckpointExemption{ExemptPaths: []string{"README.md"}},
			})
		})
	}
}

func TestExemptPathsNeverWidenAReviewThatMissedCode(t *testing.T) {
	fake := &coverageStore{reviews: []recordedCoverage{recorded(bugsSource, "rp_docs_and_a", model.LifecycleCompleted, coverageFirst, readmeNew)}}
	conductor, repository := newCheckpointConductor(t, fake, exemptMarkdown())

	report := checkPrePush(t, conductor, repository, CoverageSubject{Changes: joined(coverageWhole, readmeNew)})
	assertOutcome(t, report, checkpointOutcome{State: CheckpointMissing, Declared: true, Exemption: &CheckpointExemption{ExemptPaths: []string{"README.md"}}})
}

func TestExemptPathsFilterEachCommitAndDropEmptyCommits(t *testing.T) {
	fake := &coverageStore{reviews: []recordedCoverage{
		recorded(bugsSource, "rp_first", model.LifecycleCompleted, coverageFirst),
		recorded(bugsSource, "rp_second", model.LifecycleCompleted, coverageSecond),
	}}
	conductor, repository := newCheckpointConductor(t, fake, exemptMarkdown())

	report := checkPrePush(t, conductor, repository, CoverageSubject{
		Changes: joined(coverageWhole, readmeNew),
		Commits: []subject.CommitContentChanges{{Commit: "c1", Changes: coverageFirst}, {Commit: "c2", Changes: readmeNew}, {Commit: "c3", Changes: joined(coverageSecond, readmeNew)}},
	})
	assertOutcome(t, report, checkpointOutcome{
		State: CheckpointCovered, Declared: true, ReviewIDs: []model.ReviewID{"rp_first", "rp_second"},
		Exemption: &CheckpointExemption{ExemptPaths: []string{"README.md"}},
	})
}

func TestOnlyExemptPathsPassWithoutTheLedger(t *testing.T) {
	fake := &coverageStore{}
	conductor, repository := newCheckpointConductor(t, fake, exemptMarkdown())

	report := checkPrePush(t, conductor, repository, CoverageSubject{Changes: readmeNew})
	assertOutcome(t, report, checkpointOutcome{State: CheckpointExempt, Declared: true, Exemption: &CheckpointExemption{Reason: ExemptByPaths, ExemptPaths: []string{"README.md"}}})
	if fake.queries != 0 {
		t.Fatalf("an exempt change made %d ledger queries", fake.queries)
	}
}

func TestSmallChangesMeasureNonExemptLinesOfTheWholeChange(t *testing.T) {
	limited := func(lines int) *configuration.Checkpoint {
		checkpoint := exemptMarkdown()
		checkpoint.SmallChangeLines = lines
		return checkpoint
	}
	content := CoverageSubject{
		Changes: joined(coverageWhole, readmeNew),
		Commits: []subject.CommitContentChanges{{Commit: "c1", Changes: coverageFirst}, {Commit: "c2", Changes: coverageSecond}},
		Lines:   subject.LineCounts{"a.go": {Added: 2}, "b.go": {Added: 1, Deleted: 2}, "README.md": {Added: 400}},
	}
	for _, test := range []struct {
		name      string
		limit     int
		lines     subject.LineCounts
		state     CheckpointState
		exemption *CheckpointExemption
	}{
		{"at the limit", 5, content.Lines, CheckpointExempt, &CheckpointExemption{Reason: ExemptBySmallChange, ExemptPaths: []string{"README.md"}, ChangedLines: 5}},
		{"over the limit", 4, content.Lines, CheckpointMissing, &CheckpointExemption{ExemptPaths: []string{"README.md"}, ChangedLines: 5}},
		{"binary disqualifies", 100, subject.LineCounts{"a.go": {Added: 1}, "b.go": {Binary: true}}, CheckpointMissing, &CheckpointExemption{ExemptPaths: []string{"README.md"}}},
		{"uncounted disqualifies", 100, nil, CheckpointMissing, &CheckpointExemption{ExemptPaths: []string{"README.md"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			conductor, repository := newCheckpointConductor(t, &coverageStore{}, limited(test.limit))
			measured := content
			measured.Lines = test.lines
			report := checkPrePush(t, conductor, repository, measured)
			assertOutcome(t, report, checkpointOutcome{State: test.state, Declared: true, Exemption: test.exemption})
		})
	}
}

func waivable(policy configuration.WaiverPolicy) *configuration.Checkpoint {
	checkpoint := exemptMarkdown()
	checkpoint.Waivers = policy
	return checkpoint
}

func waive(conductor *Conductor, repository string, content CoverageSubject, by model.WaivedBy) (CheckpointReport, error) {
	return conductor.WaiveCheckpoint(context.Background(), WaiverRequest{
		Checkpoint: CheckpointRequest{Repository: repository, Name: configuration.CheckpointPrePush, Content: content},
		Reason:     "  hotfix for the outage  ",
		WaivedBy:   by,
	})
}

func mustWaive(t *testing.T, conductor *Conductor, repository string, content CoverageSubject) CheckpointReport {
	t.Helper()
	report, err := waive(conductor, repository, content, model.WaivedByNonInteractive)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestAWaiverPassesOnlyTheContentItWaived(t *testing.T) {
	fake := &coverageStore{}
	conductor, repository := newCheckpointConductor(t, fake, waivable(configuration.WaiversAnyone))
	content := CoverageSubject{Changes: joined(coverageFirst, readmeOld)}
	exempted := &CheckpointExemption{ExemptPaths: []string{"README.md"}}
	waived := checkpointOutcome{State: CheckpointWaived, Declared: true, Exemption: exempted, Waiver: "non-interactive: hotfix for the outage"}

	assertOutcome(t, mustWaive(t, conductor, repository, content), waived)
	assertOutcome(t, checkPrePush(t, conductor, repository, CoverageSubject{Changes: joined(coverageFirst, readmeNew)}), waived)
	assertOutcome(t, checkPrePush(t, conductor, repository, CoverageSubject{Changes: coverageWhole}), checkpointOutcome{State: CheckpointMissing, Declared: true})

	assertOutcome(t, mustWaive(t, conductor, repository, content), waived)
	if len(fake.waivers) != 1 || fake.waivers[0].Repository != repository {
		t.Fatalf("waiving twice recorded %#v, want one waiver from %s", fake.waivers, repository)
	}
}

func TestRecentWaiversListOnlyThisRepositoryWithinTheWindow(t *testing.T) {
	fake := &coverageStore{}
	conductor, repository := newCheckpointConductor(t, fake, waivable(configuration.WaiversAnyone))
	fake.waivers = append(fake.waivers, model.CheckpointWaiver{ID: "cw_old", Repository: repository, CreatedAt: time.Now().Add(-31 * 24 * time.Hour)})
	waived := mustWaive(t, conductor, repository, CoverageSubject{Changes: coverageFirst})

	if recent, want := recentWaivers(t, conductor, repository), []model.CheckpointWaiver{*waived.Waiver}; !reflect.DeepEqual(recent, want) || want[0].Repository != repository {
		t.Fatalf("recent waivers = %#v, want %#v in %s", recent, want, repository)
	}
	if elsewhere := recentWaivers(t, conductor, t.TempDir()); len(elsewhere) != 0 {
		t.Fatalf("another repository's waivers = %#v", elsewhere)
	}
}

func recentWaivers(t *testing.T, conductor *Conductor, repository string) []model.CheckpointWaiver {
	t.Helper()
	recent, err := conductor.RecentCheckpointWaivers(repository, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("recent waivers in %s = %v", repository, err)
	}
	return recent
}

func TestWaivingACoveredCheckpointRecordsNothingUnderAnyPolicy(t *testing.T) {
	for _, policy := range []configuration.WaiverPolicy{configuration.WaiversAnyone, configuration.WaiversHuman, configuration.WaiversNone} {
		t.Run(string(policy), func(t *testing.T) {
			fake := &coverageStore{reviews: []recordedCoverage{recorded(bugsSource, "rp_code", model.LifecycleCompleted, coverageFirst)}}
			conductor, repository := newCheckpointConductor(t, fake, waivable(policy))

			assertOutcome(t, mustWaive(t, conductor, repository, CoverageSubject{Changes: coverageFirst}), checkpointOutcome{State: CheckpointCovered, Declared: true, ReviewIDs: []model.ReviewID{"rp_code"}})
			if len(fake.waivers) != 0 {
				t.Fatalf("waiving a covered Checkpoint recorded %d waivers", len(fake.waivers))
			}
		})
	}
}

func TestARecordedWaiverPassesOnlyWhileThePolicyWouldAllowIt(t *testing.T) {
	content := CoverageSubject{Changes: coverageFirst}
	for _, test := range []struct {
		name  string
		by    model.WaivedBy
		now   configuration.WaiverPolicy
		state CheckpointState
	}{
		{"anyone still accepts a script", model.WaivedByNonInteractive, configuration.WaiversAnyone, CheckpointWaived},
		{"human rejects a script", model.WaivedByNonInteractive, configuration.WaiversHuman, CheckpointMissing},
		{"human accepts a terminal", model.WaivedByTerminal, configuration.WaiversHuman, CheckpointWaived},
		{"none rejects a terminal", model.WaivedByTerminal, configuration.WaiversNone, CheckpointMissing},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &coverageStore{}
			recording, repository := newCheckpointConductor(t, fake, waivable(configuration.WaiversAnyone))
			if _, err := waive(recording, repository, content, test.by); err != nil {
				t.Fatal(err)
			}
			checking, repository := newCheckpointConductor(t, fake, waivable(test.now))
			if report := checkPrePush(t, checking, repository, content); report.State != test.state {
				t.Fatalf("state = %s, want %s", report.State, test.state)
			}
		})
	}
}

func TestWaiverPolicy(t *testing.T) {
	content := CoverageSubject{Changes: coverageFirst}
	for _, test := range []struct {
		name    string
		policy  configuration.WaiverPolicy
		by      model.WaivedBy
		want    error
		waivers int
	}{
		{"none refuses a terminal", configuration.WaiversNone, model.WaivedByTerminal, ErrWaiversNotAllowed, 0},
		{"human refuses a script", configuration.WaiversHuman, model.WaivedByNonInteractive, ErrWaiverNeedsPerson, 0},
		{"human accepts a terminal", configuration.WaiversHuman, model.WaivedByTerminal, nil, 1},
		{"anyone accepts a script", configuration.WaiversAnyone, model.WaivedByNonInteractive, nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &coverageStore{}
			conductor, repository := newCheckpointConductor(t, fake, waivable(test.policy))
			if _, err := waive(conductor, repository, content, test.by); !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if len(fake.waivers) != test.waivers {
				t.Fatalf("waivers = %#v", fake.waivers)
			}
			for _, waiver := range fake.waivers {
				if waiver.WaivedBy != test.by {
					t.Fatalf("waived_by = %s, want %s", waiver.WaivedBy, test.by)
				}
			}
		})
	}
}

func TestWaivingNeedsAReasonAndADeclaration(t *testing.T) {
	conductor, repository := newCheckpointConductor(t, &coverageStore{}, nil)
	_, err := waive(conductor, repository, CoverageSubject{Changes: coverageFirst}, model.WaivedByTerminal)
	if err == nil || !strings.Contains(err.Error(), "review-party config checkpoint set pre-push") {
		t.Fatalf("undeclared waiver error = %v", err)
	}

	declared, declaredRepository := newCheckpointConductor(t, &coverageStore{}, waivable(configuration.WaiversAnyone))
	_, err = declared.WaiveCheckpoint(context.Background(), WaiverRequest{
		Checkpoint: CheckpointRequest{Repository: declaredRepository, Name: configuration.CheckpointPrePush, Content: CoverageSubject{Changes: coverageFirst}},
		Reason:     " ",
		WaivedBy:   model.WaivedByTerminal,
	})
	if err == nil || !strings.Contains(err.Error(), "requires a reason") {
		t.Fatalf("empty reason error = %v", err)
	}
}
