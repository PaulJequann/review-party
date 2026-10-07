package configuration

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
)

// CheckpointName names a workflow point at which the repository expects its
// Review selection to cover the change.
type CheckpointName string

const (
	CheckpointPrePush   CheckpointName = "pre-push"
	CheckpointPreCommit CheckpointName = "pre-commit"
)

// CheckpointNames lists every supported Checkpoint in workflow order.
func CheckpointNames() []CheckpointName {
	return []CheckpointName{CheckpointPrePush, CheckpointPreCommit}
}

// ParseCheckpointName accepts one supported Checkpoint name.
func ParseCheckpointName(value string) (CheckpointName, error) {
	for _, name := range CheckpointNames() {
		if string(name) == value {
			return name, nil
		}
	}
	return "", fmt.Errorf("unknown checkpoint %q; expected pre-push or pre-commit", value)
}

// CheckpointRequirement says what a Checkpoint expects of the change:
// reviewed needs Coverage, and judged also needs a current Verdict on every
// Finding of the covering Reviews.
type CheckpointRequirement string

const (
	RequirementReviewed CheckpointRequirement = "reviewed"
	RequirementJudged   CheckpointRequirement = "judged"
)

// WaiverPolicy says who may record a Checkpoint Waiver.
type WaiverPolicy string

const (
	WaiversAnyone WaiverPolicy = "anyone"
	WaiversHuman  WaiverPolicy = "human"
	WaiversNone   WaiverPolicy = "none"
)

// IntegrationName names a Checkpoint Integration the team installs as floor.
type IntegrationName string

const (
	IntegrationGit        IntegrationName = "git"
	IntegrationClaudeCode IntegrationName = "claude-code"
	IntegrationCodex      IntegrationName = "codex"
	IntegrationAgentsMD   IntegrationName = "agents-md"
)

// IntegrationNames lists every supported Integration in install order.
func IntegrationNames() []IntegrationName {
	return []IntegrationName{IntegrationGit, IntegrationClaudeCode, IntegrationCodex, IntegrationAgentsMD}
}

// ParseIntegrationName accepts one supported Integration name.
func ParseIntegrationName(value string) (IntegrationName, error) {
	for _, name := range IntegrationNames() {
		if string(name) == value {
			return name, nil
		}
	}
	return "", fmt.Errorf("unknown integration %q; expected git, claude-code, codex, or agents-md", value)
}

// Checkpoint is one declared Review Checkpoint. UnreviewedLines is the
// allowance: a change passes while no Profile has more unreviewed lines than
// it. ReviewBudget caps how many Reviews one Profile spends on a change before
// the Checkpoint stops and asks a person.
type Checkpoint struct {
	Requirement     CheckpointRequirement `json:"requirement"`
	ExemptPaths     []string              `json:"exempt_paths,omitempty"`
	UnreviewedLines int                   `json:"unreviewed_lines"`
	ReviewBudget    int                   `json:"review_budget"`
	Waivers         WaiverPolicy          `json:"waivers,omitempty"`
	Integrations    []IntegrationName     `json:"integrations,omitempty"`
}

// Defaults a declaration takes when the Caller sets nothing else.
const (
	DefaultUnreviewedLines = 0
	DefaultReviewBudget    = 3
	MaxReviewBudget        = 9
)

// ValidReviewBudget is the one definition of the budget's range.
func ValidReviewBudget(budget int) bool {
	return budget >= 1 && budget <= MaxReviewBudget
}

// NewCheckpoint returns a Checkpoint with the declared defaults: reviewed,
// no allowance, a budget of three Reviews, human waivers, no exemptions, and
// no integrations.
func NewCheckpoint() Checkpoint {
	return Checkpoint{Requirement: RequirementReviewed, UnreviewedLines: DefaultUnreviewedLines, ReviewBudget: DefaultReviewBudget, Waivers: WaiversHuman}
}

func (checkpoint *Checkpoint) UnmarshalJSON(payload []byte) error {
	fields, err := decodeObjectFields(payload, "checkpoint", "requirement", "exempt_paths", "unreviewed_lines", "review_budget", "waivers", "integrations")
	if err != nil {
		return err
	}
	for _, required := range []string{"unreviewed_lines", "review_budget"} {
		if _, present := fields[required]; !present {
			return fmt.Errorf("%s is required", required)
		}
	}
	type plainCheckpoint Checkpoint
	var decoded plainCheckpoint
	if err := strictDecode(payload, &decoded); err != nil {
		return err
	}
	if decoded.Waivers == "" {
		decoded.Waivers = WaiversHuman
	}
	*checkpoint = Checkpoint(decoded)
	return nil
}

// Exempts reports whether an exempt_paths pattern matches a repository path.
func (checkpoint Checkpoint) Exempts(name string) bool {
	for _, pattern := range checkpoint.ExemptPaths {
		if matchPathPattern(pattern, name) {
			return true
		}
	}
	return false
}

// FloorIntegrates reports whether any of the Checkpoints lists the
// Integration as team floor.
func FloorIntegrates(checkpoints map[CheckpointName]Checkpoint, integration IntegrationName) bool {
	for _, checkpoint := range checkpoints {
		if checkpoint.Integrates(integration) {
			return true
		}
	}
	return false
}

// Integrates reports whether the Checkpoint lists the Integration as team floor.
func (checkpoint Checkpoint) Integrates(integration IntegrationName) bool {
	for _, listed := range checkpoint.Integrations {
		if listed == integration {
			return true
		}
	}
	return false
}

// Summary renders the Checkpoint as one line for previews and reports.
func (checkpoint Checkpoint) Summary(name CheckpointName) string {
	parts := []string{string(name) + ": " + string(checkpoint.Requirement), "waivers " + string(checkpoint.Waivers)}
	if len(checkpoint.ExemptPaths) > 0 {
		parts = append(parts, "exempt "+strings.Join(checkpoint.ExemptPaths, " "))
	}
	parts = append(parts, fmt.Sprintf("unreviewed lines %d", checkpoint.UnreviewedLines), fmt.Sprintf("review budget %d", checkpoint.ReviewBudget))
	if len(checkpoint.Integrations) > 0 {
		integrations := make([]string, len(checkpoint.Integrations))
		for index, integration := range checkpoint.Integrations {
			integrations[index] = string(integration)
		}
		parts = append(parts, "integrations "+strings.Join(integrations, " "))
	}
	return strings.Join(parts, ", ")
}

// SortedCheckpointNames returns declared names in workflow order.
func SortedCheckpointNames(checkpoints map[CheckpointName]Checkpoint) []CheckpointName {
	names := make([]CheckpointName, 0, len(checkpoints))
	for _, name := range CheckpointNames() {
		if _, declared := checkpoints[name]; declared {
			names = append(names, name)
		}
	}
	return names
}

// Checkpoints returns the Checkpoints declared in Repository Configuration.
func (manager *Manager) Checkpoints(repository Repository) (map[CheckpointName]Checkpoint, error) {
	loaded, err := manager.Load(repository)
	if err != nil {
		return nil, err
	}
	return loaded.Repository.Document.Checkpoints, nil
}

func validateCheckpoints(checkpoints map[CheckpointName]Checkpoint) error {
	names := make([]string, 0, len(checkpoints))
	for name := range checkpoints {
		names = append(names, string(name))
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := ParseCheckpointName(name); err != nil {
			return fmt.Errorf("checkpoints: %w", err)
		}
		if err := validateCheckpoint(checkpoints[CheckpointName(name)]); err != nil {
			return fmt.Errorf("checkpoints.%s.%w", name, err)
		}
	}
	return nil
}

// checkpointFieldError prefixes a field name so validateCheckpoints can
// report the full dotted path.
type checkpointFieldError struct {
	field string
	err   error
}

func (failure checkpointFieldError) Error() string { return failure.field + ": " + failure.err.Error() }
func (failure checkpointFieldError) Unwrap() error { return failure.err }

func validateCheckpoint(checkpoint Checkpoint) error {
	switch checkpoint.Requirement {
	case RequirementReviewed, RequirementJudged:
	default:
		return checkpointFieldError{"requirement", fmt.Errorf("unknown requirement %q; expected reviewed or judged", checkpoint.Requirement)}
	}
	switch checkpoint.Waivers {
	case WaiversAnyone, WaiversHuman, WaiversNone:
	default:
		return checkpointFieldError{"waivers", fmt.Errorf("unknown waiver policy %q; expected anyone, human, or none", checkpoint.Waivers)}
	}
	if checkpoint.UnreviewedLines < 0 {
		return checkpointFieldError{"unreviewed_lines", errors.New("must not be negative")}
	}
	if !ValidReviewBudget(checkpoint.ReviewBudget) {
		return checkpointFieldError{"review_budget", fmt.Errorf("must be between 1 and %d", MaxReviewBudget)}
	}
	for _, pattern := range checkpoint.ExemptPaths {
		if err := ValidatePathPattern(pattern); err != nil {
			return checkpointFieldError{"exempt_paths", err}
		}
	}
	return validateIntegrations(checkpoint.Integrations)
}

func validateIntegrations(integrations []IntegrationName) error {
	seen := map[IntegrationName]bool{}
	for _, integration := range integrations {
		if _, err := ParseIntegrationName(string(integration)); err != nil {
			return checkpointFieldError{"integrations", err}
		}
		if seen[integration] {
			return checkpointFieldError{"integrations", fmt.Errorf("integration %q is listed twice", integration)}
		}
		seen[integration] = true
	}
	return nil
}

// ValidatePathPattern accepts one exempt_paths pattern: slash-separated and
// relative to the repository root, each segment a path.Match pattern or "**".
func ValidatePathPattern(pattern string) error {
	if pattern == "" {
		return errors.New("pattern must not be empty")
	}
	if strings.HasPrefix(pattern, "/") {
		return fmt.Errorf("pattern %q must be relative to the repository root", pattern)
	}
	for _, segment := range strings.Split(pattern, "/") {
		switch segment {
		case "", ".", "..":
			return fmt.Errorf("pattern %q has an empty, \".\", or \"..\" segment", pattern)
		}
		if _, err := path.Match(segment, ""); err != nil {
			return fmt.Errorf("pattern %q is malformed: %w", pattern, err)
		}
	}
	return nil
}

// matchPathPattern matches one repository path against one validated pattern.
// A pattern without "/" matches the base name at any depth.
func matchPathPattern(pattern, name string) bool {
	segments := strings.Split(pattern, "/")
	if len(segments) == 1 {
		segments = []string{"**", pattern}
	}
	return matchSegments(segments, strings.Split(name, "/"))
}

// matchSegments matches greedily and, on a mismatch, retries from the latest
// "**" with one more segment in it. An earlier "**" never needs a retry, since
// the later one can absorb whatever it would have, so matching stays linear in
// the pattern times the path rather than exponential in its "**" segments.
func matchSegments(pattern, name []string) bool {
	next, star, resume := 0, -1, 0
	for position := 0; position < len(name); {
		switch {
		case next < len(pattern) && pattern[next] == "**":
			star, resume = next, position
			next++
		case next < len(pattern) && segmentMatches(pattern[next], name[position]):
			next++
			position++
		case star >= 0:
			resume++
			next, position = star+1, resume
		default:
			return false
		}
	}
	return !slices.ContainsFunc(pattern[next:], func(segment string) bool { return segment != "**" })
}

func segmentMatches(pattern, name string) bool {
	matched, err := path.Match(pattern, name)
	return err == nil && matched
}

// SetCheckpoint creates or replaces one declared Checkpoint.
type SetCheckpoint struct {
	Name       CheckpointName
	Checkpoint Checkpoint
}

// RemoveCheckpoint removes one declared Checkpoint.
type RemoveCheckpoint struct {
	Name CheckpointName
}

func checkpointField(name CheckpointName) string { return "checkpoints." + string(name) }

func readCheckpoint(document Document, name CheckpointName) (string, bool) {
	checkpoint, declared := document.Checkpoints[name]
	if !declared {
		return "", false
	}
	payload, err := json.Marshal(checkpoint)
	if err != nil {
		panic(fmt.Sprintf("encode checkpoint preview: %v", err))
	}
	return string(payload), true
}

func (intent SetCheckpoint) intentScope() Scope  { return ScopeRepository }
func (intent SetCheckpoint) intentField() string { return checkpointField(intent.Name) }
func (intent SetCheckpoint) applyIntent(document *Document) {
	checkpoints := make(map[CheckpointName]Checkpoint, len(document.Checkpoints)+1)
	for name, checkpoint := range document.Checkpoints {
		checkpoints[name] = checkpoint
	}
	checkpoint := intent.Checkpoint
	checkpoint.ExemptPaths = append([]string(nil), checkpoint.ExemptPaths...)
	checkpoint.Integrations = append([]IntegrationName(nil), checkpoint.Integrations...)
	checkpoints[intent.Name] = checkpoint
	document.Checkpoints = checkpoints
}
func (intent SetCheckpoint) readIntent(document Document) (string, bool) {
	return readCheckpoint(document, intent.Name)
}

func (intent RemoveCheckpoint) intentScope() Scope  { return ScopeRepository }
func (intent RemoveCheckpoint) intentField() string { return checkpointField(intent.Name) }
func (intent RemoveCheckpoint) applyIntent(document *Document) {
	checkpoints := map[CheckpointName]Checkpoint{}
	for name, checkpoint := range document.Checkpoints {
		if name != intent.Name {
			checkpoints[name] = checkpoint
		}
	}
	if len(checkpoints) == 0 {
		checkpoints = nil
	}
	document.Checkpoints = checkpoints
}
func (intent RemoveCheckpoint) readIntent(document Document) (string, bool) {
	return readCheckpoint(document, intent.Name)
}
