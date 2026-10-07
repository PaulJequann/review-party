package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"reviewparty/internal/configuration"
	"reviewparty/internal/subject"
)

func newCheckpointUninstallCommand(streams commandIO) *cobra.Command {
	command := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove what Checkpoint install added to this repository",
		Long: `Remove the git hook blocks, Caller Agent PreToolUse entries, and agents-md
block that review-party checkpoint install added, and nothing else. A file
install created is deleted once nothing but install's own skeleton is left
in it; a file you had before install is only edited. A block or entry someone
edited, and configuration install never writes, such as lefthook, are
reported for you to remove by hand.

--undeclared removes only what no declared Checkpoint uses. --shared also
changes files other repositories may share: the Codex entry in
$CODEX_HOME/hooks.json, even inside this clone; any file that resolves outside
this clone; and hooks in a directory a core.hooksPath names anywhere but the
clone's own config or config.worktree file, even one this clone overrides,
unless it is a relative path that stays inside each repository. Rerunning is
safe.`,
		Example: "  review-party checkpoint uninstall\n  review-party checkpoint uninstall --undeclared --yes",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options := uninstallOptions{
				repository: stringFlag(cmd, "repo"), configuration: stringFlag(cmd, "config"),
				undeclared: boolFlag(cmd, "undeclared"), shared: boolFlag(cmd, "shared"), yes: boolFlag(cmd, "yes"),
			}
			return commandResult(executeCheckpointUninstall(options, streams))
		},
	}
	addRepositoryFlag(command, "Git repository to remove Checkpoint Integrations from")
	addConfigurationFlag(command)
	command.Flags().Bool("undeclared", false, "Remove only what no declared Checkpoint uses")
	command.Flags().Bool("shared", false, "Also change files other repositories may share: $CODEX_HOME/hooks.json, files outside this clone, and hooks in a shared core.hooksPath")
	command.Flags().Bool("yes", false, "Remove without a confirmation prompt")
	command.MarkFlagsMutuallyExclusive("undeclared", "shared")
	return command
}

type uninstallOptions struct {
	repository    string
	configuration string
	undeclared    bool
	shared        bool
	yes           bool
}

var (
	errUninstallUnconfirmed = errors.New("rerun with --yes to remove them without a terminal")
	errNothingRemoved       = errors.New("nothing removed")
	errUninstallIncomplete  = errors.New("some review-party hooks or blocks are left; remove them as shown above")
)

func executeCheckpointUninstall(options uninstallOptions, streams commandIO) int {
	root, err := subject.ResolveRepositoryRoot(options.repository)
	if err != nil {
		return printCommandError(streams.errors, usageExitCode, err)
	}
	var plan uninstallPlan
	if options.undeclared {
		plan, err = planUndeclaredUninstall(root, streams.configurationManager(options.configuration))
	} else {
		plan, err = planUninstall(root, uninstallScope{checkpoints: configuration.CheckpointNames(), agents: true, shared: options.shared})
	}
	if err == nil {
		err = plan.run(streams.output, streams.confirmation(options.yes, "Remove these?", errUninstallUnconfirmed, errNothingRemoved))
	}
	switch {
	case errors.Is(err, errUninstallUnconfirmed):
		return printCommandError(streams.errors, usageExitCode, err)
	case err != nil:
		return printFailure(streams.errors, err)
	}
	return 0
}

func undeclaredUninstallCommand(root, config string) string {
	return "review-party checkpoint uninstall --undeclared --repo " + shellWord(root) + configurationArgument(config)
}

func offerUndeclaredUninstall(streams commandIO, options uninstallOptions) error {
	root, err := subject.ResolveRepositoryRoot(options.repository)
	if err != nil {
		return err
	}
	plan, err := planUndeclaredUninstall(root, streams.configurationManager(options.configuration))
	if err != nil || len(plan.steps) == 0 {
		return err
	}
	command := undeclaredUninstallCommand(root, options.configuration)
	switch {
	case options.yes, !plan.changes(), !streams.interactive():
		return writeCommandOutput(streams.output, func(output *commandOutput) {
			output.write("Installed Checkpoint Integrations serve no declared Checkpoint; remove them with: %s\n", command)
		})
	}
	return promptUndeclaredUninstall(streams, plan, command)
}

func promptUndeclaredUninstall(streams commandIO, plan uninstallPlan, command string) error {
	err := writeCommandOutput(streams.output, func(output *commandOutput) {
		output.write("These installed Checkpoint Integrations serve no declared Checkpoint:\n")
	})
	if err == nil {
		err = plan.run(streams.output, streams.confirmation(false, "Remove them?", errUninstallUnconfirmed, errNothingRemoved))
	}
	if errors.Is(err, errNothingRemoved) {
		return writeCommandOutput(streams.output, func(output *commandOutput) {
			output.write("Left in place; remove them later with: %s\n", command)
		})
	}
	return err
}

type uninstallScope struct {
	checkpoints []configuration.CheckpointName
	agents      bool
	shared      bool
}

func planUndeclaredUninstall(root string, manager *configuration.Manager) (uninstallPlan, error) {
	declared, err := manager.Checkpoints(configuration.Repository(root))
	if err != nil {
		return uninstallPlan{}, err
	}
	scope := uninstallScope{agents: len(declared) == 0}
	for _, name := range configuration.CheckpointNames() {
		if _, found := declared[name]; !found {
			scope.checkpoints = append(scope.checkpoints, name)
		}
	}
	return planUninstall(root, scope)
}

type uninstallSurface struct {
	integration configuration.IntegrationName
	checkpoint  configuration.CheckpointName
	path        string
	created     []byte
	remove      func(content []byte) surfaceRemoval
	machineWide bool
	withheld    bool
}

type surfaceRemoval struct {
	residue []byte
	removed removalOutcome
	left    removalOutcome
	manual  []string
}

type removalOutcome string

const (
	removedBlock  removalOutcome = "remove the review-party block from"
	removedEntry  removalOutcome = "remove the review-party PreToolUse entry from"
	removedFile   removalOutcome = "delete the file install created at"
	leftBlock     removalOutcome = "edited review-party block left in"
	leftEntry     removalOutcome = "edited review-party entry left in"
	leftByHand    removalOutcome = "remove by hand from"
	leftMalformed removalOutcome = "fix the review-party markers by hand in"
)

func (outcome removalOutcome) remains() bool {
	switch outcome {
	case leftBlock, leftEntry, leftByHand, leftMalformed:
		return true
	case removedBlock, removedEntry, removedFile:
	}
	return false
}

func uninstallSurfaces(root string, scope uninstallScope) ([]uninstallSurface, error) {
	locations, err := subject.ResolveHookLocations(root)
	if err != nil {
		return nil, err
	}
	var surfaces []uninstallSurface
	for _, name := range scope.checkpoints {
		surfaces = append(surfaces, gitUninstallSurfaces(root, locations, name)...)
	}
	if scope.agents {
		agents, err := agentUninstallSurfaces(root, scope.shared)
		if err != nil {
			return nil, err
		}
		surfaces = append(surfaces, agents...)
		created := []byte(agentsMDBegin + "\n" + agentsMDEnd + "\n")
		for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
			surfaces = append(surfaces, uninstallSurface{integration: configuration.IntegrationAgentsMD, path: filepath.Join(root, name), created: created, remove: removeAgentsMDBlock})
		}
	}
	shared := newSharedLocations(root, locations, surfaces)
	seen := map[string]bool{}
	surfaces = slices.DeleteFunc(surfaces, func(surface uninstallSurface) bool {
		key := resolvedPath(surface.path) + "\x00" + string(surface.checkpoint)
		duplicate := seen[key]
		seen[key] = true
		return duplicate
	})
	for index := range surfaces {
		surfaces[index].withheld = !scope.shared && shared.holds(surfaces[index].path)
	}
	return surfaces, nil
}

// sharedLocations are where a file may serve other repositories: anything
// outside the working tree and the git common dir, a personal agent file
// every repository reads, and every hooks directory configuration other than
// this clone's own points git at, even one this clone overrides. Paths are
// resolved, so a symlink or another spelling of a shared location is still
// shared.
type sharedLocations struct {
	clone []string
	files []string
	hooks []string
}

func newSharedLocations(root string, locations subject.HookLocations, surfaces []uninstallSurface) sharedLocations {
	shared := sharedLocations{clone: []string{resolvedPath(root), resolvedPath(locations.Common)}}
	for _, directory := range locations.SharedHooks {
		shared.hooks = append(shared.hooks, resolvedPath(directory))
	}
	for _, surface := range surfaces {
		if surface.machineWide {
			shared.files = append(shared.files, resolvedPath(surface.path))
		}
	}
	return shared
}

func (shared sharedLocations) holds(path string) bool {
	resolved := resolvedPath(path)
	inside := slices.ContainsFunc(shared.clone, func(directory string) bool { return insideRoot(directory, resolved) })
	return !inside || slices.Contains(shared.files, resolved) || slices.Contains(shared.hooks, resolvedPath(filepath.Dir(path)))
}

func gitUninstallSurfaces(root string, locations subject.HookLocations, name configuration.CheckpointName) []uninstallSurface {
	created := []byte("#!/bin/sh\n" + checkpointHookBlock(name))
	var surfaces []uninstallSurface
	for _, directory := range []string{filepath.Join(root, ".husky"), locations.Directory, filepath.Join(locations.Common, "hooks")} {
		surfaces = append(surfaces, uninstallSurface{
			integration: configuration.IntegrationGit, checkpoint: name, path: filepath.Join(directory, string(name)),
			created: created, remove: hookBlockRemoval(name),
		})
	}
	for _, marker := range hookToolMarkers {
		if marker.tool != hookToolHusky {
			surfaces = append(surfaces, uninstallSurface{
				integration: configuration.IntegrationGit, checkpoint: name, path: filepath.Join(root, marker.path),
				remove: managerHookRemoval(name),
			})
		}
	}
	return surfaces
}

func managerHookRemoval(name configuration.CheckpointName) func([]byte) surfaceRemoval {
	return func(content []byte) surfaceRemoval {
		calls := managerHookCalls(content, name)
		if len(calls) == 0 {
			return surfaceRemoval{residue: content}
		}
		return surfaceRemoval{residue: content, left: leftByHand, manual: calls}
	}
}

func hookBlockRemoval(name configuration.CheckpointName) func([]byte) surfaceRemoval {
	block := checkpointHookBlock(name)
	start, end := hookBlockMarkers(name)
	traces := []string{start, end, "review-party checkpoint hook git " + string(name)}
	return func(content []byte) surfaceRemoval {
		result := surfaceRemoval{residue: content}
		if bytes.Contains(content, []byte(block)) {
			result.residue, result.removed = bytes.ReplaceAll(content, []byte(block), nil), removedBlock
		}
		if slices.ContainsFunc(traces, func(trace string) bool { return bytes.Contains(result.residue, []byte(trace)) }) {
			result.left = leftBlock
		}
		return result
	}
}

func agentUninstallSurfaces(root string, shared bool) ([]uninstallSurface, error) {
	var surfaces []uninstallSurface
	for _, agent := range agentIntegrations {
		created, err := appendJSONArrayElement(nil, preToolUsePath, agent.group())
		if err != nil {
			return nil, err
		}
		surfaces = append(surfaces, uninstallSurface{integration: agent.integration, path: filepath.Join(root, agent.teamFile), created: created, remove: agent.removeEntry})
		personal, err := agent.personalFile(root)
		switch {
		case err == nil:
			surfaces = append(surfaces, uninstallSurface{integration: agent.integration, path: personal, created: created, remove: agent.removeEntry, machineWide: agent.machineWide})
		case shared:
			return nil, err
		}
	}
	return surfaces, nil
}

func insideRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (agent agentIntegration) removeEntry(content []byte) surfaceRemoval {
	residue, removed, err := removeJSONArrayElement(content, preToolUsePath, agent.group())
	outcome := hookEntryAdded
	if err == nil {
		outcome, err = findAgentHookEntry(residue, agent)
	}
	switch {
	case err != nil && bytes.Contains(content, []byte(agent.hookCall())):
		return surfaceRemoval{residue: content, left: leftByHand, manual: []string{"review-party cannot read the file: " + err.Error()}}
	case err != nil:
		return surfaceRemoval{residue: content}
	}
	result := surfaceRemoval{residue: residue}
	if removed {
		result.removed = removedEntry
	}
	if outcome != hookEntryAdded || bytes.Contains(residue, []byte(agent.hookCall())) {
		result.left = leftEntry
	}
	return result
}

func removeAgentsMDBlock(content []byte) surfaceRemoval {
	lines := strings.SplitAfter(string(content), "\n")
	sequence, at := agentsMDMarkers(lines)
	switch sequence {
	case "":
		return surfaceRemoval{residue: content}
	case "be":
	default:
		return surfaceRemoval{residue: content, left: leftMalformed, manual: []string{
			sequence.problem(),
			"Delete the marker lines and the review-party lines between them, then rerun the uninstall.",
		}}
	}
	before, after := strings.Join(lines[:at[0]], ""), strings.Join(lines[at[1]+1:], "")
	if after == "" && strings.HasSuffix(before, "\n\n") {
		before = before[:len(before)-1]
	}
	return surfaceRemoval{residue: []byte(before + after), removed: removedBlock}
}

type uninstallStep struct {
	integration configuration.IntegrationName
	checkpoint  configuration.CheckpointName
	path        string
	outcome     removalOutcome
	manual      []string
}

type uninstallPlan struct {
	steps   []uninstallStep
	writes  map[string][]byte
	deletes []createdFile
	record  installRecord
	notes   []string
}

func planUninstall(root string, scope uninstallScope) (uninstallPlan, error) {
	surfaces, err := uninstallSurfaces(root, scope)
	if err != nil {
		return uninstallPlan{}, err
	}
	record, err := readInstallRecord(root)
	if err != nil {
		return uninstallPlan{}, err
	}
	plan := uninstallPlan{writes: map[string][]byte{}, record: record}
	if record.unreadable {
		plan.notes = append(plan.notes, "note: "+record.path+" is not a record install wrote, so files install created are edited, not deleted")
	}
	for _, surface := range surfaces {
		content, err := os.ReadFile(surface.path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			plan.record.forget(recordedPath(surface.path))
		case err != nil:
			return uninstallPlan{}, fmt.Errorf("read %s: %w", surface.path, err)
		case surface.withheld:
			plan.withhold(surface, surface.remove(content))
		default:
			plan.survey(surface, surface.remove(content))
		}
	}
	return plan, nil
}

func (plan *uninstallPlan) withhold(surface uninstallSurface, result surfaceRemoval) {
	if result.removed != "" || result.left.remains() {
		plan.notes = append(plan.notes, "note: "+surface.path+" may run review-party for other repositories on this machine; remove it with --shared")
	}
}

func (surface uninstallSurface) skeleton(result surfaceRemoval) bool {
	return surface.created != nil && !result.left.remains() && bytes.Equal(result.residue, surface.remove(surface.created).residue)
}

// survey deletes a recorded file holding only install's skeleton even when its
// block went earlier, so a rerun finishes what an interrupted one started.
func (plan *uninstallPlan) survey(surface uninstallSurface, result surfaceRemoval) {
	skeleton := surface.skeleton(result)
	created, recorded := plan.record.created(recordedPath(surface.path))
	switch {
	case skeleton && recorded:
		plan.deletes = append(plan.deletes, created)
		surface.path = created.Path
		plan.add(surface, removedFile, nil)
	case result.removed != "":
		plan.writes[surface.path] = result.residue
		plan.add(surface, result.removed, nil)
		if skeleton {
			plan.notes = append(plan.notes, "note: "+surface.path+" will hold only what install puts in a new file; delete it if nothing else uses it")
		}
	}
	if result.left.remains() {
		plan.add(surface, result.left, result.manual)
	} else {
		plan.record.forget(recordedPath(surface.path))
	}
}

func (plan *uninstallPlan) add(surface uninstallSurface, outcome removalOutcome, manual []string) {
	plan.steps = append(plan.steps, uninstallStep{integration: surface.integration, checkpoint: surface.checkpoint, path: surface.path, outcome: outcome, manual: manual})
}

func (plan uninstallPlan) changes() bool {
	return len(plan.writes) > 0 || len(plan.deletes) > 0
}

func (plan uninstallPlan) remaining() bool {
	return slices.ContainsFunc(plan.steps, func(step uninstallStep) bool { return step.outcome.remains() })
}

func (plan uninstallPlan) run(output io.Writer, confirm func() (bool, error)) error {
	if err := writeCommandOutput(output, plan.render); err != nil {
		return err
	}
	if plan.changes() {
		if confirmed, err := confirm(); err != nil || !confirmed {
			return err
		}
	}
	if err := plan.apply(); err != nil {
		return err
	}
	if err := writeCommandOutput(output, plan.summary); err != nil {
		return err
	}
	if plan.remaining() {
		return errUninstallIncomplete
	}
	return nil
}

func (plan uninstallPlan) render(output *commandOutput) {
	if len(plan.steps) == 0 {
		output.write("Nothing to remove.\n")
	}
	for _, step := range plan.steps {
		output.write("%s: %s %s\n", strings.TrimSpace(string(step.integration)+" "+string(step.checkpoint)), step.outcome, step.path)
		for _, line := range step.manual {
			output.write("  %s\n", line)
		}
	}
	for _, note := range plan.notes {
		output.write("%s\n", note)
	}
}

func (plan uninstallPlan) summary(output *commandOutput) {
	if plan.changes() {
		output.write("Updated %d file(s); deleted %d file(s).\n", len(plan.writes), len(plan.deletes))
	}
}

func (plan uninstallPlan) apply() error {
	for _, path := range slices.Sorted(maps.Keys(plan.writes)) {
		info, err := os.Stat(path)
		if err == nil {
			err = replaceFile(path, plan.writes[path], info.Mode().Perm())
		}
		if err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	if err := removeCreated(plan.deletes); err != nil {
		return err
	}
	return plan.record.save()
}
