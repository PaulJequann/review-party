package main

import (
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"reviewparty/internal/configuration"
)

// initReport renders what a repository still lacks before it can run a
// Review, one line per missing piece, each with the command that adds it.
// Values only the Caller can choose stay as <placeholders>.
type initReport struct {
	manager    *configuration.Manager
	target     configuration.Scope
	repository configuration.Repository
	// configuration is the --config path; config is the argument hints carry.
	configuration string
	config        string
}

func reportInitGaps(output io.Writer, manager *configuration.Manager, target configuration.Scope, repository configuration.Repository, options initOptions) error {
	report := initReport{manager: manager, target: target, repository: repository, configuration: options.configuration}
	if options.configuration != "" {
		absolute, err := filepath.Abs(options.configuration)
		if err != nil {
			return err
		}
		report.config = configurationArgument(absolute)
	}
	lines, err := report.lines()
	if err != nil {
		return err
	}
	if report.target == configuration.ScopeRepository {
		checkpoints, err := report.checkpointLines()
		if err != nil {
			return err
		}
		lines = append(lines, checkpoints...)
	}
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
	baseline, err := report.manager.Baseline(report.repository)
	if err != nil {
		return nil, err
	}
	if !binding.Declared {
		return report.selectionLines(baseline)
	}
	selection, value, err := report.manager.EffectiveReviewSelection(report.repository)
	if err != nil {
		return nil, err
	}
	if lines := report.unresolvedLines(binding, baseline, selection); len(lines) > 0 {
		return lines, nil
	}
	if len(selection.Global)+len(selection.Repository) == 0 {
		return report.selectionLines(baseline)
	}
	resolved, err := report.manager.ResolveRun(configuration.RunRequest{Repository: report.repository})
	if err != nil {
		return nil, err
	}
	return report.readyLines(resolved, value), nil
}

func (report initReport) readyLines(resolved configuration.ResolvedReviews, value configuration.Value[configuration.ReviewSelection]) []string {
	profiles := make([]string, 0, len(resolved.Expanded))
	for _, profile := range resolved.Expanded {
		profiles = append(profiles, string(profile.Scope)+":"+profile.Profile)
	}
	lines := []string{"Saved review configuration is ready."}
	if report.target == configuration.ScopeRepository && value.Source == configuration.SourceGlobal {
		lines = append(lines, "Review selection: Global Configuration "+value.Path)
	}
	lines = append(lines,
		"Effective Profiles: "+strings.Join(profiles, ", "),
		fmt.Sprintf("%s concurrency: %d", scopeTitle(report.target), resolved.ConcurrencyLimit),
		"Reviewer access and execution are unverified.",
	)
	if report.target == configuration.ScopeGlobal {
		return append(lines, "Next: review-party run --repo <repo>"+report.config)
	}
	return append(lines, "Next: review-party run --repo "+shellWord(string(report.repository))+report.config)
}

func (report initReport) initCommand() string {
	if report.target == configuration.ScopeGlobal {
		return "review-party init --global" + report.config
	}
	return "review-party init --repo " + shellWord(string(report.repository)) + report.config
}

// unresolvedLines names each definition a declared selection lacks, with a
// single line for all gaps the Review Party baseline closes. A selected baseline
// that is blocked opens the report, even when every name resolves, because
// the selection would run another composition under the baseline name.
func (report initReport) unresolvedLines(binding configuration.SelectionBinding, baseline configuration.Baseline, selection configuration.ReviewSelection) []string {
	var lines []string
	blocked := baseline.SelectionBlocked(selection)
	if blocked != nil {
		lines = append(lines, "Review Party baseline blocked: "+blocked.Error())
	}
	covered := false
	for _, missing := range binding.Unresolved {
		if baseline.Binds(selection, missing) {
			if !covered && blocked == nil {
				lines = append(lines, report.baselineGapLine(baseline))
			}
			covered = true
			continue
		}
		label, create := report.definitionFix(missing)
		lines = append(lines, label+": "+create)
	}
	return lines
}

// baselineGapLine is the single line for all gaps of a selected baseline.
func (report initReport) baselineGapLine(baseline configuration.Baseline) string {
	label := "Missing Global Party " + configuration.BaselinePartyName
	if baseline.Party.State != configuration.BaselineMissing {
		label = "Missing Global Profiles " + strings.Join(configuration.BaselineNames(baseline.MembersIn(configuration.BaselineMissing)), ", ")
	}
	return label + " (Review Party baseline): " + report.baselineCommand(baseline)
}

// baselineCommand asks for execution only when a member must be created.
func (report initReport) baselineCommand(baseline configuration.Baseline) string {
	command := report.initCommand() + " --baseline"
	if len(baseline.MembersIn(configuration.BaselineMissing)) > 0 {
		command += profileExecutionPlaceholders
	}
	return command
}

// baselineOfferLines open a report without a Review selection: the command
// that adds the Review Party baseline, or why it cannot be added.
func (report initReport) baselineOfferLines(baseline configuration.Baseline) []string {
	if !baseline.Offered() {
		return nil
	}
	if blocked := baseline.Blocked(); blocked != nil {
		return []string{"Review Party baseline blocked: " + blocked.Error()}
	}
	label := "Review Party baseline (" + strings.Join(configuration.BaselineNames(baseline.Members), ", ") + ")"
	return []string{label + ": " + report.baselineCommand(baseline)}
}

func (report initReport) checkpointLines() ([]string, error) {
	enrollment, err := report.manager.Enrollment(report.repository)
	if err != nil {
		return nil, err
	}
	var lines []string
	if enrollment.Offered() {
		lines = append(lines, "Enroll this repository in your Global Checkpoints: review-party init --enroll --yes --repo "+shellWord(string(report.repository))+report.config)
	}
	gaps, err := planFloorGaps(report.manager, string(report.repository), report.configuration)
	if err != nil {
		return nil, err
	}
	for _, gap := range gaps {
		lines = append(lines, initGapLines(gap)...)
	}
	declared, err := report.manager.Checkpoints(report.repository)
	if err != nil {
		return nil, err
	}
	if configuration.FloorIntegrates(declared, configuration.IntegrationCodex) {
		lines = append(lines, codexApprovalStep)
	}
	return lines, nil
}

// initGapLines renders a gap as init reports it: what is missing and the
// command that adds it, or the lines to add by hand.
func initGapLines(gap floorGap) []string {
	switch gap.State {
	case gapEdited:
		return nil
	case gapManual, gapMalformed:
		lines := []string{gap.summary() + ":"}
		for _, line := range gap.Manual {
			lines = append(lines, "  "+line)
		}
		return lines
	case gapMissing, gapNotExecutable, gapInactive, gapStale:
	}
	return []string{gap.summary() + ": " + gap.Fix}
}

// definitionFix names a selected definition this Caller lacks and the
// command that creates it.
func (report initReport) definitionFix(missing configuration.UnresolvedReferenceError) (string, string) {
	label := "Missing " + scopeTitle(missing.Scope)
	target := shellWord(missing.Name) + " --scope " + string(missing.Scope)
	if missing.Scope == configuration.ScopeRepository {
		target += " --repo " + shellWord(string(report.repository))
	}
	if missing.Kind == configuration.ItemParty {
		return label + " Party " + missing.Name, "review-party config party create " + target + report.config +
			" --profile " + string(missing.Scope) + ":<profile> --concurrency-limit <limit>"
	}
	return label + " Profile " + missing.Name, "review-party config profile create " + target + report.config +
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

// selectionLines covers a repository with no Review selection. After the
// Review Party baseline offer, it names the Profiles and Parties the Caller
// can already select, or first the Profile Creation command when there are
// none.
func (report initReport) selectionLines(baseline configuration.Baseline) ([]string, error) {
	profiles, err := report.manager.ProfileInventory(report.repository)
	if err != nil {
		return nil, err
	}
	parties, err := report.manager.PartyInventory(report.repository)
	if err != nil {
		return nil, err
	}
	add := "Missing Review selection: " + report.initCommand() + " --profile <name>"
	profileNames, partyNames := definitionNames(profiles), definitionNames(baseline.OtherParties(parties))
	if len(profileNames) == 0 {
		create := "Missing Review Profile: review-party config profile create <name> --scope global" + report.config +
			" --template <template>" + profileExecutionPlaceholders
		return append(report.baselineOfferLines(baseline), create, add), nil
	}
	add += " (Profiles: " + strings.Join(profileNames, ", ") + ")"
	if len(partyNames) > 0 {
		add += " or --party <name> (Parties: " + strings.Join(partyNames, ", ") + ")"
	}
	return append(report.baselineOfferLines(baseline), add), nil
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
