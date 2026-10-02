package main

// The git Checkpoint Integration installer wires `review-party checkpoint hook
// git` into whichever hook tool the repository already uses. It only adds:
// existing hook lines are never altered, an installed block is a no-op, and a
// block someone edited is reported and left alone. Tools whose configuration
// it cannot extend safely get a snippet to add by hand.

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
lefthook YAML gains a command when the hook has no entry yet. Anything else
prints the snippet to add by hand. Rerunning is safe.`,
		Example: "  review-party checkpoint install git\n  review-party checkpoint install git --yes",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options := hookInstallOptions{repository: stringFlag(cmd, "repo"), configuration: stringFlag(cmd, "config"), yes: boolFlag(cmd, "yes")}
			return commandResult(executeCheckpointInstall(options, streams))
		},
	}
	addRepositoryFlag(git, "Git repository to install hooks into")
	addConfigurationFlag(git)
	git.Flags().Bool("yes", false, "Write the hooks without a confirmation prompt")
	install.AddCommand(git)
	return install
}

type hookInstallOptions struct {
	repository    string
	configuration string
	yes           bool
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
	target := hookInstallTarget{root: root, config: configurationArgument(options.configuration)}
	err = installCheckpointHooks(target, streams.configurationManager(options.configuration), streams.output, confirm)
	switch {
	case errors.Is(err, errNoCheckpointDeclared), errors.Is(err, errHookInstallUnconfirmed):
		return printCommandError(streams.errors, usageExitCode, err)
	case err != nil:
		return printFailure(streams.errors, err)
	}
	return 0
}

// installCheckpointHooks prints what installing the git Integration for every
// declared Checkpoint does, and writes only when confirm agrees. The init
// journey shares it with its own confirmation form.
func installCheckpointHooks(target hookInstallTarget, manager *configuration.Manager, output io.Writer, confirm func() (bool, error)) error {
	plan, err := planHookInstall(target, manager)
	if err != nil {
		return err
	}
	if len(plan.steps) == 0 {
		return errNoCheckpointDeclared
	}
	if err := writeCommandOutput(output, plan.render); err != nil || len(plan.writes) == 0 {
		return err
	}
	confirmed, err := confirm()
	if err != nil || !confirmed {
		return err
	}
	if err := plan.apply(); err != nil {
		return err
	}
	return writeCommandOutput(output, func(output *commandOutput) {
		output.write("Wrote %d hook file(s).\n", len(plan.writes))
	})
}

// hookTool is the hook manager a repository uses, in detection order.
type hookTool string

const (
	hookToolLefthook  hookTool = "lefthook"
	hookToolHusky     hookTool = "husky"
	hookToolPreCommit hookTool = "pre-commit framework"
	hookToolHooksPath hookTool = "core.hooksPath"
	hookToolPlain     hookTool = "git hooks"
)

// hookOutcome is what the installer does for one Checkpoint.
type hookOutcome string

const (
	hookCreated   hookOutcome = "create"
	hookInserted  hookOutcome = "add the review-party block to"
	hookAppended  hookOutcome = "add a review-party-checkpoint command to"
	hookInstalled hookOutcome = "already installed in"
	hookEdited    hookOutcome = "edited review-party block left unchanged in"
	hookManual    hookOutcome = "add by hand to"
)

type hookInstallStep struct {
	checkpoint configuration.CheckpointName
	path       string
	outcome    hookOutcome
	manual     []string
}

// hookInstallTarget is the repository whose hooks are installed and the
// --config argument its hooks need to load the Caller's configuration.
type hookInstallTarget struct {
	root   string
	config string
}

type hookInstallPlan struct {
	tool hookTool
	// location is where the tool keeps Checkpoint entries: a configuration
	// file for lefthook and the pre-commit framework, a hook directory
	// otherwise.
	location string
	steps    []hookInstallStep
	writes   map[string][]byte
	// missingBinary reports that review-party is not on PATH, so installed
	// hooks will warn and allow until it is.
	missingBinary bool
	// config is the --config argument hooks in this clone carry. committed
	// hooks are shared with the team, so they never carry one.
	config    string
	committed bool
}

func planHookInstall(target hookInstallTarget, manager *configuration.Manager) (hookInstallPlan, error) {
	declared, err := manager.Checkpoints(configuration.Repository(target.root))
	if err != nil {
		return hookInstallPlan{}, err
	}
	tool, location, err := detectHookTool(target.root)
	if err != nil {
		return hookInstallPlan{}, err
	}
	_, lookErr := exec.LookPath("review-party")
	plan := hookInstallPlan{
		tool: tool, location: location, writes: map[string][]byte{}, missingBinary: lookErr != nil,
		config: target.config, committed: target.committed(tool, location),
	}
	for _, name := range configuration.SortedCheckpointNames(declared) {
		step, err := plan.planCheckpoint(name)
		if err != nil {
			return hookInstallPlan{}, err
		}
		plan.steps = append(plan.steps, step)
	}
	return plan, nil
}

var lefthookConfigFiles = []string{
	"lefthook.yml", "lefthook.yaml", ".lefthook.yml", ".lefthook.yaml", ".config/lefthook.yml", ".config/lefthook.yaml",
	"lefthook.toml", ".lefthook.toml", ".config/lefthook.toml",
	"lefthook.json", ".lefthook.json", ".config/lefthook.json", "lefthook.jsonc", ".lefthook.jsonc", ".config/lefthook.jsonc",
}

func detectHookTool(root string) (hookTool, string, error) {
	for _, name := range lefthookConfigFiles {
		if exists(filepath.Join(root, name)) {
			return hookToolLefthook, filepath.Join(root, name), nil
		}
	}
	if exists(filepath.Join(root, ".husky")) {
		return hookToolHusky, filepath.Join(root, ".husky"), nil
	}
	if exists(filepath.Join(root, ".pre-commit-config.yaml")) {
		return hookToolPreCommit, filepath.Join(root, ".pre-commit-config.yaml"), nil
	}
	locations, err := subject.ResolveHookLocations(root)
	if err != nil {
		return "", "", err
	}
	if locations.HooksPath != "" {
		return hookToolHooksPath, locations.Directory, nil
	}
	return hookToolPlain, locations.Directory, nil
}

// committed reports whether the hooks live in files the team shares: a hook
// tool's configuration, or a core.hooksPath inside the work tree.
func (target hookInstallTarget) committed(tool hookTool, location string) bool {
	if tool == hookToolPlain {
		return false
	}
	relative, err := filepath.Rel(target.root, location)
	return tool != hookToolHooksPath || err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (plan *hookInstallPlan) planCheckpoint(name configuration.CheckpointName) (hookInstallStep, error) {
	switch plan.tool {
	case hookToolLefthook:
		return plan.planLefthook(name)
	case hookToolPreCommit:
		return hookInstallStep{checkpoint: name, path: plan.location, outcome: hookManual, manual: preCommitFrameworkSnippet(name)}, nil
	case hookToolHusky, hookToolHooksPath, hookToolPlain:
		return plan.planHookScript(name)
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

func (plan *hookInstallPlan) planHookScript(name configuration.CheckpointName) (hookInstallStep, error) {
	path := filepath.Join(plan.location, string(name))
	content, found, err := plan.pending(path)
	if err != nil {
		return hookInstallStep{}, err
	}
	step := hookInstallStep{checkpoint: name, path: path}
	config := plan.config
	if plan.committed {
		config = ""
	}
	block := checkpointHookBlock(name, config)
	if !found {
		step.outcome = hookCreated
		plan.writes[path] = []byte("#!/bin/sh\n" + block)
		return step, nil
	}
	updated, outcome := insertHookBlock(string(content), block)
	step.outcome = outcome
	if outcome == hookInserted {
		plan.writes[path] = []byte(updated)
	}
	return step, nil
}

func hookBlockMarkers(name configuration.CheckpointName) (string, string) {
	return "# >>> review-party checkpoint " + string(name) + " >>>", "# <<< review-party checkpoint " + string(name) + " <<<"
}

// checkpointHookBlock is the shell the installer adds to a hook script. The
// pre-push block reads the refs git sends, hands them to review-party, and
// feeds them back as standard input so the rest of the hook still reads them.
func checkpointHookBlock(name configuration.CheckpointName, config string) string {
	start, end := hookBlockMarkers(name)
	missing := "  " + missingBinaryWarning(name) + "\n"
	if name == configuration.CheckpointPreCommit {
		return start + "\n" +
			"if command -v review-party >/dev/null 2>&1; then\n" +
			"  review-party checkpoint hook git pre-commit" + config + " || exit $?\n" +
			"else\n" + missing + "fi\n" + end + "\n"
	}
	return start + "\n" +
		"review_party_refs=$(cat)\n" +
		"if command -v review-party >/dev/null 2>&1; then\n" +
		"  printf '%s\\n' \"$review_party_refs\" | review-party checkpoint hook git pre-push" + config + " \"$@\" || exit $?\n" +
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
// without one. A present block is compared, never rewritten.
func insertHookBlock(content, block string) (string, hookOutcome) {
	start := block[:strings.Index(block, "\n")+1]
	if index := strings.Index(content, start); index >= 0 {
		if strings.HasPrefix(content[index:], block) {
			return content, hookInstalled
		}
		return content, hookEdited
	}
	if !strings.HasPrefix(content, "#!") {
		return block + content, hookInserted
	}
	shebang, rest, found := strings.Cut(content, "\n")
	if !found {
		return shebang + "\n" + block, hookInserted
	}
	return shebang + "\n" + block + rest, hookInserted
}

// lefthookCommand is the lefthook entry for one Checkpoint. Keys follow
// https://lefthook.dev/configuration/: run executes through sh with {1} and
// {2} as the hook's arguments (https://lefthook.dev/configuration/run/), and
// use_stdin passes git's ref lines through (https://lefthook.dev/configuration/use_stdin/).
func lefthookCommand(name configuration.CheckpointName) string {
	if name == configuration.CheckpointPreCommit {
		run := guardedHookCommand(name, "review-party checkpoint hook git pre-commit")
		return "pre-commit:\n  commands:\n    review-party-checkpoint:\n      run: " + yamlSingleQuoted(run) + "\n"
	}
	run := guardedHookCommand(name, "review-party checkpoint hook git pre-push {1} {2}")
	return "pre-push:\n  commands:\n    review-party-checkpoint:\n      run: " + yamlSingleQuoted(run) + "\n      use_stdin: true\n"
}

// guardedHookCommand runs command only when review-party is on PATH and
// otherwise warns, so a missing binary never blocks the hook.
func guardedHookCommand(name configuration.CheckpointName, command string) string {
	return "if command -v review-party >/dev/null 2>&1; then " + command + "; else " + missingBinaryWarning(name) + "; fi"
}

func missingBinaryWarning(name configuration.CheckpointName) string {
	return `echo "review-party: warning: review-party is not on PATH; ` + string(name) + ` Checkpoint not checked" >&2`
}

func yamlSingleQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func (plan *hookInstallPlan) planLefthook(name configuration.CheckpointName) (hookInstallStep, error) {
	path := plan.location
	step := hookInstallStep{checkpoint: name, path: path}
	command := lefthookCommand(name)
	content, _, err := plan.pending(path)
	if err != nil {
		return step, err
	}
	text := string(content)
	switch {
	case strings.Contains(text, command):
		step.outcome = hookInstalled
	case !slices.Contains([]string{".yml", ".yaml"}, filepath.Ext(path)) || hasTopLevelKey(text, name):
		step.outcome, step.manual = hookManual, strings.Split(strings.TrimSuffix(command, "\n"), "\n")
	default:
		step.outcome = hookAppended
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		plan.writes[path] = []byte(text + "\n" + command)
	}
	return step, nil
}

func hasTopLevelKey(yaml string, name configuration.CheckpointName) bool {
	return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(string(name)) + `\s*:`).MatchString(yaml)
}

// preCommitFrameworkSnippet is a local hook for .pre-commit-config.yaml. The
// framework sets PRE_COMMIT_* variables for pre-push hooks instead of passing
// git's ref lines (https://pre-commit.com/), so the entry rebuilds one line.
func preCommitFrameworkSnippet(name configuration.CheckpointName) []string {
	command := "review-party checkpoint hook git pre-commit"
	if name == configuration.CheckpointPrePush {
		command = `printf "%s %s %s %s\n" "$PRE_COMMIT_LOCAL_BRANCH" "$PRE_COMMIT_TO_REF" "$PRE_COMMIT_REMOTE_BRANCH" "$PRE_COMMIT_FROM_REF" | review-party checkpoint hook git pre-push "$PRE_COMMIT_REMOTE_NAME"`
	}
	entry := yamlSingleQuoted("sh -c '" + guardedHookCommand(name, command) + "'")
	return []string{
		"- repo: local",
		"  hooks:",
		"    - id: review-party-checkpoint-" + string(name),
		"      name: review-party " + string(name) + " Checkpoint",
		"      entry: " + entry,
		"      language: system",
		"      pass_filenames: false",
		"      always_run: true",
		"      stages: [" + string(name) + "]",
		"then run: pre-commit install --hook-type " + string(name),
	}
}

func (plan hookInstallPlan) render(output *commandOutput) {
	for _, step := range plan.steps {
		output.write("%s: %s %s (%s)\n", step.checkpoint, step.outcome, step.path, plan.tool)
		for _, line := range step.manual {
			output.write("  %s\n", line)
		}
	}
	if plan.missingBinary {
		output.write("warning: review-party is not on PATH; the hooks warn and allow until it is\n")
	}
	if plan.committed && plan.config != "" {
		output.write("warning: %s hooks are shared with the team, so they load each Caller's default configuration, not%s\n", plan.tool, plan.config)
	}
}

func (plan hookInstallPlan) apply() error {
	for path, content := range plan.writes {
		mode := fs.FileMode(0o755)
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
		if plan.tool != hookToolLefthook {
			mode |= 0o111
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, content, mode); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		if err := os.Chmod(path, mode); err != nil {
			return fmt.Errorf("make %s executable: %w", path, err)
		}
	}
	return nil
}
