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
)

var (
	readmeOld = []model.ContentChange{{Path: "README.md", Before: model.ZeroObjectID, After: "3333333333333333333333333333333333333333"}}
	readmeNew = []model.ContentChange{{Path: "README.md", Before: model.ZeroObjectID, After: "4444444444444444444444444444444444444444"}}
	a1        = coverageFirst[0].After
	a2        = strings.Repeat("a2", 20)
	a3        = strings.Repeat("a3", 20)
	b1        = strings.Repeat("b1", 20)
)

const bugsSource = "global:profiles/bugs"

func aGo(before, after string) []model.ContentChange {
	return []model.ContentChange{{Path: "a.go", Before: before, After: after}}
}

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

func bounded(lines, budget int) *configuration.Checkpoint {
	checkpoint := exemptMarkdown()
	checkpoint.UnreviewedLines, checkpoint.ReviewBudget = lines, budget
	return checkpoint
}

func checkPrePush(t *testing.T, conductor *Conductor, repository string, content []model.ContentChange) CheckpointReport {
	t.Helper()
	report, err := conductor.CheckCheckpoint(context.Background(), CheckpointRequest{Repository: repository, Name: configuration.CheckpointPrePush, Content: content})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// checkpointOutcome is the part of a CheckpointReport a Caller acts on.
type checkpointOutcome struct {
	State      CheckpointState
	Declared   bool
	Exemption  *CheckpointExemption
	ReviewIDs  []model.ReviewID
	Spent      int
	Unreviewed []model.ContentChange
	Unjudged   []UnjudgedReview
	Waiver     string
}

func outcomeOf(report CheckpointReport) checkpointOutcome {
	outcome := checkpointOutcome{State: report.State, Declared: report.Declaration != nil, Exemption: report.Exemption, Unreviewed: report.Coverage.Unreviewed}
	for _, profile := range report.Coverage.Profiles {
		outcome.ReviewIDs = append(outcome.ReviewIDs, profile.Reviews...)
		outcome.Unjudged = append(outcome.Unjudged, profile.Unjudged...)
		outcome.Spent += profile.Spent
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

func TestUndeclaredCheckpointAllowsNoUnreviewedLines(t *testing.T) {
	fake := &coverageStore{reviews: []recordedCoverage{recorded(bugsSource, "rp_code", model.LifecycleCompleted, coverageFirst)}}
	conductor, repository := newCheckpointConductor(t, fake, nil)

	report := checkPrePush(t, conductor, repository, joined(coverageFirst, readmeNew))
	assertOutcome(t, report, checkpointOutcome{State: CheckpointMissing, ReviewIDs: []model.ReviewID{"rp_code"}, Spent: 1, Unreviewed: readmeNew})
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

			report := checkPrePush(t, conductor, repository, joined(coverageFirst, readmeNew))
			assertOutcome(t, report, checkpointOutcome{
				State: CheckpointCovered, Declared: true, ReviewIDs: []model.ReviewID{"rp_code"}, Spent: 1,
				Exemption: &CheckpointExemption{ExemptPaths: []string{"README.md"}},
			})
		})
	}
}

func TestExemptPathsNeverWidenAReviewThatMissedCode(t *testing.T) {
	fake := &coverageStore{reviews: []recordedCoverage{recorded(bugsSource, "rp_docs_and_a", model.LifecycleCompleted, coverageFirst, readmeNew)}}
	conductor, repository := newCheckpointConductor(t, fake, exemptMarkdown())

	report := checkPrePush(t, conductor, repository, joined(coverageWhole, readmeNew))
	assertOutcome(t, report, checkpointOutcome{
		State: CheckpointMissing, Declared: true, ReviewIDs: []model.ReviewID{"rp_docs_and_a"}, Spent: 1, Unreviewed: coverageSecond,
		Exemption: &CheckpointExemption{ExemptPaths: []string{"README.md"}},
	})
}

func TestOnlyExemptPathsPassWithoutTheLedger(t *testing.T) {
	fake := &coverageStore{}
	conductor, repository := newCheckpointConductor(t, fake, exemptMarkdown())

	report := checkPrePush(t, conductor, repository, readmeNew)
	assertOutcome(t, report, checkpointOutcome{State: CheckpointExempt, Declared: true, Exemption: &CheckpointExemption{ExemptPaths: []string{"README.md"}}})
	if fake.queries != 0 {
		t.Fatalf("an exempt change made %d ledger queries", fake.queries)
	}
}

func TestTheUnreviewedDeltaStartsFromTheNewestReviewedState(t *testing.T) {
	for _, test := range []struct {
		name    string
		reviews []recordedCoverage
		content []model.ContentChange
		want    checkpointOutcome
	}{
		{
			name:    "a Review of the same blobs covers, from a working tree or a commit",
			reviews: []recordedCoverage{recorded(bugsSource, "rp_1", model.LifecycleCompleted, aGo(model.ZeroObjectID, a1))},
			content: aGo(model.ZeroObjectID, a1),
			want:    checkpointOutcome{State: CheckpointCovered, ReviewIDs: []model.ReviewID{"rp_1"}, Spent: 1},
		},
		{
			name:    "a squash or an unreviewed-delta Review chains through the middle state",
			reviews: []recordedCoverage{recorded(bugsSource, "rp_1", model.LifecycleCompleted, aGo(model.ZeroObjectID, a1)), recorded(bugsSource, "rp_2", model.LifecycleCompleted, aGo(a1, a2))},
			content: aGo(model.ZeroObjectID, a2),
			want:    checkpointOutcome{State: CheckpointCovered, ReviewIDs: []model.ReviewID{"rp_1", "rp_2"}, Spent: 2},
		},
		{
			name:    "an amended commit leaves the delta from the reviewed state",
			reviews: []recordedCoverage{recorded(bugsSource, "rp_1", model.LifecycleCompleted, aGo(model.ZeroObjectID, a1))},
			content: aGo(model.ZeroObjectID, a2),
			want:    checkpointOutcome{State: CheckpointMissing, ReviewIDs: []model.ReviewID{"rp_1"}, Spent: 1, Unreviewed: aGo(a1, a2)},
		},
		{
			name:    "a rebase onto a changed base counts the file in full",
			reviews: []recordedCoverage{recorded(bugsSource, "rp_1", model.LifecycleCompleted, aGo(model.ZeroObjectID, a1))},
			content: aGo(b1, a2),
			want:    checkpointOutcome{State: CheckpointMissing, Unreviewed: aGo(b1, a2)},
		},
		{
			name:    "a gap in the chain re-reviews from the last reached state",
			reviews: []recordedCoverage{recorded(bugsSource, "rp_1", model.LifecycleCompleted, aGo(model.ZeroObjectID, a1)), recorded(bugsSource, "rp_2", model.LifecycleCompleted, aGo(a2, a3))},
			content: aGo(model.ZeroObjectID, a3),
			want:    checkpointOutcome{State: CheckpointMissing, ReviewIDs: []model.ReviewID{"rp_1"}, Spent: 1, Unreviewed: aGo(a1, a3)},
		},
		{
			name:    "an incomplete Review spends and never credits",
			reviews: []recordedCoverage{recorded(bugsSource, "rp_1", model.LifecycleIncomplete, aGo(model.ZeroObjectID, a1))},
			content: aGo(model.ZeroObjectID, a1),
			want:    checkpointOutcome{State: CheckpointMissing, Spent: 1, Unreviewed: aGo(model.ZeroObjectID, a1)},
		},
		{
			name:    "the newest of parallel Reviews is the evidence",
			reviews: []recordedCoverage{recorded(bugsSource, "rp_old", model.LifecycleCompleted, aGo(model.ZeroObjectID, a1)), recorded(bugsSource, "rp_new", model.LifecycleCompleted, aGo(model.ZeroObjectID, a1))},
			content: aGo(model.ZeroObjectID, a1),
			want:    checkpointOutcome{State: CheckpointCovered, ReviewIDs: []model.ReviewID{"rp_new"}, Spent: 2},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			conductor, repository := newCheckpointConductor(t, &coverageStore{reviews: test.reviews}, bounded(0, 9))
			test.want.Declared = true
			assertOutcome(t, checkPrePush(t, conductor, repository, test.content), test.want)
		})
	}
}

func TestAReviewedStateWhoseBlobIsGoneIsUnreached(t *testing.T) {
	for _, test := range []struct {
		name    string
		reviews []recordedCoverage
		absent  map[string]bool
		want    checkpointOutcome
	}{
		{
			name:    "a pruned working-tree Review leaves the path unreviewed from the base",
			reviews: []recordedCoverage{recorded(bugsSource, "rp_1", model.LifecycleCompleted, aGo(model.ZeroObjectID, a1))},
			absent:  map[string]bool{a1: true},
			want:    checkpointOutcome{State: CheckpointMissing, Declared: true, Spent: 1, Unreviewed: aGo(model.ZeroObjectID, a2)},
		},
		{
			name:    "a pruned state in a chain falls back to the state before it",
			reviews: []recordedCoverage{recorded(bugsSource, "rp_1", model.LifecycleCompleted, aGo(model.ZeroObjectID, a1)), recorded(bugsSource, "rp_2", model.LifecycleCompleted, aGo(a1, a3))},
			absent:  map[string]bool{a3: true},
			want:    checkpointOutcome{State: CheckpointMissing, Declared: true, ReviewIDs: []model.ReviewID{"rp_1"}, Spent: 2, Unreviewed: aGo(a1, a2)},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			conductor, repository := newCheckpointConductor(t, &coverageStore{reviews: test.reviews, absent: test.absent}, bounded(0, 9))
			assertOutcome(t, checkPrePush(t, conductor, repository, aGo(model.ZeroObjectID, a2)), test.want)
		})
	}
}

// A gitlink's states are commits of the nested repository, so the repository
// is never asked whether it has them.
func TestAReviewedGitlinkStateIsReachedWithoutTheObject(t *testing.T) {
	nested := []model.ContentChange{{Path: "nested", Before: model.ZeroObjectID, After: a1, AfterGitlink: true}}
	reviews := []recordedCoverage{recorded(bugsSource, "rp_1", model.LifecycleCompleted, nested)}
	conductor, repository := newCheckpointConductor(t, &coverageStore{reviews: reviews, absent: map[string]bool{a1: true}}, bounded(0, 9))
	assertOutcome(t, checkPrePush(t, conductor, repository, nested), checkpointOutcome{State: CheckpointCovered, Declared: true, ReviewIDs: []model.ReviewID{"rp_1"}, Spent: 1})
}

// The kind of a reviewed state between a file and a gitlink is unknown, so a
// path that changed kind stays unreviewed as a whole until its current
// content is reviewed.
func TestATypeChangedPathIsUnreviewedAsAWhole(t *testing.T) {
	reviews := []recordedCoverage{recorded(bugsSource, "rp_1", model.LifecycleCompleted, []model.ContentChange{{Path: "vendored", Before: a1, After: a2}})}
	conductor, repository := newCheckpointConductor(t, &coverageStore{reviews: reviews}, bounded(0, 9))
	current := []model.ContentChange{{Path: "vendored", Before: a1, After: a3, AfterGitlink: true}}
	report := checkPrePush(t, conductor, repository, current)
	if report.State != CheckpointMissing || !reflect.DeepEqual(report.Coverage.Unreviewed, current) {
		t.Fatalf("state = %s, unreviewed = %v, want missing with %v", report.State, report.Coverage.Unreviewed, current)
	}
}

func TestTheAllowanceDecidesBetweenResidualAndMissing(t *testing.T) {
	reviewed := []recordedCoverage{recorded(bugsSource, "rp_1", model.LifecycleCompleted, aGo(model.ZeroObjectID, a1))}
	for _, test := range []struct {
		name      string
		allowance int
		content   []model.ContentChange
		binary    bool
		state     CheckpointState
	}{
		{"at the allowance", 3, aGo(model.ZeroObjectID, a2), false, CheckpointResidual},
		{"over the allowance", 2, aGo(model.ZeroObjectID, a2), false, CheckpointMissing},
		{"a binary delta exceeds any allowance", 100, aGo(model.ZeroObjectID, a2), true, CheckpointMissing},
		{"nothing unreviewed is covered, not residual", 3, aGo(model.ZeroObjectID, a1), false, CheckpointCovered},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &coverageStore{reviews: reviewed, lines: map[model.ContentChange]int{{Path: "a.go", Before: a1, After: a2}: 3}}
			if test.binary {
				fake.binary = map[string]bool{"a.go": true}
			}
			conductor, repository := newCheckpointConductor(t, fake, bounded(test.allowance, 9))
			report := checkPrePush(t, conductor, repository, test.content)
			if report.State != test.state {
				t.Fatalf("state = %s, want %s", report.State, test.state)
			}
			if test.state == CheckpointResidual && !reflect.DeepEqual(report.Coverage.Unreviewed, aGo(a1, a2)) {
				t.Fatalf("residual delta = %v, want %v", report.Coverage.Unreviewed, aGo(a1, a2))
			}
		})
	}
}

func TestTheBudgetAndRunningReviewsDecideAnOverAllowanceProfile(t *testing.T) {
	completed := recorded(bugsSource, "rp_1", model.LifecycleCompleted, aGo(model.ZeroObjectID, a1))
	for _, test := range []struct {
		name      string
		reviews   []recordedCoverage
		allowance int
		content   []model.ContentChange
		want      checkpointOutcome
	}{
		{
			name:    "a running Review of the base is running",
			reviews: []recordedCoverage{recorded(bugsSource, "rp_1", model.LifecycleRunning, aGo(model.ZeroObjectID, a1))},
			content: aGo(model.ZeroObjectID, a1),
			want:    checkpointOutcome{State: CheckpointRunning, Spent: 1, Unreviewed: aGo(model.ZeroObjectID, a1)},
		},
		{
			name:    "the budget is spent after as many Reviews of any lifecycle",
			reviews: []recordedCoverage{completed, recorded(bugsSource, "rp_2", model.LifecycleIncomplete, aGo(a1, a2))},
			content: aGo(model.ZeroObjectID, a3),
			want:    checkpointOutcome{State: CheckpointSpent, ReviewIDs: []model.ReviewID{"rp_1"}, Spent: 2, Unreviewed: aGo(a1, a3)},
		},
		{
			name:    "a running Review is reported before a spent budget",
			reviews: []recordedCoverage{completed, recorded(bugsSource, "rp_2", model.LifecycleRunning, aGo(a1, a2))},
			content: aGo(model.ZeroObjectID, a3),
			want:    checkpointOutcome{State: CheckpointRunning, ReviewIDs: []model.ReviewID{"rp_1"}, Spent: 2, Unreviewed: aGo(a1, a3)},
		},
		{
			name:    "a Review from an unreached state spends nothing",
			reviews: []recordedCoverage{completed, recorded(bugsSource, "rp_2", model.LifecycleCompleted, aGo(a2, a3))},
			content: aGo(model.ZeroObjectID, a3),
			want:    checkpointOutcome{State: CheckpointMissing, ReviewIDs: []model.ReviewID{"rp_1"}, Spent: 1, Unreviewed: aGo(a1, a3)},
		},
		{
			name:      "within the allowance a spent budget still passes",
			reviews:   []recordedCoverage{completed, recorded(bugsSource, "rp_2", model.LifecycleIncomplete, aGo(a1, a2))},
			allowance: 5,
			content:   aGo(model.ZeroObjectID, a2),
			want:      checkpointOutcome{State: CheckpointResidual, ReviewIDs: []model.ReviewID{"rp_1"}, Spent: 2, Unreviewed: aGo(a1, a2)},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &coverageStore{reviews: test.reviews, lines: map[model.ContentChange]int{{Path: "a.go", Before: a1, After: a2}: 2}}
			conductor, repository := newCheckpointConductor(t, fake, bounded(test.allowance, 2))
			test.want.Declared = true
			assertOutcome(t, checkPrePush(t, conductor, repository, test.content), test.want)
		})
	}
}

func waivable(policy configuration.WaiverPolicy) *configuration.Checkpoint {
	checkpoint := exemptMarkdown()
	checkpoint.Waivers = policy
	return checkpoint
}

func waive(conductor *Conductor, repository string, content []model.ContentChange, by model.WaivedBy) (CheckpointReport, error) {
	return conductor.WaiveCheckpoint(context.Background(), WaiverRequest{
		Checkpoint: CheckpointRequest{Repository: repository, Name: configuration.CheckpointPrePush, Content: content},
		Reason:     "  hotfix for the outage  ",
		WaivedBy:   by,
	})
}

func mustWaive(t *testing.T, conductor *Conductor, repository string, content []model.ContentChange) CheckpointReport {
	t.Helper()
	report, err := waive(conductor, repository, content, model.WaivedByNonInteractive)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func TestAWaiverPassesOnlyTheDeltaItWaived(t *testing.T) {
	bGo := func(before, after string) []model.ContentChange {
		return []model.ContentChange{{Path: "b.go", Before: before, After: after}}
	}
	b2 := coverageSecond[0].After
	fake := &coverageStore{reviews: []recordedCoverage{recorded(bugsSource, "rp_1", model.LifecycleCompleted, coverageWhole)}}
	conductor, repository := newCheckpointConductor(t, fake, waivable(configuration.WaiversAnyone))
	content := joined(aGo(model.ZeroObjectID, a2), bGo(model.ZeroObjectID, b2), readmeOld)
	exempted := &CheckpointExemption{ExemptPaths: []string{"README.md"}}
	waived := checkpointOutcome{State: CheckpointWaived, Declared: true, Exemption: exempted, ReviewIDs: []model.ReviewID{"rp_1"}, Spent: 1, Unreviewed: aGo(a1, a2), Waiver: "non-interactive: hotfix for the outage"}

	assertOutcome(t, mustWaive(t, conductor, repository, content), waived)
	assertOutcome(t, checkPrePush(t, conductor, repository, joined(aGo(model.ZeroObjectID, a2), bGo(model.ZeroObjectID, b2), readmeNew)), waived)
	assertOutcome(t, mustWaive(t, conductor, repository, content), waived)
	if len(fake.waivers) != 1 || fake.waivers[0].Repository != repository {
		t.Fatalf("waiving twice recorded %#v, want one waiver from %s", fake.waivers, repository)
	}

	fake.reviews = append(fake.reviews, recorded(bugsSource, "rp_2", model.LifecycleCompleted, bGo(b2, b1)))
	waived.ReviewIDs, waived.Spent = []model.ReviewID{"rp_1", "rp_2"}, 2
	assertOutcome(t, checkPrePush(t, conductor, repository, joined(aGo(model.ZeroObjectID, a2), bGo(model.ZeroObjectID, b1), readmeNew)), waived)
	assertOutcome(t, checkPrePush(t, conductor, repository, joined(aGo(model.ZeroObjectID, a3), bGo(model.ZeroObjectID, b1))), checkpointOutcome{State: CheckpointMissing, Declared: true, ReviewIDs: []model.ReviewID{"rp_1", "rp_2"}, Spent: 2, Unreviewed: aGo(a1, a3)})
}

func TestRecentWaiversListOnlyThisRepositoryWithinTheWindow(t *testing.T) {
	fake := &coverageStore{}
	conductor, repository := newCheckpointConductor(t, fake, waivable(configuration.WaiversAnyone))
	fake.waivers = append(fake.waivers, model.CheckpointWaiver{ID: "cw_old", Repository: repository, CreatedAt: time.Now().Add(-31 * 24 * time.Hour)})
	waived := mustWaive(t, conductor, repository, coverageFirst)

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

			assertOutcome(t, mustWaive(t, conductor, repository, coverageFirst), checkpointOutcome{State: CheckpointCovered, Declared: true, ReviewIDs: []model.ReviewID{"rp_code"}, Spent: 1})
			if len(fake.waivers) != 0 {
				t.Fatalf("waiving a covered Checkpoint recorded %d waivers", len(fake.waivers))
			}
		})
	}
}

func TestARecordedWaiverPassesOnlyWhileThePolicyWouldAllowIt(t *testing.T) {
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
			if _, err := waive(recording, repository, coverageFirst, test.by); err != nil {
				t.Fatal(err)
			}
			checking, repository := newCheckpointConductor(t, fake, waivable(test.now))
			if report := checkPrePush(t, checking, repository, coverageFirst); report.State != test.state {
				t.Fatalf("state = %s, want %s", report.State, test.state)
			}
		})
	}
}

func TestWaiverPolicy(t *testing.T) {
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
			if _, err := waive(conductor, repository, coverageFirst, test.by); !errors.Is(err, test.want) {
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
	_, err := waive(conductor, repository, coverageFirst, model.WaivedByTerminal)
	if err == nil || !strings.Contains(err.Error(), "review-party config checkpoint set pre-push") {
		t.Fatalf("undeclared waiver error = %v", err)
	}

	declared, declaredRepository := newCheckpointConductor(t, &coverageStore{}, waivable(configuration.WaiversAnyone))
	_, err = declared.WaiveCheckpoint(context.Background(), WaiverRequest{
		Checkpoint: CheckpointRequest{Repository: declaredRepository, Name: configuration.CheckpointPrePush, Content: coverageFirst},
		Reason:     " ",
		WaivedBy:   model.WaivedByTerminal,
	})
	if err == nil || !strings.Contains(err.Error(), "requires a reason") {
		t.Fatalf("empty reason error = %v", err)
	}
}
