package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reviewparty/internal/model"
)

const partyDefinitionSchema = 1

const maximumPartyBytes = 16 * 1024

type PartySummary = model.PartySummary

type partyLayer struct {
	directory string
	source    string
}

func builtinPartyDefinitions() []model.PartyDefinition {
	return []model.PartyDefinition{{
		SchemaVersion: partyDefinitionSchema,
		Name:          "standard",
		Description:   "Bugs, code-quality, and documentation reviews over one shared Review Subject.",
		Profiles:      []model.PartyMember{{Profile: "bugs"}, {Profile: "code-quality"}, {Profile: "documentation"}},
	}}
}

func (conductor *Conductor) partyLayers(repository string) []partyLayer {
	var layers []partyLayer
	if repository != "" {
		layers = append(layers, partyLayer{directory: filepath.Join(repository, ".reviewparty", "parties"), source: "repository"})
	}
	if global := conductor.profiles.globalDirectory; global != "" {
		layers = append(layers, partyLayer{directory: filepath.Join(global, "parties"), source: "global"})
	}
	return layers
}

type partyLookup struct {
	repository string
	name       string
}

func (conductor *Conductor) resolveParty(lookup partyLookup) (model.PartyDefinition, string, error) {
	definition, source, found, err := conductor.resolveFilesystemParty(lookup)
	if err != nil {
		return model.PartyDefinition{}, "", err
	}
	if found {
		return definition, source, nil
	}
	if definition, found := builtinPartyDefinition(lookup.name); found {
		return definition, "packaged", nil
	}
	return model.PartyDefinition{}, "", UnknownPartyError{Name: lookup.name, Available: conductor.partyNames(lookup.repository)}
}

func (conductor *Conductor) resolveFilesystemParty(lookup partyLookup) (model.PartyDefinition, string, bool, error) {
	for _, layer := range conductor.partyLayers(lookup.repository) {
		definition, found, err := readPartyDefinition(layer, lookup)
		if err != nil {
			return model.PartyDefinition{}, "", false, err
		}
		if found {
			return definition, layer.source + ":.reviewparty/parties/" + lookup.name + ".json", true, nil
		}
	}
	return model.PartyDefinition{}, "", false, nil
}

func builtinPartyDefinition(name string) (model.PartyDefinition, bool) {
	for _, definition := range builtinPartyDefinitions() {
		if definition.Name == name {
			return definition, true
		}
	}
	return model.PartyDefinition{}, false
}

func readPartyDefinition(layer partyLayer, lookup partyLookup) (model.PartyDefinition, bool, error) {
	name := lookup.name
	path := filepath.Join(layer.directory, name+".json")
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return model.PartyDefinition{}, false, nil
	}
	if err != nil {
		return model.PartyDefinition{}, false, fmt.Errorf("read party %q: %w", name, err)
	}
	if len(payload) > maximumPartyBytes {
		return model.PartyDefinition{}, false, fmt.Errorf("party %q exceeds %d bytes", name, maximumPartyBytes)
	}
	var definition model.PartyDefinition
	if err := decodeStrictObject(payload, &definition, "party definition "+name, "schema_version", "name", "description", "profiles"); err != nil {
		return model.PartyDefinition{}, false, InvalidPartyDefinitionError{Name: name, Reason: err.Error()}
	}
	if err := validatePartyDefinition(definition); err != nil {
		return model.PartyDefinition{}, false, InvalidPartyDefinitionError{Name: name, Reason: err.Error()}
	}
	if definition.Name != name {
		return model.PartyDefinition{}, false, InvalidPartyDefinitionError{Name: name, Reason: fmt.Sprintf("name field %q does not match file name", definition.Name)}
	}
	return definition, true, nil
}

func validatePartyDefinition(definition model.PartyDefinition) error {
	if definition.SchemaVersion != partyDefinitionSchema {
		return fmt.Errorf("unsupported schema_version %d", definition.SchemaVersion)
	}
	if err := validateProfileName(definition.Name); err != nil {
		return fmt.Errorf("name %s", err)
	}
	if len(definition.Profiles) == 0 {
		return errors.New("profiles must contain at least one member")
	}
	seen := make(map[string]struct{}, len(definition.Profiles))
	for index, member := range definition.Profiles {
		if strings.TrimSpace(member.Profile) == "" {
			return fmt.Errorf("profiles[%d] requires a profile", index)
		}
		if _, exists := seen[member.Profile]; exists {
			return fmt.Errorf("profiles contains duplicate profile %q", member.Profile)
		}
		seen[member.Profile] = struct{}{}
	}
	if definition.ConcurrencyLimit < 0 {
		return errors.New("concurrency_limit must be positive")
	}
	return nil
}

type UnknownPartyError struct {
	Name      string
	Available []string
}

func (failure UnknownPartyError) Error() string {
	return fmt.Sprintf("unknown review party %q; expected %s", failure.Name, strings.Join(failure.Available, ", "))
}

type InvalidPartyDefinitionError struct {
	Name   string
	Reason string
}

func (failure InvalidPartyDefinitionError) Error() string {
	return fmt.Sprintf("invalid party definition %q: %s", failure.Name, failure.Reason)
}

func (conductor *Conductor) partyNames(repository string) []string {
	names := make(map[string]struct{})
	for _, definition := range builtinPartyDefinitions() {
		names[definition.Name] = struct{}{}
	}
	for _, layer := range conductor.partyLayers(repository) {
		addDirectoryPartyNames(names, layer.directory)
	}
	return sortedPartyNames(names)
}

func addDirectoryPartyNames(names map[string]struct{}, directory string) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		names[strings.TrimSuffix(entry.Name(), ".json")] = struct{}{}
	}
}

func sortedPartyNames(names map[string]struct{}) []string {
	available := make([]string, 0, len(names))
	for name := range names {
		available = append(available, name)
	}
	sort.Strings(available)
	return available
}

func (conductor *Conductor) PartiesForRepository(repository string) ([]PartySummary, error) {
	root, err := resolvePartyRepositoryRoot(repository)
	if err != nil {
		return nil, err
	}
	summaries := make([]PartySummary, 0)
	seen := make(map[string]struct{})
	if err := conductor.addLayerParties(&summaries, seen, root); err != nil {
		return nil, err
	}
	addPackagedParties(&summaries, seen)
	sort.Slice(summaries, func(left, right int) bool { return summaries[left].Name < summaries[right].Name })
	return summaries, nil
}

func resolvePartyRepositoryRoot(repository string) (string, error) {
	if repository == "" {
		return "", nil
	}
	return resolveRepositoryRoot(repository)
}

func (conductor *Conductor) addLayerParties(summaries *[]PartySummary, seen map[string]struct{}, repository string) error {
	for _, layer := range conductor.partyLayers(repository) {
		if err := addLayerPartySummaries(summaries, seen, layer); err != nil {
			return err
		}
	}
	return nil
}

func addPackagedParties(summaries *[]PartySummary, seen map[string]struct{}) {
	for _, definition := range builtinPartyDefinitions() {
		if _, exists := seen[definition.Name]; exists {
			continue
		}
		seen[definition.Name] = struct{}{}
		*summaries = append(*summaries, PartySummary{
			Name:        definition.Name,
			Description: definition.Description,
			Members:     partyMemberNames(definition),
			Source:      "packaged",
		})
	}
}

func addLayerPartySummaries(summaries *[]PartySummary, seen map[string]struct{}, layer partyLayer) error {
	entries, err := os.ReadDir(layer.directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list parties in %q: %w", layer.directory, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		if _, exists := seen[name]; exists {
			continue
		}
		summary := layerPartySummary(layer, partyLookup{name: name})
		if summary.Source == "" {
			continue
		}
		seen[name] = struct{}{}
		*summaries = append(*summaries, summary)
	}
	return nil
}

// layerPartySummary summarizes one party file. An empty Source reports a
// missing definition that should not be listed.
func layerPartySummary(layer partyLayer, lookup partyLookup) PartySummary {
	summary := PartySummary{Name: lookup.name, Source: layer.source}
	definition, found, err := readPartyDefinition(layer, lookup)
	switch {
	case err != nil:
		summary.Error = err.Error()
	case !found:
		summary.Source = ""
	default:
		summary.Description = definition.Description
		summary.Members = partyMemberNames(definition)
	}
	return summary
}

func partyMemberNames(definition model.PartyDefinition) []string {
	members := make([]string, 0, len(definition.Profiles))
	for _, member := range definition.Profiles {
		members = append(members, member.Profile)
	}
	return members
}
