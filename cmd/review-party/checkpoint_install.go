package main

// Checkpoint Integration installers wire `review-party checkpoint hook` into
// git's hook tool or a Caller Agent's hook settings. They only add: existing
// lines and entries are never altered, an installed entry is a no-op, and one
// someone edited is reported and left alone. Configuration the installer
// cannot extend safely gets a snippet to add by hand.

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"reviewparty/internal/configuration"
	"reviewparty/internal/subject"
)

func newCheckpointInstallCommand(streams commandIO) *cobra.Command {
	install := &cobra.Command{Use: "install", Short: "Install Checkpoint Integrations", Args: cobra.NoArgs, RunE: showCommandHelp}
	git := &cobra.Command{
		Use:   "git",
		Short: "Install git hooks that decide every declared Checkpoint",
		Long: `Install git hooks that decide every declared Checkpoint, in the hook tool
the repository already uses. Detection order: lefthook, husky, the pre-commit
framework, core.hooksPath, then plain .git/hooks.

For plain hooks, core.hooksPath, and husky, a marked block is added after the
shebang; existing lines are never changed and a missing hook file is created.
lefthook, the pre-commit framework, and hooks the block cannot run in get a
snippet to add by hand. Installed hooks load each Caller's default Global
Configuration. Rerunning is safe.`,
		Example: "  review-party checkpoint install git\n  review-party checkpoint install git --yes",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options := hookInstallOptions{repository: stringFlag(cmd, "repo"), configuration: stringFlag(cmd, "config"), yes: boolFlag(cmd, "yes"), integration: configuration.IntegrationGit}
			return commandResult(executeCheckpointInstall(options, streams))
		},
	}
	addRepositoryFlag(git, "Git repository to install hooks into")
	addConfigurationFlag(git)
	git.Flags().Bool("yes", false, "Write the hooks without a confirmation prompt")
	install.AddCommand(git)
	for _, agent := range agentIntegrations {
		install.AddCommand(newAgentInstallCommand(agent, streams))
	}
	return install
}

type hookInstallOptions struct {
	repository    string
	configuration string
	yes           bool
	integration   configuration.IntegrationName
	personal      bool
}

var (
	errNoCheckpointDeclared   = errors.New("no Checkpoint is declared; declare one with review-party config checkpoint set pre-push")
	errHookInstallUnconfirmed = errors.New("rerun with --yes to write the hooks without a terminal")
	errHooksNotWritten        = errors.New("hooks not written")
)

func executeCheckpointInstall(options hookInstallOptions, streams commandIO) int {
	root, err := subject.ResolveRepositoryRoot(options.repository)
	if err != nil {
		return printCommandError(streams.errors, usageExitCode, err)
	}
	confirm := func() (bool, error) {
		switch {
		case options.yes:
			return true, nil
		case !streams.interactive():
			return false, errHookInstallUnconfirmed
		}
		confirmed, err := confirmPrompt(streams.input, streams.output, "Write these hooks?")
		if err == nil && !confirmed {
			err = errHooksNotWritten
		}
		return confirmed, err
	}
	target := hookInstallTarget{root: root, configuration: options.configuration, integration: options.integration, personal: options.personal}
	err = installCheckpointHooks(target, streams.configurationManager(options.configuration), streams.output, confirm)
	switch {
	case errors.Is(err, errNoCheckpointDeclared), errors.Is(err, errHookInstallUnconfirmed):
		return printCommandError(streams.errors, usageExitCode, err)
	case err != nil:
		return printFailure(streams.errors, err)
	}
	return 0
}

// installCheckpointHooks prints what installing one Integration for every
// declared Checkpoint does, and writes only when confirm agrees. The init
// journey shares it with its own confirmation form.
func installCheckpointHooks(target hookInstallTarget, manager *configuration.Manager, output io.Writer, confirm func() (bool, error)) error {
	plan, err := planIntegrationInstall(target, manager)
	if err != nil {
		return err
	}
	if len(plan.steps) == 0 {
		return errNoCheckpointDeclared
	}
	if err := writeCommandOutput(output, plan.render); err != nil {
		return err
	}
	if proceed, err := plan.applyConfirmed(confirm); err != nil || !proceed {
		return err
	}
	return writeCommandOutput(output, func(output *commandOutput) {
		if len(plan.writes) > 0 {
			output.write("Wrote %d hook file(s).\n", len(plan.writes))
		}
		for _, line := range plan.followUp {
			output.write("%s\n", line)
		}
	})
}

func planIntegrationInstall(target hookInstallTarget, manager *configuration.Manager) (hookInstallPlan, error) {
	if target.integration == configuration.IntegrationGit {
		return planHookInstall(target, manager)
	}
	for _, agent := range agentIntegrations {
		if agent.integration == target.integration {
			return planAgentHookInstall(target, manager, agent)
		}
	}
	return hookInstallPlan{}, fmt.Errorf("no installer for the %s Integration", target.integration)
}

// hookTool is the hook manager a repository uses, in detection order.
type hookTool string

const (
	hookToolLefthook  hookTool = "lefthook"
	hookToolHusky     hookTool = "husky"
	hookToolPreCommit hookTool = "pre-commit framework"
	hookToolHooksPath hookTool = "core.hooksPath"
	hookToolPlain     hookTool = "git hooks"
	hookToolClaude    hookTool = "Claude Code"
	hookToolCodex     hookTool = "Codex"
)

// hookOutcome is what the installer does for one Checkpoint.
type hookOutcome string

const (
	hookCreated   hookOutcome = "create"
	hookInserted  hookOutcome = "add the review-party block to"
	hookInstalled hookOutcome = "already installed in"
	hookEdited    hookOutcome = "edited review-party block left unchanged in"
	hookManual    hookOutcome = "add by hand to"
	// hookNotExecutable is an installed block in a hook git skips because the
	// file lost its execute bits.
	hookNotExecutable hookOutcome = "make executable"
	// Caller Agent settings files take PreToolUse entries.
	hookEntryAdded  hookOutcome = "add a PreToolUse entry to"
	hookEntryShared hookOutcome = "share the PreToolUse entry added to"
	hookEntryEdited hookOutcome = "edited review-party entry left unchanged in"
)

type hookInstallStep struct {
	checkpoint configuration.CheckpointName
	path       string
	outcome    hookOutcome
	manual     []string
	// activate is the command that makes git run the hook in this clone,
	// empty when it already does.
	activate string
}

// hookInstallTarget is the repository whose hooks are installed, the
// Caller's --config, which installed hooks do not carry, and the Integration
// to install. Personal selects a Caller Agent's personal settings over the
// team file.
type hookInstallTarget struct {
	root          string
	configuration string
	integration   configuration.IntegrationName
	personal      bool
}

type hookInstallPlan struct {
	tool hookTool
	// location is where the tool keeps Checkpoint entries: a configuration
	// file for lefthook and the pre-commit framework, a hook directory
	// otherwise.
	location string
	// hooks is the directory git runs hooks from in this clone.
	hooks  string
	steps  []hookInstallStep
	writes map[string][]byte
	// fileMode is the mode of a file the plan creates. An executable mode
	// also restores the execute bits of an existing hook.
	fileMode fs.FileMode
	// config is the Caller's --config argument when it is not the default.
	// Installed hooks leave it out, so install warns that they load the
	// default.
	config string
	// warnings follow the steps, before confirmation; followUp follows the
	// install, naming what the Caller still has to do.
	warnings []string
	followUp []string
}

// missingBinaryNote names what installed hooks do while review-party is
// not on PATH, or is empty when it is.
func missingBinaryNote(effect string) []string {
	if _, err := exec.LookPath("review-party"); err == nil {
		return nil
	}
	return []string{"warning: review-party is not on PATH; " + effect + " until it is"}
}

func planHookInstall(target hookInstallTarget, manager *configuration.Manager) (hookInstallPlan, error) {
	declared, err := manager.Checkpoints(configuration.Repository(target.root))
	if err != nil {
		return hookInstallPlan{}, err
	}
	locations, err := subject.ResolveHookLocations(target.root)
	if err != nil {
		return hookInstallPlan{}, err
	}
	tool, location := detectHookTool(target.root, locations)
	plan := hookInstallPlan{
		tool: tool, location: location, hooks: locations.Directory, writes: map[string][]byte{}, fileMode: 0o755,
		config: configurationArgument(target.configuration), warnings: missingBinaryNote("the hooks warn and allow"),
	}
	for _, name := range configuration.SortedCheckpointNames(declared) {
		step, err := plan.planCheckpoint(name)
		if err != nil {
			return hookInstallPlan{}, err
		}
		step.activate = plan.activation(name)
		plan.steps = append(plan.steps, step)
	}
	return plan, nil
}

// hookToolMarkers are the files that select a hook tool, in the order they
// are checked. The first present one wins.
var hookToolMarkers = []struct {
	path string
	tool hookTool
}{
	{"lefthook.yml", hookToolLefthook}, {"lefthook.yaml", hookToolLefthook}, {".lefthook.yml", hookToolLefthook},
	{".lefthook.yaml", hookToolLefthook}, {".config/lefthook.yml", hookToolLefthook}, {".config/lefthook.yaml", hookToolLefthook},
	{"lefthook.toml", hookToolLefthook}, {".lefthook.toml", hookToolLefthook}, {".config/lefthook.toml", hookToolLefthook},
	{"lefthook.json", hookToolLefthook}, {".lefthook.json", hookToolLefthook}, {".config/lefthook.json", hookToolLefthook},
	{"lefthook.jsonc", hookToolLefthook}, {".lefthook.jsonc", hookToolLefthook}, {".config/lefthook.jsonc", hookToolLefthook},
	{".husky", hookToolHusky},
	{".pre-commit-config.yaml", hookToolPreCommit},
}

func detectHookTool(root string, locations subject.HookLocations) (hookTool, string) {
	for _, marker := range hookToolMarkers {
		path := filepath.Join(root, marker.path)
		if _, err := os.Stat(path); err == nil {
			return marker.tool, path
		}
	}
	if locations.HooksPath != "" {
		return hookToolHooksPath, locations.Directory
	}
	return hookToolPlain, locations.Directory
}

// activation is the command that makes git run a Checkpoint's hook in this
// clone. lefthook, husky, and the pre-commit framework keep their hooks in
// committed files, but each clone has to install the tool before git runs
// them.
func (plan *hookInstallPlan) activation(name configuration.CheckpointName) string {
	switch {
	case plan.tool == hookToolLefthook && !plan.generatedHook(name, "call_lefthook"):
		return "lefthook install"
	case plan.tool == hookToolHusky && !plan.huskyActive(name):
		return "npx husky"
	case plan.tool == hookToolPreCommit && !plan.generatedHook(name, "# File generated by pre-commit: https://pre-commit.com"):
		return "pre-commit install --hook-type " + string(name)
	}
	return ""
}

// huskyActive reports whether git runs the Checkpoint's hook from .husky,
// where husky 5 to 8 point core.hooksPath, or through the wrapper husky 9
// generates for it in .husky/_.
func (plan *hookInstallPlan) huskyActive(name configuration.CheckpointName) bool {
	husky, hooks := resolvedPath(plan.location), resolvedPath(plan.hooks)
	if hooks == husky {
		return true
	}
	wrapper, err := os.Stat(filepath.Join(plan.hooks, string(name)))
	return hooks == filepath.Join(husky, "_") && err == nil && wrapper.Mode().Perm()&0o100 != 0
}

// generatedHook reports whether git runs the Checkpoint's hook from the file a
// hook tool's install command generates, which carries marker.
func (plan *hookInstallPlan) generatedHook(name configuration.CheckpointName, marker string) bool {
	path := filepath.Join(plan.hooks, string(name))
	content, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(content), marker) && hookInstallStep{path: path}.executable()
}

// resolvedPath resolves symlinks in the longest existing prefix of path, so a
// hooks directory that does not exist yet resolves through its parents.
func resolvedPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(resolvedPath(parent), filepath.Base(path))
}

func (plan *hookInstallPlan) planCheckpoint(name configuration.CheckpointName) (hookInstallStep, error) {
	switch plan.tool {
	case hookToolLefthook:
		return plan.planManagerSnippet(name, strings.Split(strings.TrimSuffix(lefthookCommand(name), "\n"), "\n"))
	case hookToolPreCommit:
		return plan.planManagerSnippet(name, preCommitFrameworkSnippet(name))
	case hookToolHusky, hookToolHooksPath, hookToolPlain:
		return plan.planHookScript(name)
	case hookToolClaude, hookToolCodex:
	}
	return hookInstallStep{}, fmt.Errorf("unknown hook tool %q", plan.tool)
}

// pending is the content a path will have once earlier steps are applied.
func (plan *hookInstallPlan) pending(path string) ([]byte, bool, error) {
	if content, ok := plan.writes[path]; ok {
		return content, true, nil
	}
	content, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	return content, true, nil
}

// hookScript is the hook file git runs, or a hook tool sources, for a
// Checkpoint.
func (plan *hookInstallPlan) hookScript(name configuration.CheckpointName) string {
	return filepath.Join(plan.location, string(name))
}

func (plan *hookInstallPlan) planHookScript(name configuration.CheckpointName) (hookInstallStep, error) {
	path := plan.hookScript(name)
	content, found, err := plan.pending(path)
	if err != nil {
		return hookInstallStep{}, err
	}
	step := hookInstallStep{checkpoint: name, path: path}
	block := checkpointHookBlock(name)
	if !found {
		step.outcome = hookCreated
		plan.writes[path] = []byte("#!/bin/sh\n" + block)
		return step, nil
	}
	updated, outcome := insertHookBlock(string(content), name, block)
	step.outcome = outcome
	switch outcome {
	case hookInserted:
		plan.writes[path] = []byte(updated)
	case hookInstalled, hookEdited:
		if !step.executable() {
			step.outcome = hookNotExecutable
			plan.writes[path] = content
		}
	case hookManual:
		step.manual = hookScriptCall(name)
	case hookCreated, hookNotExecutable, hookEntryAdded, hookEntryShared, hookEntryEdited:
	}
	return step, nil
}

// hookScriptCall is what to add by hand to a hook the shell block cannot
// run in.
func hookScriptCall(name configuration.CheckpointName) []string {
	command := "review-party checkpoint hook git " + string(name)
	if name == configuration.CheckpointPrePush {
		command += " -- <remote> <url>"
	}
	return []string{
		"# This hook is not a shell script. Run the command below from it with the",
		"# hook's arguments in place of any <placeholders>, and its standard input.",
		"# Stop only when it exits 1; any other status means the Checkpoint was not",
		"# checked, so warn and continue.",
		command,
	}
}

// executable reports whether git can run the step's hook. A hook it cannot
// stat counts as executable, so install leaves it to git to report.
func (step hookInstallStep) executable() bool {
	info, err := os.Stat(step.path)
	return err != nil || info.Mode().Perm()&0o100 != 0
}

func hookBlockMarkers(name configuration.CheckpointName) (string, string) {
	return "# >>> review-party checkpoint " + string(name) + " >>>", "# <<< review-party checkpoint " + string(name) + " <<<"
}

// checkpointHookBlock is the shell the installer adds to a hook script. The
// pre-push block reads the refs git sends, hands them to review-party, and
// feeds them back as standard input so the rest of the hook still reads them.
func checkpointHookBlock(name configuration.CheckpointName) string {
	start, end := hookBlockMarkers(name)
	missing := "  " + missingBinaryWarning(name) + "\n"
	if name == configuration.CheckpointPreCommit {
		return start + "\n" +
			"if command -v review-party >/dev/null 2>&1; then\n" +
			"  " + strings.Join(hookCommand{name, "review-party checkpoint hook git pre-commit"}.checked(), "\n  ") + "\n" +
			"else\n" + missing + "fi\n" + end + "\n"
	}
	return start + "\n" +
		"review_party_refs=$(cat)\n" +
		"if command -v review-party >/dev/null 2>&1; then\n" +
		"  " + strings.Join(hookCommand{name, "printf '%s\\n' \"$review_party_refs\" | review-party checkpoint hook git pre-push -- \"$@\""}.checked(), "\n  ") + "\n" +
		"else\n" + missing + "fi\n" +
		"if [ -n \"$review_party_refs\" ]; then\n" +
		"exec 0<<REVIEW_PARTY_REFS\n" +
		"$review_party_refs\n" +
		"REVIEW_PARTY_REFS\n" +
		"else\n" +
		"exec 0</dev/null\n" +
		"fi\n" + end + "\n"
}

// insertHookBlock adds the block after the shebang, or at the top of a hook
// without one. A present block that differs is the Caller's edit. A hook for
// another interpreter, or a compiled one, which holds a NUL byte, cannot run
// the shell block, so it is left for the Caller to edit by hand.
func insertHookBlock(content string, name configuration.CheckpointName, block string) (string, hookOutcome) {
	start, end := hookBlockMarkers(name)
	if index := strings.Index(content, start+"\n"); index >= 0 {
		present, _, _ := strings.Cut(content[index:], end+"\n")
		if present+end+"\n" == block {
			return content, hookInstalled
		}
		return content, hookEdited
	}
	if strings.ContainsRune(content, 0) {
		return content, hookManual
	}
	if !strings.HasPrefix(content, "#!") {
		return block + content, hookInserted
	}
	shebang, rest, found := strings.Cut(content, "\n")
	if !runsInPosixShell(strings.Fields(shebang[2:])) {
		return content, hookManual
	}
	if !found {
		return shebang + "\n" + block, hookInserted
	}
	return shebang + "\n" + block + rest, hookInserted
}

var posixShells = []string{"sh", "bash", "dash", "zsh", "ksh", "ash", "mksh"}

// runsInPosixShell reports whether a shebang's fields name a POSIX shell,
// directly or through env.
func runsInPosixShell(shebang []string) bool {
	if len(shebang) > 1 && filepath.Base(shebang[0]) == "env" {
		shebang = slices.DeleteFunc(shebang[1:], func(field string) bool { return strings.HasPrefix(field, "-") })
	}
	return len(shebang) > 0 && slices.Contains(posixShells, filepath.Base(shebang[0]))
}

// lefthookCommand is the lefthook entry for one Checkpoint. Keys follow
// https://lefthook.dev/configuration/: run executes through sh with {1} and
// {2} as the hook's arguments (https://lefthook.dev/configuration/run/), and
// use_stdin passes git's ref lines through (https://lefthook.dev/configuration/use_stdin/).
// lefthook pastes {1}, the remote, into the shell text unquoted, so the
// pre-push run reads it from a quoted heredoc, where the shell takes it
// literally. The : before it keeps a remote named like the delimiter from
// ending the heredoc early.
func lefthookCommand(name configuration.CheckpointName) string {
	if name == configuration.CheckpointPreCommit {
		run := hookCommand{name, "review-party checkpoint hook git pre-commit"}.guarded()
		return "pre-commit:\n  commands:\n    review-party-checkpoint:\n      run: " + yamlSingleQuoted(run) + "\n"
	}
	run := []string{
		"review_party_remote=$(cat <<'REVIEW_PARTY_REMOTE'", ":{1}", "REVIEW_PARTY_REMOTE", ")",
		hookCommand{name, `review-party checkpoint hook git pre-push -- "${review_party_remote#:}"`}.guarded(),
	}
	return "pre-push:\n  commands:\n    review-party-checkpoint:\n      run: |\n        " + strings.Join(run, "\n        ") + "\n      use_stdin: true\n"
}

// hookCommand is the review-party call a hook makes for one Checkpoint.
type hookCommand struct {
	checkpoint configuration.CheckpointName
	run        string
}

// guarded runs the command only when review-party is on PATH and otherwise
// warns, so a missing binary never blocks the hook.
func (command hookCommand) guarded() string {
	return "if command -v review-party >/dev/null 2>&1; then " + strings.Join(command.checked(), "; ") + "; else " + missingBinaryWarning(command.checkpoint) + "; fi"
}

// checked stops the hook only on exit 1, a reached refusal. Any other status,
// such as a usage error from a review-party that predates the command, warns
// and lets git continue. The || keeps it safe under set -e.
func (command hookCommand) checked() []string {
	return []string{
		"review_party_status=0",
		command.run + " || review_party_status=$?",
		`if [ "$review_party_status" -eq 1 ]; then exit 1; fi`,
		`if [ "$review_party_status" -ne 0 ]; then echo "review-party: warning: review-party exited $review_party_status; ` + string(command.checkpoint) + ` Checkpoint not checked" >&2; fi`,
	}
}

func missingBinaryWarning(name configuration.CheckpointName) string {
	return `echo "review-party: warning: review-party is not on PATH; ` + string(name) + ` Checkpoint not checked" >&2`
}

func yamlSingleQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// planManagerSnippet counts a Checkpoint as installed in a hook tool's
// configuration when a line that is not a comment calls its hook, and
// otherwise prints snippet to add by hand. The installer never edits these
// files.
func (plan *hookInstallPlan) planManagerSnippet(name configuration.CheckpointName, snippet []string) (hookInstallStep, error) {
	content, err := os.ReadFile(plan.location)
	if err != nil {
		return hookInstallStep{}, fmt.Errorf("read %s: %w", plan.location, err)
	}
	call := "review-party checkpoint hook git " + string(name)
	for line := range strings.Lines(string(content)) {
		if strings.Contains(line, call) && !strings.HasPrefix(strings.TrimSpace(line), "#") {
			return hookInstallStep{checkpoint: name, path: plan.location, outcome: hookInstalled}, nil
		}
	}
	return hookInstallStep{checkpoint: name, path: plan.location, outcome: hookManual, manual: snippet}, nil
}

// preCommitFrameworkEntry is the entry key of the local hook for
// .pre-commit-config.yaml. The framework sets PRE_COMMIT_* variables for
// pre-push hooks instead of passing git's ref lines (https://pre-commit.com/),
// so the entry rebuilds one line.
func preCommitFrameworkEntry(name configuration.CheckpointName) string {
	command, arguments := "review-party checkpoint hook git pre-commit", ""
	if name == configuration.CheckpointPrePush {
		command = `printf "%s %s %s %s\n" "$PRE_COMMIT_LOCAL_BRANCH" "$PRE_COMMIT_TO_REF" "$PRE_COMMIT_REMOTE_BRANCH" "$PRE_COMMIT_FROM_REF" | review-party checkpoint hook git pre-push`
		arguments = ` -- "$PRE_COMMIT_REMOTE_NAME"`
	}
	return "entry: " + yamlSingleQuoted("sh -c "+shellQuoteArgument(hookCommand{name, command + arguments}.guarded()))
}

func preCommitFrameworkSnippet(name configuration.CheckpointName) []string {
	return []string{
		"- repo: local",
		"  hooks:",
		"    - id: review-party-checkpoint-" + string(name),
		"      name: review-party " + string(name) + " Checkpoint",
		"      " + preCommitFrameworkEntry(name),
		"      language: system",
		"      pass_filenames: false",
		"      always_run: true",
		"      stages: [" + string(name) + "]",
	}
}

func (plan hookInstallPlan) render(output *commandOutput) {
	for _, step := range plan.steps {
		output.write("%s: %s %s (%s)\n", step.checkpoint, step.outcome, step.path, plan.tool)
		for _, line := range step.manual {
			output.write("  %s\n", line)
		}
		if step.activate != "" {
			output.write("  git does not run it in this clone until you run: %s\n", step.activate)
		}
	}
	for _, warning := range plan.warnings {
		output.write("%s\n", warning)
	}
	if plan.config != "" {
		output.write("warning: the hooks load each Caller's default Global Configuration, not%s\n", plan.config)
	}
}

// applyConfirmed writes the plan's files when it has any and confirm agrees.
// It reports false when the Caller declined.
func (plan hookInstallPlan) applyConfirmed(confirm func() (bool, error)) (bool, error) {
	if len(plan.writes) == 0 {
		return true, nil
	}
	confirmed, err := confirm()
	if err != nil || !confirmed {
		return false, err
	}
	return true, plan.apply()
}

func (plan hookInstallPlan) apply() error {
	for path, content := range plan.writes {
		mode := plan.fileMode
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
		if plan.fileMode&0o111 != 0 {
			mode |= 0o111
		}
		if err := replaceFile(path, content, mode); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// replaceFile writes content beside path and renames it over path, so a
// failed write leaves the existing hook intact. A symlinked hook is written
// at its target, even one that does not exist yet, so the link survives.
func replaceFile(path string, content []byte, mode fs.FileMode) (returnErr error) {
	path, err := linkTarget(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, os.Remove(temporary.Name()))
		}
	}()
	_, writeErr := temporary.Write(content)
	if err := errors.Join(writeErr, temporary.Chmod(mode), temporary.Close()); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

// linkTarget follows a chain of symlinks from path to the file it names,
// whether or not that file exists.
func linkTarget(path string) (string, error) {
	for range 40 {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&fs.ModeSymlink == 0 {
			return path, nil
		}
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		path = target
	}
	return "", fmt.Errorf("%s: too many levels of symbolic links", path)
}
