package main

// A Caller Agent hook sees a shell command before the shell runs it. This file
// finds the git push and git commit commands in it and says what each would
// send, without touching configuration, the ledger, or git, because Codex runs
// the hook on every shell command.

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"reviewparty/internal/configuration"
	"reviewparty/internal/subject"
)

// agentCommandForm is what a git push or commit sends, as far as the hook can
// know it before git runs.
type agentCommandForm int

const (
	formPush agentCommandForm = iota
	// formCommitStaged commits the index, which pre-commit checks.
	formCommitStaged
	// formCommitTracked commits every tracked change, as git commit -a does.
	formCommitTracked
	// formCommitPaths commits paths or hunks the command picks, content the
	// hook cannot see.
	formCommitPaths
	// formUndecidable names refs or a repository the hook cannot resolve.
	formUndecidable
)

// agentGitCommand is one git push or git commit found in a shell command.
type agentGitCommand struct {
	checkpoint configuration.CheckpointName
	// directory is where git runs, relative to the hook's cwd unless absolute.
	directory string
	// preceded is true when a command other than a cd runs before this one in
	// the same shell command, so the content at hook time is not what git will
	// see.
	preceded bool
	form     agentCommandForm
	// reason says why a formUndecidable command cannot be checked.
	reason string
	push   subject.PushCommand
}

func (command agentGitCommand) verb() string {
	if command.checkpoint == configuration.CheckpointPreCommit {
		return "commit"
	}
	return "push"
}

// findAgentGitCommands returns the git push and git commit commands in a shell
// command line, in order. A command line that never mentions them returns
// before any parsing.
func findAgentGitCommands(command string) ([]agentGitCommand, error) {
	namesVerb := strings.Contains(command, "push") || strings.Contains(command, "commit")
	if !strings.Contains(command, "git") || !namesVerb {
		return nil, nil
	}
	segments, err := splitShellCommand(command)
	if err != nil {
		return nil, err
	}
	var commands []agentGitCommand
	directory, preceded := "", false
	for index, segment := range segments {
		if target, moves := changedDirectory(segments, index); moves {
			directory = within(directory, target)
			continue
		}
		if found, ok := classifyGitSegment(segment.words); ok {
			found.directory, found.preceded = within(directory, found.directory), preceded
			commands = append(commands, found)
		}
		preceded = true
	}
	return commands, nil
}

// within resolves directory against base, as cd and git -C do.
func within(base, directory string) string {
	if filepath.IsAbs(directory) {
		return directory
	}
	return filepath.Join(base, directory)
}

// sequentialOperators run the next command after this one whatever its
// result, or only once it succeeds.
var sequentialOperators = []string{"&&", ";", "\n"}

// leadsOn reports whether the commands after this one run in the shell it ran
// in, once it has run: it is outside any subshell and ends in && or ;.
func (segment shellSegment) leadsOn() bool {
	return segment.depth == 0 && slices.Contains(sequentialOperators, segment.operator)
}

// changedDirectory returns the directory segments[index] moves the commands
// after it to. Only a cd to one literal word that always runs and leads on
// moves them; a cd in a pipeline, a subshell, or after || does not move the
// shell the git command runs in.
func changedDirectory(segments []shellSegment, index int) (string, bool) {
	segment := segments[index]
	target, ok := segment.words.cdTarget()
	always := index == 0 || segments[index-1].leadsOn()
	return target, ok && always && segment.leadsOn()
}

// cdTarget returns the directory of a cd to one literal word. A word the shell
// would expand, with a variable, a command substitution, ~, or a glob, is not
// literal, nor is cd - or a bare cd.
func (words shellWords) cdTarget() (string, bool) {
	if len(words) != 2 || words[0] != "cd" {
		return "", false
	}
	target := words[1]
	return target, target != "" && target != "-" && !strings.ContainsAny(target, "$`~*?[")
}

var gitVerbCheckpoints = map[string]configuration.CheckpointName{
	"push":   configuration.CheckpointPrePush,
	"commit": configuration.CheckpointPreCommit,
}

func classifyGitSegment(words shellWords) (agentGitCommand, bool) {
	prefixes, words := words.splitCommandPrefixes()
	if len(words) == 0 || filepath.Base(words[0]) != "git" {
		return agentGitCommand{}, false
	}
	reader := &segmentReader{words: words[1:]}
	command, verb := readGitGlobalOptions(reader)
	checkpoint, ok := gitVerbCheckpoints[verb]
	command.checkpoint = checkpoint
	switch {
	case !ok:
		return agentGitCommand{}, false
	case command.form == formUndecidable:
		return command, true
	case prefixes.assignRepository():
		command.form, command.reason = formUndecidable, "a GIT_DIR, GIT_WORK_TREE, or GIT_INDEX_FILE assignment names a repository the hook does not resolve"
		return command, true
	case checkpoint == configuration.CheckpointPrePush:
		return readPushArguments(command, pushOptions.read(reader))
	}
	return readCommitArguments(command, commitOptions.read(reader))
}

// commandPrefixes are words a shell or a wrapper reads before the command
// itself; shellAssignment matches the VAR=value words a shell reads there.
// repositoryVariables are the assignments that point git at another
// repository, work tree, or index.
var (
	commandPrefixes     = []string{"!", "{", "if", "then", "else", "elif", "do", "while", "until", "time", "command", "exec", "nohup", "env"}
	shellAssignment     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	repositoryVariables = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE"}
)

// splitCommandPrefixes returns the prefix words and the command after them.
func (words shellWords) splitCommandPrefixes() (shellWords, shellWords) {
	start := slices.IndexFunc(words, func(word string) bool {
		return !shellAssignment.MatchString(word) && !slices.Contains(commandPrefixes, word)
	})
	if start < 0 {
		return words, nil
	}
	return words[:start], words[start:]
}

func (words shellWords) assignRepository() bool {
	return slices.ContainsFunc(words, func(word string) bool {
		name, _, _ := strings.Cut(word, "=")
		return slices.Contains(repositoryVariables, name)
	})
}

func (words shellWords) firstGitVerb() string {
	for _, word := range words {
		if _, ok := gitVerbCheckpoints[word]; ok {
			return word
		}
	}
	return ""
}

type segmentReader struct {
	words shellWords
	next  int
}

func (reader *segmentReader) done() bool { return reader.next >= len(reader.words) }

func (reader *segmentReader) take() string {
	word := reader.words[reader.next]
	reader.next++
	return word
}

func (reader *segmentReader) rest() shellWords { return reader.words[reader.next:] }

// takeValue returns an option's value: the part after "=", or else the next
// word when one is left.
func (reader *segmentReader) takeValue(option optionWord) string {
	if option.inline || reader.done() {
		return option.value
	}
	return reader.take()
}

// optionWord is one option word, split at "=" as in --name=value.
type optionWord struct {
	word, name, value string
	inline            bool
}

func splitOption(word string) optionWord {
	name, value, inline := strings.Cut(word, "=")
	return optionWord{word: word, name: name, value: value, inline: inline}
}

// gitValueOptions are git's global options that take the next word as their
// value, and gitRepositoryOptions those of them that name a repository the
// hook does not resolve. gitPushConfig prefixes the -c keys that change where
// a push goes. gitFlags take no value.
var (
	gitValueOptions      = []string{"-C", "-c", "--git-dir", "--work-tree", "--namespace", "--super-prefix", "--config-env", "--attr-source", "--shallow-file"}
	gitRepositoryOptions = []string{"--git-dir", "--work-tree"}
	gitPushConfig        = []string{"push.", "remote.", "branch."}
	gitFlags             = []string{"-p", "--paginate", "-P", "--no-pager", "--bare", "--no-replace-objects", "--literal-pathspecs", "--glob-pathspecs", "--noglob-pathspecs", "--icase-pathspecs", "--no-optional-locks", "--no-lazy-fetch", "--no-advice"}
)

// readGitGlobalOptions reads the options between git and its subcommand and
// returns the subcommand. When an option makes the command undecidable, the
// subcommand is the first push or commit word after it.
func readGitGlobalOptions(reader *segmentReader) (command agentGitCommand, verb string) {
	for !reader.done() {
		word := reader.take()
		if !strings.HasPrefix(word, "-") {
			return command, word
		}
		if command.readGitGlobalOption(splitOption(word), reader); command.form == formUndecidable {
			return command, reader.rest().firstGitVerb()
		}
	}
	return command, ""
}

// readGitGlobalOption reads one global option. -C moves the directory git
// runs in. --git-dir, --work-tree, -c with a key that changes where git
// pushes, or an option this reader does not know makes the command
// undecidable.
func (command *agentGitCommand) readGitGlobalOption(option optionWord, reader *segmentReader) {
	switch {
	case slices.Contains(gitFlags, option.word):
	case option.name == "-C":
		command.directory = within(command.directory, reader.takeValue(option))
	case slices.Contains(gitRepositoryOptions, option.name):
		command.form, command.reason = formUndecidable, fmt.Sprintf("git %s names a repository the hook does not resolve", option.name)
	case option.name == "-c":
		if key, _, _ := strings.Cut(strings.ToLower(reader.takeValue(option)), "="); slices.ContainsFunc(gitPushConfig, func(prefix string) bool { return strings.HasPrefix(key, prefix) }) {
			command.form, command.reason = formUndecidable, fmt.Sprintf("git -c %s changes where git pushes, which the hook does not resolve", key)
		}
	case slices.Contains(gitValueOptions, option.name):
		reader.takeValue(option)
	default:
		command.form, command.reason = formUndecidable, fmt.Sprintf("git option %q is not one the hook reads", option.word)
	}
}

// commandOptions describes one subcommand's options: those that take a value,
// the short letters that take one, and what each remaining option means for
// the hook.
type commandOptions struct {
	valued       []string
	valuedShort  string
	optionalTail string
	meanings     map[string]optionMeaning
}

// optionMeaning is what an option changes about the check; the zero value,
// for options missing from a meanings table, changes nothing.
type optionMeaning int

const (
	// meaningDryRun sends nothing, so the hook has nothing to check, unless
	// a later meaningNotDryRun turns it off.
	meaningDryRun optionMeaning = iota + 1
	meaningNotDryRun
	meaningTracked
	meaningPaths
	meaningUndecidable
)

var pushOptions = commandOptions{
	valued:      []string{"--repo", "-o", "--push-option", "--receive-pack", "--exec", "--recurse-submodules"},
	valuedShort: "o",
	meanings: map[string]optionMeaning{
		"-n": meaningDryRun, "--dry-run": meaningDryRun, "--no-dry-run": meaningNotDryRun,
		"--all": meaningUndecidable, "--branches": meaningUndecidable, "--mirror": meaningUndecidable, "--tags": meaningUndecidable,
		"-d": meaningUndecidable, "--delete": meaningUndecidable, "--prune": meaningUndecidable,
	},
}

var commitOptions = commandOptions{
	valued: []string{
		"-m", "--message", "-F", "--file", "-C", "--reuse-message", "-c", "--reedit-message", "-t", "--template",
		"--author", "--date", "--cleanup", "--fixup", "--squash", "--trailer", "--pathspec-from-file",
	},
	valuedShort:  "mFCct",
	optionalTail: "Su",
	meanings: map[string]optionMeaning{
		"--dry-run": meaningDryRun, "--no-dry-run": meaningNotDryRun,
		"-a": meaningTracked, "--all": meaningTracked,
		"-p": meaningPaths, "--patch": meaningPaths, "--interactive": meaningPaths, "-o": meaningPaths, "--only": meaningPaths,
		"-i": meaningPaths, "--include": meaningPaths, "--pathspec-from-file": meaningPaths,
	},
}

type commandArguments struct {
	meanings   []optionMeaning
	positional shellWords
	values     map[string]string
}

func (arguments commandArguments) means(meaning optionMeaning) bool {
	return slices.Contains(arguments.meanings, meaning)
}

// dryRun reports whether the last dry-run option given turns dry run on.
func (arguments commandArguments) dryRun() bool {
	dryRun := false
	for _, meaning := range arguments.meanings {
		if meaning == meaningDryRun || meaning == meaningNotDryRun {
			dryRun = meaning == meaningDryRun
		}
	}
	return dryRun
}

func (options commandOptions) read(reader *segmentReader) commandArguments {
	arguments := commandArguments{values: map[string]string{}}
	for !reader.done() {
		word := reader.take()
		switch {
		case word == "--":
			arguments.positional = append(arguments.positional, reader.rest()...)
			return arguments
		case !strings.HasPrefix(word, "-") || word == "-":
			arguments.positional = append(arguments.positional, word)
		case strings.HasPrefix(word, "--"):
			option := splitOption(word)
			if slices.Contains(options.valued, option.name) {
				arguments.values[option.name] = reader.takeValue(option)
			}
			arguments.meanings = append(arguments.meanings, options.meanings[option.name])
		default:
			arguments.meanings = append(arguments.meanings, options.readCluster(word[1:], reader)...)
		}
	}
	return arguments
}

// readCluster reads short options such as -am, where a letter that takes a
// value takes the rest of the cluster or, when none is left, the next word.
func (options commandOptions) readCluster(letters string, reader *segmentReader) []optionMeaning {
	var meanings []optionMeaning
	for index, letter := range letters {
		meanings = append(meanings, options.meanings["-"+string(letter)])
		switch {
		case strings.ContainsRune(options.valuedShort, letter):
			if index == len(letters)-1 && !reader.done() {
				reader.take()
			}
			return meanings
		case strings.ContainsRune(options.optionalTail, letter):
			return meanings
		}
	}
	return meanings
}

func readPushArguments(command agentGitCommand, arguments commandArguments) (agentGitCommand, bool) {
	switch {
	case arguments.dryRun():
		return agentGitCommand{}, false
	case arguments.means(meaningUndecidable):
		command.form, command.reason = formUndecidable, "git push names refs the hook does not resolve"
		return command, true
	}
	command.form = formPush
	command.push.Remote = arguments.values["--repo"]
	if len(arguments.positional) > 0 {
		command.push.Remote, command.push.Refspecs = arguments.positional[0], arguments.positional[1:]
	}
	return command, true
}

func readCommitArguments(command agentGitCommand, arguments commandArguments) (agentGitCommand, bool) {
	switch {
	case arguments.dryRun():
		return agentGitCommand{}, false
	case arguments.means(meaningPaths) || len(arguments.positional) > 0:
		command.form = formCommitPaths
	case arguments.means(meaningTracked):
		command.form = formCommitTracked
	default:
		command.form = formCommitStaged
	}
	return command, true
}
