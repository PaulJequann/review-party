package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/model"
	"reviewparty/internal/store"
	"reviewparty/internal/subject"
)

type checkpointFixture struct {
	t          *testing.T
	repository string
	ledger     string
	base       string
}

func newCheckpointFixture(t *testing.T) checkpointFixture {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	repository := testGitRepository(t)
	fixture := checkpointFixture{t: t, repository: repository, ledger: filepath.Join(stateHome, "review-party")}
	fixture.git("config", "user.email", "review-party@example.invalid")
	fixture.git("config", "user.name", "Review Party Test")
	fixture.writeFile(".git/info/exclude", ".reviewparty/\n")
	fixture.writeFile("app.go", "package app\n")
	fixture.git("add", "app.go")
	fixture.git("commit", "--quiet", "-m", "base")
	fixture.base = fixture.git("rev-parse", "HEAD")
	runMainCommand(t, []string{"init", "--repo", repository})
	for _, name := range []string{"bugs", "docs"} {
		fixture.writeFile(".reviewparty/profiles/"+name+"/profile.json", `{"schema_version":1,"name":"`+name+`","reviewer":"grok","model":"grok-4.5","reasoning_effort":"high","attempt_deadline":"1m"}`)
		fixture.writeFile(".reviewparty/profiles/"+name+"/instructions.md", "Review "+name+".\n")
	}
	fixture.writeFile(".reviewparty/config.json", `{"schema_version":1,"reviews":{"concurrency_limit":1,"global":[],"repository":[{"profile":"bugs"},{"profile":"docs"}]}}`)
	return fixture
}

func (fixture checkpointFixture) git(arguments ...string) string {
	fixture.t.Helper()
	command := exec.Command("git", append([]string{"-C", fixture.repository}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		fixture.t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

func (fixture checkpointFixture) writeFile(path, content string) {
	fixture.t.Helper()
	location := filepath.Join(fixture.repository, path)
	if err := os.MkdirAll(filepath.Dir(location), 0o755); err != nil {
		fixture.t.Fatal(err)
	}
	if err := os.WriteFile(location, []byte(content), 0o644); err != nil {
		fixture.t.Fatal(err)
	}
}

func (fixture checkpointFixture) commit(path, content string) string {
	fixture.t.Helper()
	fixture.writeFile(path, content)
	fixture.git("add", path)
	fixture.git("commit", "--quiet", "-m", "change "+path)
	return fixture.git("rev-parse", "HEAD")
}

// nestedRepository is a repository inside the fixture's, committed there as
// a gitlink. Its commits are never objects of the fixture's repository.
type nestedRepository struct {
	fixture checkpointFixture
	path    string
}

// commitNestedRepository creates a nested repository at path and commits its
// gitlink, returning it with the fixture's new HEAD.
func (fixture checkpointFixture) commitNestedRepository(path string) (nestedRepository, string) {
	fixture.t.Helper()
	fixture.git("init", "--quiet", path)
	fixture.git("-C", path, "config", "user.email", "review-party@example.invalid")
	fixture.git("-C", path, "config", "user.name", "Review Party Test")
	nested := nestedRepository{fixture: fixture, path: path}
	return nested, nested.commitBump()
}

// stageBump advances the nested repository and stages the bumped gitlink.
func (nested nestedRepository) stageBump() {
	nested.fixture.t.Helper()
	nested.fixture.git("-C", nested.path, "commit", "--quiet", "--allow-empty", "-m", "nested")
	nested.fixture.git("add", nested.path)
}

// commitBump commits a bumped gitlink and returns the fixture's new HEAD.
func (nested nestedRepository) commitBump() string {
	nested.fixture.t.Helper()
	nested.stageBump()
	nested.fixture.git("commit", "--quiet", "-m", "bump "+nested.path)
	return nested.fixture.git("rev-parse", "HEAD")
}

// saveReview records a Review of one repository Profile over exactly changes,
// as a run of that Profile would.
func (fixture checkpointFixture) saveReview(id model.ReviewID, profile string, lifecycle model.Lifecycle, changes []model.ContentChange) {
	fixture.t.Helper()
	record := fixture.reviewRecord(id, profile, lifecycle, changes)
	if lifecycle == model.LifecycleCompleted {
		record.Result = &model.ReviewResult{Status: model.ResultClean, Summary: "clean"}
	}
	fixture.save(record)
}

func (fixture checkpointFixture) reviewRecord(id model.ReviewID, profile string, lifecycle model.Lifecycle, changes []model.ContentChange) model.ReviewRecord {
	now := time.Now().UTC()
	return model.ReviewRecord{
		SchemaVersion: model.CurrentReviewRecordSchemaVersion, ID: id, Lifecycle: lifecycle,
		Subject:         model.ReviewSubject{Kind: model.SubjectCommittedRange, Repository: fixture.repository, Identity: string(id), ChangedPaths: []string{}, ContentChanges: changes},
		ProfileRevision: model.ProfileRevision{Name: profile, Source: "repository:.reviewparty/profiles/" + profile},
		ProfileSnapshot: model.ProfileSnapshot{Name: profile},
		CreatedAt:       now, UpdatedAt: now,
	}
}

func (fixture checkpointFixture) save(record model.ReviewRecord) {
	fixture.t.Helper()
	ledger, err := store.NewLedgerRecordStore(fixture.ledger)
	if err != nil {
		fixture.t.Fatal(err)
	}
	defer closeCheckpointLedger(fixture.t, ledger)
	if err := ledger.Save(record); err != nil {
		fixture.t.Fatal(err)
	}
}

func closeCheckpointLedger(t *testing.T, ledger *store.LedgerRecordStore) {
	t.Helper()
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
}

func (fixture checkpointFixture) rangeChanges(head string) subject.RangeContentChanges {
	fixture.t.Helper()
	changes, err := subject.CommittedRangeContentChanges(fixture.repository, model.CommittedRange(fixture.base, head))
	if err != nil {
		fixture.t.Fatal(err)
	}
	return changes
}

func (fixture checkpointFixture) check(arguments ...string) (int, string, string) {
	fixture.t.Helper()
	var stdout, stderr bytes.Buffer
	exit := run(context.Background(), append([]string{"checkpoint", "check"}, append(arguments, "--repo", fixture.repository)...), &stdout, &stderr)
	return exit, stdout.String(), stderr.String()
}

func TestCheckpointPrePushCoveredByOneRangeReview(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.commit("one.go", "package app\n\nconst one = 1\n")
	head := fixture.commit("two.go", "package app\n\nconst two = 2\n")
	whole := fixture.rangeChanges(head).Changes
	fixture.saveReview("rp_1725192000000_00000000000000a1", "bugs", model.LifecycleCompleted, whole)
	fixture.saveReview("rp_1725192000000_00000000000000a2", "docs", model.LifecycleCompleted, whole)

	exit, stdout, stderr := fixture.check("pre-push", "--base", fixture.base, "--format", "json")
	if exit != 0 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout, stderr)
	}
	want := `{"checkpoint": "pre-push", "base": "` + fixture.base + `", "head": "` + head + `", "range_source": "flags", "state": "covered", "unreviewed_lines": 0, "profiles": [
		{"scope": "repository", "name": "bugs", "state": "covered", "reviews": ["rp_1725192000000_00000000000000a1"], "unreviewed_lines": 0, "budget_spent": 1, "review_budget": 0},
		{"scope": "repository", "name": "docs", "state": "covered", "reviews": ["rp_1725192000000_00000000000000a2"], "unreviewed_lines": 0, "budget_spent": 1, "review_budget": 0}]}`
	var got, wanted any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &wanted); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, wanted) {
		t.Fatalf("report = %s, want %s", stdout, want)
	}
}

func TestCheckpointPrePushCoveredByAReviewOfEachPath(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.commit("one.go", "package app\n\nconst one = 1\n")
	head := fixture.commit("two.go", "package app\n\nconst two = 2\n")
	whole := fixture.rangeChanges(head).Changes
	fixture.saveReview("rp_1725192000000_00000000000000b1", "bugs", model.LifecycleCompleted, whole[:1])
	fixture.saveReview("rp_1725192000000_00000000000000b2", "bugs", model.LifecycleCompleted, whole[1:])
	fixture.saveReview("rp_1725192000000_00000000000000b3", "docs", model.LifecycleCompleted, whole)

	exit, stdout, stderr := fixture.check("pre-push", "--base", fixture.base)
	if exit != 0 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout, stderr)
	}
	want := "pre-push checkpoint: " + fixture.base + ".." + head + " (range from --base)\n" +
		"bugs (repository): covered by rp_1725192000000_00000000000000b1, rp_1725192000000_00000000000000b2\n" +
		"docs (repository): covered by rp_1725192000000_00000000000000b3\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestCheckpointPrePushMissingPrintsRunCommand(t *testing.T) {
	fixture := newCheckpointFixture(t)
	head := fixture.commit("one.go", "package app\n\nconst one = 1\n")
	fixture.saveReview("rp_1725192000000_00000000000000c1", "bugs", model.LifecycleCompleted, fixture.rangeChanges(head).Changes)
	fixture.saveReview("rp_1725192000000_00000000000000c2", "docs", model.LifecycleIncomplete, fixture.rangeChanges(head).Changes)

	exit, stdout, stderr := fixture.check("pre-push", "--base", fixture.base)
	if exit != 1 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout, stderr)
	}
	for _, line := range []string{
		"bugs (repository): covered by rp_1725192000000_00000000000000c1\n",
		"docs (repository): missing, 3 unreviewed lines in one.go\n",
		"next: review-party run --unreviewed --base " + fixture.base + " --head " + head + " --repo " + shellQuoteArgument(fixture.repository) + "\n",
	} {
		if !strings.Contains(stdout, line) {
			t.Fatalf("stdout lacks %q:\n%s", line, stdout)
		}
	}
	if strings.Contains(stdout, "wait:") {
		t.Fatalf("missing coverage should not suggest waiting:\n%s", stdout)
	}

	absolute, err := filepath.Abs("relative.json")
	if err != nil {
		t.Fatal(err)
	}
	_, stdout, _ = fixture.check("pre-push", "--base", fixture.base, "--config", "relative.json")
	if line := " --repo " + shellQuoteArgument(fixture.repository) + " --config " + shellQuoteArgument(absolute) + "\n"; !strings.Contains(stdout, line) {
		t.Fatalf("a relative --config should be suggested as %q:\n%s", line, stdout)
	}
}

func TestCheckpointPrePushRunningPrintsWait(t *testing.T) {
	fixture := newCheckpointFixture(t)
	head := fixture.commit("one.go", "package app\n\nconst one = 1\n")
	whole := fixture.rangeChanges(head).Changes
	fixture.saveReview("rp_1725192000000_00000000000000d1", "bugs", model.LifecycleRunning, whole)
	fixture.saveReview("rp_1725192000000_00000000000000d2", "docs", model.LifecycleCompleted, whole)

	exit, stdout, stderr := fixture.check("pre-push", "--base", fixture.base, "--head", "HEAD")
	if exit != 1 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout, stderr)
	}
	want := "pre-push checkpoint: " + fixture.base + ".." + head + " (range from --base)\n" +
		"bugs (repository): running rp_1725192000000_00000000000000d1, 3 unreviewed lines in one.go\n" +
		"docs (repository): covered by rp_1725192000000_00000000000000d2\n" +
		"wait: review-party wait rp_1725192000000_00000000000000d1\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestCheckpointPreCommitWithPartialStagingIsUncovered(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.writeFile("app.go", "package app\n\nconst staged = true\n")
	fixture.git("add", "app.go")
	staged, err := subject.StagedContentChanges(fixture.repository)
	if err != nil {
		t.Fatal(err)
	}
	fixture.saveReview("rp_1725192000000_00000000000000e1", "bugs", model.LifecycleCompleted, staged)
	fixture.writeFile("notes.txt", "untracked\n")

	exit, stdout, stderr := fixture.check("pre-commit")
	if exit != 1 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout, stderr)
	}
	want := "pre-commit checkpoint: staged changes\n" +
		"bugs (repository): covered by rp_1725192000000_00000000000000e1\n" +
		"docs (repository): missing, 2 unreviewed lines in app.go\n" +
		"The Review must match the staged content, so stash or stage the rest of your changes first.\n" +
		"next: review-party run --unreviewed --repo " + shellQuoteArgument(fixture.repository) + "\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestCheckpointPreCommitRejectsRangeFlags(t *testing.T) {
	fixture := newCheckpointFixture(t)
	exit, _, stderr := fixture.check("pre-commit", "--base", fixture.base)
	if exit != usageExitCode || !strings.Contains(stderr, "only to pre-push") {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
}

func TestCheckpointPrePushWithoutDefaultBaseAsksForBase(t *testing.T) {
	fixture := newCheckpointFixture(t)
	exit, _, stderr := fixture.check("pre-push")
	if exit != usageExitCode || !strings.Contains(stderr, "pass --base") {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
}

func TestCheckpointPrePushDefaultsToMergeBaseWithDivergedUpstream(t *testing.T) {
	fixture := newCheckpointFixture(t)
	fixture.git("checkout", "-q", "-b", "published", fixture.base)
	fixture.commit("upstream.go", "package app\n\nconst upstream = 1\n")
	fixture.git("checkout", "-q", "-")
	fixture.git("branch", "--set-upstream-to=published")
	head := fixture.commit("one.go", "package app\n\nconst one = 1\n")

	exit, stdout, stderr := fixture.check("pre-push")
	if exit != 1 {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout, stderr)
	}
	if first := strings.SplitN(stdout, "\n", 2)[0]; first != "pre-push checkpoint: "+fixture.base+".."+head+" (base is the merge base with the upstream branch)" {
		t.Fatalf("first line = %q", first)
	}
}
