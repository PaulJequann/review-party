package main

import (
	"bytes"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
)

var baselineExecutionFlags = []string{"--reviewer", "codex", "--model", "luna", "--effort", "high", "--deadline", "8m"}

var baselineSelection = []configuration.SelectionItem{{Party: configuration.BaselinePartyName}}

func newBaselineInitFixture(t *testing.T) initFixture {
	t.Helper()
	return newInitFixture(t,
		configuration.Template{ID: "bugs", Revision: "bugs-v1", Instructions: "BUGS\n", Baseline: true},
		configuration.Template{ID: "docs", Revision: "docs-v1", Instructions: "DOCS\n", Baseline: true},
		configuration.Template{ID: "security", Revision: "security-v1", Instructions: "SECURITY\n"},
	)
}

func (fixture initFixture) baseline(t *testing.T, arguments ...string) (int, string, string) {
	t.Helper()
	return fixture.run(t, commandIO{}, append([]string{"--baseline"}, arguments...)...)
}

func (fixture initFixture) requireBaseline(t *testing.T) configuration.Baseline {
	t.Helper()
	baseline, err := fixture.manager.Baseline(configuration.Repository(fixture.repository))
	if err != nil {
		t.Fatal(err)
	}
	return baseline
}

func (fixture initFixture) requireCompleteBaseline(t *testing.T) {
	t.Helper()
	baseline := fixture.requireBaseline(t)
	for _, member := range baseline.Members {
		if member.State != configuration.BaselineReady {
			t.Fatalf("member %s = %s", member.Template.ID, member.State)
		}
	}
	if baseline.Party.State != configuration.BaselineReady {
		t.Fatalf("Party state = %s", baseline.Party.State)
	}
}

// requireCreatedMembers checks that each baseline Profile carries the
// execution baselineExecutionFlags chose and its Template's identity.
func (fixture initFixture) requireCreatedMembers(t *testing.T) {
	t.Helper()
	for _, template := range fixture.templates {
		if !template.Baseline {
			continue
		}
		profile, _, err := fixture.manager.LoadProfile(configuration.ScopeGlobal, configuration.Repository(fixture.repository), template.ID)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{profile.Reviewer, profile.Model, profile.ReasoningEffort, profile.AttemptDeadline, profile.TemplateID, profile.TemplateRevision}
		if want := []string{"codex", "luna", "high", "8m", template.ID, template.Revision}; !slices.Equal(got, want) {
			t.Fatalf("Profile %s = %v, want %v", template.ID, got, want)
		}
	}
}

// differingBaselineParty publishes a Global Party baseline whose only member
// is a Global Profile bugs that no Template created.
func (fixture initFixture) differingBaselineParty(t *testing.T) {
	t.Helper()
	fixture.profile(t, configuration.ScopeGlobal, "bugs")
	plan, err := fixture.manager.PlanPartyCreation(configuration.Repository(fixture.repository), configuration.PartyDraft{
		Target: configuration.ScopeGlobal, Name: configuration.BaselinePartyName, ConcurrencyLimit: 1,
		Profiles: []configuration.ProfileReference{{Scope: configuration.ScopeGlobal, Profile: "bugs"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
}

// written is every file under the Global root and the repository's
// configuration directory, by path.
func (fixture initFixture) written(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, root := range []string{fixture.globalRoot, filepath.Join(fixture.repository, ".reviewparty")} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			content, err := os.ReadFile(path)
			files[path] = string(content)
			return err
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	return files
}

func TestInitBaselineCreatesProfilesPartyAndSelectionThenRerunWritesNothing(t *testing.T) {
	fixture := newBaselineInitFixture(t)

	exit, _, stderr := fixture.baseline(t, append(baselineExecutionFlags, "--yes")...)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	fixture.requireCompleteBaseline(t)
	fixture.requireCreatedMembers(t)
	party, _, err := fixture.manager.LoadParty(configuration.ScopeGlobal, configuration.Repository(fixture.repository), configuration.BaselinePartyName)
	if err != nil || party.ConcurrencyLimit != 2 {
		t.Fatalf("Party = %#v, error %v", party, err)
	}
	selection, _ := fixture.selection(t)
	if want := (configuration.ReviewSelection{ConcurrencyLimit: 2, Global: baselineSelection, Repository: []configuration.SelectionItem{}}); !reflect.DeepEqual(selection, want) {
		t.Fatalf("selection = %#v", selection)
	}

	before := fixture.written(t)
	exit, stdout, stderr := fixture.baseline(t)
	if exit != 0 || stdout != "The Review selection already includes the Review Party baseline; it was not changed.\n" {
		t.Fatalf("rerun exit = %d, stdout = %q, stderr = %q", exit, stdout, stderr)
	}
	if after := fixture.written(t); !maps.Equal(after, before) {
		t.Fatalf("rerun wrote files:\nbefore %v\nafter %v", slices.Sorted(maps.Keys(before)), slices.Sorted(maps.Keys(after)))
	}
}

func TestInitBaselineKeepsAnExistingProfileAsItIs(t *testing.T) {
	fixture := newBaselineInitFixture(t)
	fixture.profile(t, configuration.ScopeGlobal, "bugs")
	path := filepath.Join(fixture.globalRoot, "profiles", "bugs", "profile.json")
	kept, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	exit, stdout, stderr := fixture.baseline(t, append(baselineExecutionFlags, "--yes")...)
	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	if !strings.Contains(stdout, "Kept Global Profile bugs, which was not created from Template bugs") {
		t.Fatalf("stdout = %q", stdout)
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, kept) {
		t.Fatalf("existing Profile changed: %v\n%s", err, after)
	}
	if state := fixture.requireBaseline(t).Party.State; state != configuration.BaselineReady {
		t.Fatalf("Party state = %s", state)
	}
}

func TestInitBaselineRefusesBeforeWritingWithoutExecutionForMissingMembers(t *testing.T) {
	fixture := newBaselineInitFixture(t)

	exit, _, stderr := fixture.baseline(t, "--yes")

	if exit == 0 || !strings.Contains(stderr, "Global Profiles bugs, docs are missing; add --reviewer --model --effort --deadline") {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	if written := fixture.written(t); len(written) != 0 {
		t.Fatalf("wrote %v", slices.Sorted(maps.Keys(written)))
	}
}

func TestInitExecutionFlagsAreAllOrNoneAndRequireBaseline(t *testing.T) {
	fixture := newBaselineInitFixture(t)
	for _, arguments := range [][]string{
		append(slices.Clone(baselineExecutionFlags), "--yes"),
		{"--baseline", "--reviewer", "codex", "--yes"},
	} {
		exit, _, stderr := fixture.run(t, commandIO{}, arguments...)
		if exit == 0 || stderr == "" {
			t.Fatalf("%v: exit = %d, stderr = %q", arguments, exit, stderr)
		}
	}
	if written := fixture.written(t); len(written) != 0 {
		t.Fatalf("wrote %v", slices.Sorted(maps.Keys(written)))
	}
}

func TestInitBaselineRequiresYesWithoutATerminal(t *testing.T) {
	fixture := newBaselineInitFixture(t)

	exit, _, stderr := fixture.baseline(t, baselineExecutionFlags...)

	if exit == 0 || !strings.Contains(stderr, "requires --yes") {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	if written := fixture.written(t); len(written) != 0 {
		t.Fatalf("wrote %v", slices.Sorted(maps.Keys(written)))
	}
}

func TestInitBaselineDifferingPartyBlocksEveryStep(t *testing.T) {
	fixture := newBaselineInitFixture(t)
	fixture.differingBaselineParty(t)
	before := fixture.written(t)

	exit, _, stderr := fixture.baseline(t, append(baselineExecutionFlags, "--yes")...)

	if blocked := "Review Party baseline blocked: Global Party \"baseline\" at " + filepath.Join(fixture.globalRoot, "parties", "baseline.json"); exit == 0 || !strings.Contains(stderr, blocked) {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	if after := fixture.written(t); !maps.Equal(after, before) {
		t.Fatalf("blocked run wrote files: %v", slices.Sorted(maps.Keys(after)))
	}
}

func TestInitBaselineKeepsAnExistingSelectionLimitWithANote(t *testing.T) {
	fixture := newBaselineInitFixture(t)
	fixture.profile(t, configuration.ScopeRepository, "local")
	if exit, _, stderr := fixture.run(t, commandIO{}, "--profile", "local", "--yes"); exit != 0 {
		t.Fatalf("setup exit = %d, stderr = %q", exit, stderr)
	}

	exit, stdout, stderr := fixture.baseline(t, append(baselineExecutionFlags, "--yes")...)

	if exit != 0 || !strings.Contains(stdout, "keeps Concurrency Limit 1, below the baseline's 2 Profiles") {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout, stderr)
	}
	selection, _ := fixture.selection(t)
	want := configuration.ReviewSelection{ConcurrencyLimit: 1, Global: baselineSelection, Repository: []configuration.SelectionItem{{Profile: "local"}}}
	if !reflect.DeepEqual(selection, want) {
		t.Fatalf("selection = %#v", selection)
	}
}

func TestInitWithoutSelectionOffersTheBaselineFirst(t *testing.T) {
	fixture := newBaselineInitFixture(t)

	exit, stdout, stderr := fixture.run(t, commandIO{})

	want := "Review Party baseline (bugs, docs): review-party init --repo " + fixture.repository + " --baseline" + profileExecutionPlaceholders + "\n"
	if exit != 0 || !strings.HasPrefix(stdout, want) {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", exit, stdout, stderr)
	}
}

func TestInitBaselineBindsATeammateCloneWithoutTouchingRepositoryConfiguration(t *testing.T) {
	author := newBaselineInitFixture(t)
	if exit, _, stderr := author.baseline(t, append(baselineExecutionFlags, "--yes")...); exit != 0 {
		t.Fatalf("author exit = %d, stderr = %q", exit, stderr)
	}
	committed := author.repositoryConfiguration(t)
	teammate := author.onMachine(t)

	exit, stdout, _ := teammate.run(t, commandIO{})
	want := "Missing Global Party baseline (Review Party baseline): review-party init --repo " + teammate.repository + " --baseline" + profileExecutionPlaceholders + "\n"
	if exit != 0 || stdout != want {
		t.Fatalf("report exit = %d, stdout = %q", exit, stdout)
	}

	if exit, _, stderr := teammate.baseline(t, append(baselineExecutionFlags, "--yes")...); exit != 0 {
		t.Fatalf("bind exit = %d, stderr = %q", exit, stderr)
	}
	teammate.requireCompleteBaseline(t)
	if !bytes.Equal(teammate.repositoryConfiguration(t), committed) {
		t.Fatal("binding changed Repository Configuration")
	}
	if exit, stdout, _ := teammate.run(t, commandIO{}); exit != 0 || !strings.HasPrefix(stdout, "Repository is ready") {
		t.Fatalf("after binding exit = %d, stdout = %q", exit, stdout)
	}
}

func TestInitBaselineCompletesThePartyAndSelectionFromExistingMembers(t *testing.T) {
	fixture := newBaselineInitFixture(t)
	plan, err := fixture.manager.PlanBaselineProfiles(configuration.Repository(fixture.repository), configuration.ProfileExecution{
		Reviewer: "codex", Model: "luna", ReasoningEffort: "high", AttemptDeadline: "8m",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.manager.Publish(plan); err != nil {
		t.Fatal(err)
	}

	exit, _, stderr := fixture.baseline(t, "--yes")

	if exit != 0 {
		t.Fatalf("exit = %d, stderr = %q", exit, stderr)
	}
	fixture.requireCompleteBaseline(t)
	if selection, _ := fixture.selection(t); !reflect.DeepEqual(selection.Global, baselineSelection) {
		t.Fatalf("selection = %#v", selection)
	}
}

func TestInitBaselineRecreatesAMemberMissingFromAReadyParty(t *testing.T) {
	fixture := newBaselineInitFixture(t)
	if exit, _, stderr := fixture.baseline(t, append(baselineExecutionFlags, "--yes")...); exit != 0 {
		t.Fatalf("setup exit = %d, stderr = %q", exit, stderr)
	}
	committed := fixture.repositoryConfiguration(t)
	if err := os.RemoveAll(filepath.Join(fixture.globalRoot, "profiles", "docs")); err != nil {
		t.Fatal(err)
	}

	exit, stdout, _ := fixture.run(t, commandIO{})
	want := "Missing Global Profiles docs (Review Party baseline): review-party init --repo " + fixture.repository + " --baseline" + profileExecutionPlaceholders + "\n"
	if exit != 0 || stdout != want {
		t.Fatalf("report exit = %d, stdout = %q", exit, stdout)
	}

	if exit, _, stderr := fixture.baseline(t, append(baselineExecutionFlags, "--yes")...); exit != 0 {
		t.Fatalf("bind exit = %d, stderr = %q", exit, stderr)
	}
	fixture.requireCompleteBaseline(t)
	fixture.requireCreatedMembers(t)
	if !bytes.Equal(fixture.repositoryConfiguration(t), committed) {
		t.Fatal("binding changed Repository Configuration")
	}
}

func TestInitReportsASelectedDifferingBaselineAsBlockedNotReady(t *testing.T) {
	author := newBaselineInitFixture(t)
	if exit, _, stderr := author.baseline(t, append(baselineExecutionFlags, "--yes")...); exit != 0 {
		t.Fatalf("author exit = %d, stderr = %q", exit, stderr)
	}
	teammate := author.onMachine(t)
	teammate.differingBaselineParty(t)

	exit, stdout, _ := teammate.run(t, commandIO{})

	want := "Review Party baseline blocked: Global Party \"baseline\" at " + filepath.Join(teammate.globalRoot, "parties", "baseline.json") +
		" composes Profiles other than bugs, docs; edit or remove that file to use the baseline\n"
	if exit != 0 || stdout != want {
		t.Fatalf("exit = %d, stdout = %q", exit, stdout)
	}
}
