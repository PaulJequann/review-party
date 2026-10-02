package configurationhub

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"reviewparty/internal/configuration"
)

// recordingInstaller stands in for the hook installers: it asks for
// confirmation as the real ones do when hooks are pending, and records each
// install as "<integration> team|personal <confirmed>".
type recordingInstaller struct {
	installs []string
}

func (installer *recordingInstaller) install(integration configuration.IntegrationName, personal bool, confirm func() (bool, error)) error {
	confirmed, err := confirm()
	target := "team"
	if personal {
		target = "personal"
	}
	installer.installs = append(installer.installs, fmt.Sprintf("%s %s %t", integration, target, confirmed))
	return err
}

// checkpointJourney is a repository whose selection binds one global
// Profile, ready for the first-use journey to reach its Checkpoint step.
type checkpointJourney struct {
	manager    *configuration.Manager
	repository configuration.Repository
	installer  recordingInstaller
	onPath     []configuration.IntegrationName
}

func newCheckpointJourney(t *testing.T, profile string) *checkpointJourney {
	t.Helper()
	return newTemplateCheckpointJourney(t, profile, "")
}

// newTemplateCheckpointJourney selects a global Profile created from the
// Template, or from none when templateID is empty.
func newTemplateCheckpointJourney(t *testing.T, profile, templateID string) *checkpointJourney {
	t.Helper()
	journey := &checkpointJourney{manager: firstUseManager(t), repository: configuration.Repository(t.TempDir())}
	draft := configuration.ProfileDraft{
		Target: configuration.ScopeGlobal, Name: profile, Reviewer: "codex",
		Model: "luna", ReasoningEffort: "high", AttemptDeadline: "8m", Instructions: "Review.",
	}
	if templateID != "" {
		template, _ := journey.manager.Template(templateID)
		draft.TemplateID, draft.TemplateRevision = template.ID, template.Revision
	}
	plan, err := journey.manager.PlanProfileCreation("", draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := journey.manager.Publish(plan); err != nil {
		t.Fatal(err)
	}
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
		Context: ctx, Repository: journey.repository, Accessible: true, InstallCheckpointHooks: journey.installer.install, AgentsOnPath: journey.onPath,
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
		"0",            // team floor: git and agents-md, preselected
		"y",            // publish
		"",             // install the git hooks: default yes
		"",             // install the agents-md block: default yes
		"0",            // no personal agent hooks
	)

	declared, err := journey.manager.Checkpoints(journey.repository)
	want := configuration.NewCheckpoint()
	want.ExemptPaths, want.SmallChangeLines = []string{"*.md", "docs/**"}, 5
	want.Integrations = []configuration.IntegrationName{configuration.IntegrationGit, configuration.IntegrationAgentsMD}
	if err != nil || !reflect.DeepEqual(declared, map[configuration.CheckpointName]configuration.Checkpoint{configuration.CheckpointPrePush: want}) {
		t.Fatalf("declared = %#v, %v\noutput:\n%s", declared, err, output)
	}
	if !reflect.DeepEqual(journey.installer.installs, []string{"git team true", "agents-md team true"}) {
		t.Fatalf("installs = %q", journey.installer.installs)
	}
	if strings.Contains(output, "warning: exempting *.md") {
		t.Fatalf("warned without a documentation Profile:\n%s", output)
	}
}

func TestFirstUseWarnsWhenMarkdownExemptionsSkipADocumentationProfile(t *testing.T) {
	journey := newTemplateCheckpointJourney(t, "prose", configuration.DocumentationTemplateID)

	output := journey.run(t,
		"2",    // pre-commit
		"*.md", // exemptions
		"0",    // small-change lines
		"2",    // waivers: anyone
		"0",    // team floor: git and agents-md, preselected
		"n",    // do not publish
	)

	if !strings.Contains(output, "warning: exempting *.md means this Checkpoint never requires documentation Profile prose to review Markdown changes.\n") {
		t.Fatalf("missing documentation warning:\n%s", output)
	}
	declared, err := journey.manager.Checkpoints(journey.repository)
	if err != nil {
		t.Fatal(err)
	}
	if len(declared) != 0 || len(journey.installer.installs) != 0 {
		t.Fatalf("declined plan declared %#v, installs %q", declared, journey.installer.installs)
	}
}

func TestFirstUseDoesNotTakeAProfileNamedForDocsAsADocumentationReview(t *testing.T) {
	journey := newTemplateCheckpointJourney(t, "docs", "bugs")

	output := journey.run(t, "", "*.md", "0", "", "0", "n")

	if strings.Contains(output, "warning: exempting *.md") {
		t.Fatalf("warned for a bugs Profile named docs:\n%s", output)
	}
}

func TestFirstUseSummarizesDeclaredCheckpointsWithoutAskingAgain(t *testing.T) {
	journey := newCheckpointJourney(t, "bugs")
	declared := configuration.NewCheckpoint()
	declared.Waivers = configuration.WaiversNone
	declared.Integrations = []configuration.IntegrationName{configuration.IntegrationGit}
	plan, err := journey.manager.Plan(journey.repository, []configuration.Intent{configuration.SetCheckpoint{Name: configuration.CheckpointPreCommit, Checkpoint: declared}})
	if err != nil {
		t.Fatal(err)
	}
	if err := journey.manager.Publish(plan); err != nil {
		t.Fatal(err)
	}

	output := journey.run(t, "n", "0")

	if !strings.Contains(output, "Checkpoint pre-commit: reviewed, waivers none, integrations git\n") || strings.Contains(output, "Which Review Checkpoint") {
		t.Fatalf("output:\n%s", output)
	}
	if !reflect.DeepEqual(journey.installer.installs, []string{"git team false"}) {
		t.Fatalf("installs = %q", journey.installer.installs)
	}
}

func TestFirstUsePreselectsAgentsOnPathInTheFloor(t *testing.T) {
	journey := newCheckpointJourney(t, "bugs")
	journey.onPath = []configuration.IntegrationName{configuration.IntegrationCodex}

	output := journey.run(t,
		"",  // pre-push
		"",  // no exemptions
		"0", // small-change lines
		"",  // waivers: human
		"0", // team floor: git, codex, and agents-md, preselected
		"y", // publish
		"",  // install the git hooks
		"",  // install the codex hook
		"",  // install the agents-md block
		"0", // claude-code is not on PATH, so not preselected
	)

	declared, err := journey.manager.Checkpoints(journey.repository)
	want := []configuration.IntegrationName{configuration.IntegrationGit, configuration.IntegrationCodex, configuration.IntegrationAgentsMD}
	if err != nil || !reflect.DeepEqual(declared[configuration.CheckpointPrePush].Integrations, want) {
		t.Fatalf("declared = %#v, %v\noutput:\n%s", declared, err, output)
	}
	if !reflect.DeepEqual(journey.installer.installs, []string{"git team true", "codex team true", "agents-md team true"}) {
		t.Fatalf("installs = %q", journey.installer.installs)
	}
}

func TestFirstUseOffersAgentsOutsideTheFloorAsPersonalHooks(t *testing.T) {
	journey := newCheckpointJourney(t, "bugs")
	journey.onPath = []configuration.IntegrationName{configuration.IntegrationClaudeCode, configuration.IntegrationCodex}
	declared := configuration.NewCheckpoint()
	declared.Integrations = []configuration.IntegrationName{configuration.IntegrationCodex}
	plan, err := journey.manager.Plan(journey.repository, []configuration.Intent{configuration.SetCheckpoint{Name: configuration.CheckpointPrePush, Checkpoint: declared}})
	if err != nil {
		t.Fatal(err)
	}
	if err := journey.manager.Publish(plan); err != nil {
		t.Fatal(err)
	}

	output := journey.run(t,
		"y", // install the codex team hook
		"0", // personal: claude-code, preselected
		"n", // decline its write
	)

	if strings.Contains(output, "git hooks now") || strings.Contains(output, "agents-md: a block") {
		t.Fatalf("offered a team-file Integration outside the floor:\n%s", output)
	}
	if !reflect.DeepEqual(journey.installer.installs, []string{"codex team true", "claude-code personal false"}) {
		t.Fatalf("installs = %q\noutput:\n%s", journey.installer.installs, output)
	}
}
