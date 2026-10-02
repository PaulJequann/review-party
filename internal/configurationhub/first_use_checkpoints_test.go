package configurationhub

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/configuration"
)

// recordingInstaller stands in for the git hook installer: it asks for
// confirmation as the real one does when hooks are pending.
type recordingInstaller struct {
	calls     int
	confirmed []bool
}

func (installer *recordingInstaller) install(confirm func() (bool, error)) error {
	installer.calls++
	confirmed, err := confirm()
	installer.confirmed = append(installer.confirmed, confirmed)
	return err
}

// checkpointJourney is a repository whose selection binds one global
// Profile, ready for the first-use journey to reach its Checkpoint step.
type checkpointJourney struct {
	manager    *configuration.Manager
	repository configuration.Repository
	installer  recordingInstaller
}

func newCheckpointJourney(t *testing.T, profile string) *checkpointJourney {
	t.Helper()
	journey := &checkpointJourney{manager: firstUseManager(t), repository: configuration.Repository(t.TempDir())}
	publishGlobalProfile(t, journey.manager, profile)
	publishSelection(t, journey.manager, journey.repository, configuration.ReviewSelection{
		ConcurrencyLimit: 1, Global: []configuration.SelectionItem{{Profile: profile}},
	})
	return journey
}

func (journey *checkpointJourney) run(t *testing.T, script ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var output bytes.Buffer
	err := RunFirstUse(journey.manager, RunOptions{
		Context: ctx, Repository: journey.repository, Accessible: true, InstallCheckpointHooks: journey.installer.install,
		Input: newHoldingInput(strings.Join(script, "\n") + "\n"), Output: &output,
	})
	if err != nil {
		t.Fatalf("RunFirstUse: %v\noutput:\n%s", err, output.String())
	}
	return output.String()
}

func TestFirstUseDeclaresACheckpointThenOffersItsHooks(t *testing.T) {
	journey := newCheckpointJourney(t, "bugs")

	output := journey.run(t,
		"",             // pre-push, the default
		"*.md docs/**", // exemptions
		"5",            // small-change lines
		"",             // waivers: human, the default
		"y",            // publish
		"",             // install the hooks: default yes
	)

	declared, err := journey.manager.Checkpoints(journey.repository)
	want := configuration.NewCheckpoint()
	want.ExemptPaths, want.SmallChangeLines = []string{"*.md", "docs/**"}, 5
	want.Integrations = []configuration.IntegrationName{configuration.IntegrationGit}
	if err != nil || !reflect.DeepEqual(declared, map[configuration.CheckpointName]configuration.Checkpoint{configuration.CheckpointPrePush: want}) {
		t.Fatalf("declared = %#v, %v\noutput:\n%s", declared, err, output)
	}
	if !reflect.DeepEqual(journey.installer.confirmed, []bool{true}) {
		t.Fatalf("installer confirmations = %v", journey.installer.confirmed)
	}
	if strings.Contains(output, "warning: exempting *.md") {
		t.Fatalf("warned without a documentation Profile:\n%s", output)
	}
}

func TestFirstUseWarnsWhenMarkdownExemptionsSkipADocumentationProfile(t *testing.T) {
	journey := newCheckpointJourney(t, "documentation")

	output := journey.run(t,
		"2",    // pre-commit
		"*.md", // exemptions
		"0",    // small-change lines
		"2",    // waivers: anyone
		"n",    // do not publish
	)

	if !strings.Contains(output, "warning: exempting *.md means this Checkpoint never requires documentation Profile documentation to review Markdown changes.\n") {
		t.Fatalf("missing documentation warning:\n%s", output)
	}
	declared, err := journey.manager.Checkpoints(journey.repository)
	if err != nil {
		t.Fatal(err)
	}
	if outcome := [2]int{len(declared), journey.installer.calls}; outcome != [2]int{} {
		t.Fatalf("declined plan declared %#v, installer calls %d", declared, journey.installer.calls)
	}
}

func TestFirstUseSummarizesDeclaredCheckpointsWithoutAskingAgain(t *testing.T) {
	journey := newCheckpointJourney(t, "bugs")
	declared := configuration.NewCheckpoint()
	declared.Waivers = configuration.WaiversNone
	plan, err := journey.manager.Plan(journey.repository, []configuration.Intent{configuration.SetCheckpoint{Name: configuration.CheckpointPreCommit, Checkpoint: declared}})
	if err != nil {
		t.Fatal(err)
	}
	if err := journey.manager.Publish(plan); err != nil {
		t.Fatal(err)
	}

	output := journey.run(t, "n")

	if !strings.Contains(output, "Checkpoint pre-commit: reviewed, waivers none\n") || strings.Contains(output, "Which Review Checkpoint") {
		t.Fatalf("output:\n%s", output)
	}
	if !reflect.DeepEqual(journey.installer.confirmed, []bool{false}) {
		t.Fatalf("installer confirmations = %v", journey.installer.confirmed)
	}
}
