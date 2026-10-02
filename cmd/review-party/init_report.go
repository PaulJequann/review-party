package main

import (
	"io"
	"slices"
	"strings"

	"reviewparty/internal/configuration"
)

// initReport renders what a repository still lacks before it can run a
// Review, one line per missing piece, each with the command that adds it.
// Values only the Caller can choose stay as <placeholders>.
type initReport struct {
	manager    *configuration.Manager
	repository configuration.Repository
	// configuration is the --config path; config is the argument hints carry.
	configuration string
	config        string
}

func reportInitGaps(output io.Writer, manager *configuration.Manager, repository configuration.Repository, options initOptions) error {
	report := initReport{manager: manager, repository: repository, configuration: options.configuration, config: configurationArgument(options.configuration)}
	lines, err := report.lines()
	if err != nil {
		return err
	}
	hooks, err := report.checkpointLines()
	if err != nil {
		return err
	}
	lines = append(lines, hooks...)
	return writeCommandOutput(output, func(output *commandOutput) {
		for _, line := range lines {
			output.write("%s\n", line)
		}
	})
}

func (report initReport) lines() ([]string, error) {
	binding, err := report.manager.SelectionBinding(report.repository)
	if err != nil {
		return nil, err
	}
	if !binding.Declared {
		return report.selectionLines()
	}
	if binding.Ready() {
		return []string{"Repository is ready: review-party run --repo " + shellWord(string(report.repository)) + report.config}, nil
	}
	lines := make([]string, 0, len(binding.Unresolved))
	for _, missing := range binding.Unresolved {
		lines = append(lines, report.definitionLine(missing))
	}
	return lines, nil
}

// checkpointLines names each Checkpoint that lists the git Integration as team
// floor but has no git hook in place, with the command that installs it or the
// snippet a hook tool needs by hand. A block someone edited counts as in place.
func (report initReport) checkpointLines() ([]string, error) {
	declared, err := report.manager.Checkpoints(report.repository)
	if err != nil {
		return nil, err
	}
	plan, err := planHookInstall(hookInstallTarget{root: string(report.repository), configuration: report.configuration}, report.manager)
	if err != nil {
		return nil, err
	}
	install := "review-party checkpoint install git --repo " + shellWord(string(report.repository)) + report.config
	var lines []string
	for _, step := range plan.steps {
		if declared[step.checkpoint].Integrates(configuration.IntegrationGit) {
			lines = append(lines, hookGapLines(step, plan.tool, install)...)
		}
	}
	return lines, nil
}

// hookGapLines reports what one planned hook step says is still missing:
// the snippet when it must be added by hand, the install command when the
// installer can add it, and the command that activates a hook tool git does
// not run in this clone.
func hookGapLines(step hookInstallStep, tool hookTool, install string) []string {
	checkpoint := "Checkpoint " + string(step.checkpoint)
	var lines []string
	switch step.outcome {
	case hookManual:
		lines = append(lines, checkpoint+" has no git hook; add to "+step.path+" ("+string(tool)+") by hand:")
		for _, line := range step.manual {
			lines = append(lines, "  "+line)
		}
	case hookCreated, hookInserted, hookAppended:
		lines = append(lines, checkpoint+" has no git hook: "+install)
	case hookNotExecutable:
		lines = append(lines, checkpoint+" git hook "+step.path+" is not executable, so git skips it: "+install)
	case hookRefreshed:
		lines = append(lines, checkpoint+" git hook "+step.path+" loads another configuration: "+install)
	case hookInstalled, hookEdited:
	}
	if step.activate != "" {
		lines = append(lines, checkpoint+" git hook does not run until "+string(tool)+" is active in this clone: "+step.activate)
	}
	return lines
}

func (report initReport) definitionLine(missing configuration.UnresolvedReferenceError) string {
	label := "Missing " + scopeTitle(missing.Scope)
	target := shellWord(missing.Name) + " --scope " + string(missing.Scope)
	if missing.Scope == configuration.ScopeRepository {
		target += " --repo " + shellWord(string(report.repository))
	}
	if missing.Kind == configuration.ItemParty {
		return label + " Party " + missing.Name + ": review-party config party create " + target + report.config +
			" --profile " + string(missing.Scope) + ":<profile> --concurrency-limit <limit>"
	}
	return label + " Profile " + missing.Name + ": review-party config profile create " + target + report.config +
		" --template " + report.templateFor(missing.Name) + profileExecutionPlaceholders
}

const profileExecutionPlaceholders = " --reviewer <reviewer> --model <model> --effort <effort> --deadline <deadline>"

func (report initReport) templateFor(name string) string {
	if _, found := report.manager.Template(name); found {
		return shellWord(name)
	}
	return "<template>"
}

func scopeTitle(scope configuration.Scope) string {
	if scope == configuration.ScopeRepository {
		return "Repository"
	}
	return "Global"
}

// selectionLines covers a repository with no Review selection. It names the
// Profiles and Parties the Caller can already select, or first the Profile
// Creation command when there are none.
func (report initReport) selectionLines() ([]string, error) {
	profiles, err := report.manager.ProfileInventory(report.repository)
	if err != nil {
		return nil, err
	}
	parties, err := report.manager.PartyInventory(report.repository)
	if err != nil {
		return nil, err
	}
	add := "Missing Review selection: review-party init --repo " + shellWord(string(report.repository)) + report.config + " --profile <name>"
	profileNames, partyNames := definitionNames(profiles), definitionNames(parties)
	if len(profileNames) == 0 {
		create := "Missing Review Profile: review-party config profile create <name> --scope global" + report.config +
			" --template <template>" + profileExecutionPlaceholders
		return []string{create, add}, nil
	}
	add += " (Profiles: " + strings.Join(profileNames, ", ") + ")"
	if len(partyNames) > 0 {
		add += " or --party <name> (Parties: " + strings.Join(partyNames, ", ") + ")"
	}
	return []string{add}, nil
}

// definitionNames lists valid definitions as the unqualified names init
// accepts, once each even when both scopes define a name.
func definitionNames[T any](definitions []configuration.Definition[T]) []string {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		if definition.Err == nil && !slices.Contains(names, definition.Name) {
			names = append(names, definition.Name)
		}
	}
	return names
}

// shellWord leaves a word that needs no quoting bare and quotes anything else.
func shellWord(value string) string {
	if value != "" && strings.Trim(value, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-/:@+=,") == "" {
		return value
	}
	return shellQuoteArgument(value)
}
