package engine

import (
	"testing"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
	"reviewparty/internal/subject"
)

func judgedCheckpoint() *configuration.Checkpoint {
	checkpoint := exemptMarkdown()
	checkpoint.Requirement = configuration.RequirementJudged
	return checkpoint
}

func findingsAt(locations ...string) []model.Finding {
	findings := []model.Finding{}
	for index, location := range locations {
		findings = append(findings, model.Finding{Ordinal: index + 1, Location: location})
	}
	return findings
}

func verdict(id string, ordinal int, judged model.Verdict) model.FindingVerdict {
	return model.FindingVerdict{ReviewID: model.ReviewID(id), Ordinal: ordinal, Verdict: judged}
}

func staleVerdict(id string, ordinal int) model.FindingVerdict {
	stale := verdict(id, ordinal, model.VerdictAccepted)
	stale.Stale = true
	return stale
}

func TestJudgedCountsOnlyCurrentVerdicts(t *testing.T) {
	exempted := &CheckpointExemption{ExemptPaths: []string{"README.md"}}
	for _, test := range []struct {
		name     string
		findings []model.Finding
		verdicts []model.FindingVerdict
		want     checkpointOutcome
	}{
		{
			name: "every verdict kind counts", findings: findingsAt("a.go:1", "a.go:2", "a.go:3"),
			verdicts: []model.FindingVerdict{verdict("rp_code", 1, model.VerdictAccepted), verdict("rp_code", 2, model.VerdictRejected), verdict("rp_code", 3, model.VerdictDeferred)},
			want:     checkpointOutcome{State: CheckpointCovered, Declared: true, Exemption: exempted, ReviewIDs: []model.ReviewID{"rp_code"}},
		},
		{
			name: "one finding without a verdict", findings: findingsAt("a.go:1", "a.go:2"),
			verdicts: []model.FindingVerdict{verdict("rp_code", 1, model.VerdictAccepted)},
			want: checkpointOutcome{
				State: CheckpointUnjudged, Declared: true, Exemption: exempted, ReviewIDs: []model.ReviewID{"rp_code"},
				Unjudged: []UnjudgedReview{{Review: "rp_code", Ordinals: []int{2}}},
			},
		},
		{
			name: "a stale verdict", findings: findingsAt("a.go:1", "a.go:2"),
			verdicts: []model.FindingVerdict{verdict("rp_code", 1, model.VerdictAccepted), staleVerdict("rp_code", 2)},
			want: checkpointOutcome{
				State: CheckpointUnjudged, Declared: true, Exemption: exempted, ReviewIDs: []model.ReviewID{"rp_code"},
				Unjudged: []UnjudgedReview{{Review: "rp_code", Ordinals: []int{2}}},
			},
		},
		{
			name: "zero findings", findings: findingsAt(),
			want: checkpointOutcome{State: CheckpointCovered, Declared: true, Exemption: exempted, ReviewIDs: []model.ReviewID{"rp_code"}},
		},
		{
			name: "a finding in an exempt path", findings: findingsAt("README.md:3", "a.go:1"),
			verdicts: []model.FindingVerdict{verdict("rp_code", 2, model.VerdictAccepted)},
			want:     checkpointOutcome{State: CheckpointCovered, Declared: true, Exemption: exempted, ReviewIDs: []model.ReviewID{"rp_code"}},
		},
		{
			name: "a location that is not one path and line", findings: findingsAt("README.md and a.go", "docs/README.md:1", "README.md:3 and a.go:5", "README.md"),
			want: checkpointOutcome{
				State: CheckpointUnjudged, Declared: true, Exemption: exempted, ReviewIDs: []model.ReviewID{"rp_code"},
				Unjudged: []UnjudgedReview{{Review: "rp_code", Ordinals: []int{1, 3, 4}}},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &coverageStore{
				reviews:  []recordedCoverage{recorded(bugsSource, "rp_code", model.LifecycleCompleted, coverageFirst)},
				findings: map[model.ReviewID][]model.Finding{"rp_code": test.findings},
				verdicts: test.verdicts,
			}
			conductor, repository := newCheckpointConductor(t, fake, judgedCheckpoint())

			report := checkPrePush(t, conductor, repository, CoverageSubject{Changes: joined(coverageFirst, readmeNew)})
			assertOutcome(t, report, test.want)
			if !report.Coverage.Covered {
				t.Fatalf("Coverage = %+v, want covered whatever the verdicts", report.Coverage)
			}
		})
	}
}

func TestJudgedSkipsFindingsInPathsOnlyTheReviewChanged(t *testing.T) {
	fake := &coverageStore{
		reviews:  []recordedCoverage{recorded(bugsSource, "rp_code", model.LifecycleCompleted, joined(coverageFirst, readmeNew))},
		findings: map[model.ReviewID][]model.Finding{"rp_code": findingsAt("README.md:3")},
	}
	conductor, repository := newCheckpointConductor(t, fake, judgedCheckpoint())

	assertOutcome(t, checkPrePush(t, conductor, repository, CoverageSubject{Changes: coverageFirst}), checkpointOutcome{
		State: CheckpointCovered, Declared: true, ReviewIDs: []model.ReviewID{"rp_code"},
	})
}

func TestAnyFullyJudgedCoveringReviewPasses(t *testing.T) {
	fake := &coverageStore{
		reviews: []recordedCoverage{
			recorded(bugsSource, "rp_new", model.LifecycleCompleted, coverageFirst),
			recorded(bugsSource, "rp_old", model.LifecycleCompleted, coverageFirst),
		},
		findings: map[model.ReviewID][]model.Finding{"rp_new": findingsAt("a.go:1"), "rp_old": findingsAt("a.go:1")},
	}
	conductor, repository := newCheckpointConductor(t, fake, judgedCheckpoint())
	content := CoverageSubject{Changes: coverageFirst}

	assertOutcome(t, checkPrePush(t, conductor, repository, content), checkpointOutcome{
		State: CheckpointUnjudged, Declared: true, ReviewIDs: []model.ReviewID{"rp_new"},
		Unjudged: []UnjudgedReview{{Review: "rp_new", Ordinals: []int{1}}},
	})

	fake.verdicts = []model.FindingVerdict{verdict("rp_old", 1, model.VerdictRejected)}
	assertOutcome(t, checkPrePush(t, conductor, repository, content), checkpointOutcome{
		State: CheckpointCovered, Declared: true, ReviewIDs: []model.ReviewID{"rp_old"},
	})
}

func TestJudgedPerCommitNeedsEachCommitsReviewJudged(t *testing.T) {
	fake := &coverageStore{
		reviews: []recordedCoverage{
			recorded(bugsSource, "rp_first", model.LifecycleCompleted, coverageFirst),
			recorded(bugsSource, "rp_second", model.LifecycleCompleted, coverageSecond),
		},
		findings: map[model.ReviewID][]model.Finding{"rp_first": findingsAt(), "rp_second": findingsAt("b.go:4", "b.go:9")},
		verdicts: []model.FindingVerdict{verdict("rp_second", 2, model.VerdictAccepted)},
	}
	conductor, repository := newCheckpointConductor(t, fake, judgedCheckpoint())
	content := CoverageSubject{Changes: coverageWhole, Commits: []subject.CommitContentChanges{{Commit: "c1", Changes: coverageFirst}, {Commit: "c2", Changes: coverageSecond}}}

	assertOutcome(t, checkPrePush(t, conductor, repository, content), checkpointOutcome{
		State: CheckpointUnjudged, Declared: true, ReviewIDs: []model.ReviewID{"rp_first", "rp_second"},
		Unjudged: []UnjudgedReview{{Review: "rp_second", Ordinals: []int{1}}},
	})
}

func TestAWaiverPassesAnUnjudgedChange(t *testing.T) {
	checkpoint := judgedCheckpoint()
	checkpoint.Waivers = configuration.WaiversAnyone
	fake := &coverageStore{
		reviews:  []recordedCoverage{recorded(bugsSource, "rp_code", model.LifecycleCompleted, coverageFirst)},
		findings: map[model.ReviewID][]model.Finding{"rp_code": findingsAt("a.go:1")},
	}
	conductor, repository := newCheckpointConductor(t, fake, checkpoint)
	content := CoverageSubject{Changes: coverageFirst}

	if state := checkPrePush(t, conductor, repository, content).State; state != CheckpointUnjudged {
		t.Fatalf("state before the waiver = %s, want unjudged", state)
	}
	mustWaive(t, conductor, repository, content)
	if state := checkPrePush(t, conductor, repository, content).State; state != CheckpointWaived {
		t.Fatalf("state after the waiver = %s, want waived", state)
	}
}

func TestVerdictsAreReadOnlyForJudgedCoveredProfiles(t *testing.T) {
	reviewed := exemptMarkdown()
	for _, test := range []struct {
		name       string
		checkpoint *configuration.Checkpoint
		lifecycle  model.Lifecycle
		state      CheckpointState
	}{
		{"reviewed ignores findings", reviewed, model.LifecycleCompleted, CheckpointCovered},
		{"undeclared ignores findings", nil, model.LifecycleCompleted, CheckpointCovered},
		{"judged waits for a running review", judgedCheckpoint(), model.LifecycleRunning, CheckpointRunning},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := &coverageStore{
				reviews:  []recordedCoverage{recorded(bugsSource, "rp_code", test.lifecycle, coverageFirst)},
				findings: map[model.ReviewID][]model.Finding{"rp_code": findingsAt("a.go:1")},
			}
			conductor, repository := newCheckpointConductor(t, fake, test.checkpoint)

			if state := checkPrePush(t, conductor, repository, CoverageSubject{Changes: coverageFirst}).State; state != test.state {
				t.Fatalf("state = %s, want %s", state, test.state)
			}
			if fake.loads != 0 {
				t.Fatalf("read %d Reviews for verdicts, want none", fake.loads)
			}
		})
	}
}
