package main

import (
	"strings"

	"reviewparty/internal/configuration"
)

// floorGap is one thing a declared Checkpoint's team floor still lacks in
// this clone, read from the plan its Integration's installer would carry
// out. Fix is the command that closes it; for a manual gap it prints what to
// add by hand.
type floorGap struct {
	Checkpoint  configuration.CheckpointName  `json:"checkpoint"`
	Integration configuration.IntegrationName `json:"integration"`
	State       floorGapState                 `json:"state"`
	Path        string                        `json:"path"`
	Tool        hookTool                      `json:"tool"`
	Fix         string                        `json:"fix"`
	Manual      []string                      `json:"manual,omitempty"`
}

type floorGapState string

const (
	gapMissing       floorGapState = "missing"
	gapManual        floorGapState = "manual"
	gapNotExecutable floorGapState = "not_executable"
	gapInactive      floorGapState = "inactive"
	gapEdited        floorGapState = "edited"
	gapStale         floorGapState = "stale"
	gapMalformed     floorGapState = "malformed"
)

// floorGapStates maps each installer outcome to the gap it leaves. An
// installed outcome leaves none.
var floorGapStates = map[hookOutcome]floorGapState{
	hookCreated: gapMissing, hookInserted: gapMissing, hookEntryAdded: gapMissing, hookEntryShared: gapMissing,
	hookManual: gapManual, hookNotExecutable: gapNotExecutable,
	hookEdited: gapEdited, hookEntryEdited: gapEdited,
	hookBlockStale: gapStale, hookBlockMalformed: gapMalformed,
}

// planFloorGaps plans every Integration's install for the repository and
// keeps the gaps of the Checkpoints that list it as team floor. Checkpoints
// that do not list an Integration are not reported for it.
func planFloorGaps(manager *configuration.Manager, repository, configurationPath string) ([]floorGap, error) {
	declared, err := manager.Checkpoints(configuration.Repository(repository))
	if err != nil {
		return nil, err
	}
	var gaps []floorGap
	for _, integration := range configuration.IntegrationNames() {
		if !configuration.FloorIntegrates(declared, integration) {
			continue
		}
		found, err := integrationGaps(manager, declared, hookInstallTarget{root: repository, configuration: configurationPath, integration: integration})
		if err != nil {
			return nil, err
		}
		gaps = append(gaps, found...)
	}
	return gaps, nil
}

// integrationGaps plans one Integration's install and keeps the steps of the
// Checkpoints that list it.
func integrationGaps(manager *configuration.Manager, declared map[configuration.CheckpointName]configuration.Checkpoint, target hookInstallTarget) ([]floorGap, error) {
	plan, err := planIntegrationInstall(target, manager)
	if err != nil {
		return nil, err
	}
	install := "review-party checkpoint install " + string(target.integration) + " --repo " + shellWord(target.root) + configurationArgument(target.configuration)
	var gaps []floorGap
	for _, step := range plan.steps {
		if declared[step.checkpoint].Integrates(target.integration) {
			gaps = append(gaps, stepGaps(step, target.integration, plan.tool, install)...)
		}
	}
	return gaps, nil
}

// stepGaps reads one planned step. It returns the gap its outcome leaves,
// then one for a hook tool git does not run in this clone.
func stepGaps(step hookInstallStep, integration configuration.IntegrationName, tool hookTool, install string) []floorGap {
	gap := floorGap{Checkpoint: step.checkpoint, Integration: integration, Path: step.path, Tool: tool, Fix: install}
	var gaps []floorGap
	if state, found := floorGapStates[step.outcome]; found {
		gap.State = state
		if state == gapManual || state == gapMalformed {
			gap.Manual = step.manual
		}
		gaps = append(gaps, gap)
	}
	if step.activate != "" {
		gap.State, gap.Fix, gap.Manual = gapInactive, step.activate, nil
		gaps = append(gaps, gap)
	}
	return gaps
}

// floorGapSummaries says what each gap is, without its fix. {integration}
// names what the Integration installs, such as "git hook".
var floorGapSummaries = map[floorGapState]string{
	gapMissing:       "Checkpoint {checkpoint} has no {integration}",
	gapManual:        "Checkpoint {checkpoint} has no {integration}, and {tool} needs it added to {path} by hand",
	gapNotExecutable: "Checkpoint {checkpoint} {integration} {path} is not executable, so git skips it",
	gapInactive:      "Checkpoint {checkpoint} {integration} does not run until {tool} is active in this clone",
	gapEdited:        "Checkpoint {checkpoint} {integration} in {path} was edited, and install leaves it alone until the edited one is removed",
	gapStale:         "Checkpoint {checkpoint} {integration} in {path} no longer matches the declared Checkpoints",
	gapMalformed:     "Checkpoint {checkpoint} {integration} in {path} has unbalanced or repeated markers to repair by hand",
}

func (gap floorGap) summary() string {
	return strings.NewReplacer(
		"{checkpoint}", string(gap.Checkpoint), "{integration}", string(gap.Integration)+" "+gap.noun(),
		"{tool}", string(gap.Tool), "{path}", gap.Path,
	).Replace(floorGapSummaries[gap.State])
}

// noun is what the Integration installs: a block of agent instructions, or
// a hook.
func (gap floorGap) noun() string {
	if gap.Integration == configuration.IntegrationAgentsMD {
		return "block"
	}
	return "hook"
}
