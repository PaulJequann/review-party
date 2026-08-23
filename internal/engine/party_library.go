package engine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

const partyDefinitionSchema = 1

func decodeStrictObject(payload []byte, destination any, objectName string, nonNullFields ...string) error {
	if bytes.Equal(bytes.TrimSpace(payload), []byte("null")) {
		return fmt.Errorf("%s must be an object, not null", objectName)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return err
	}
	for _, name := range nonNullFields {
		if value, exists := fields[name]; exists && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("%s must not be null", name)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	return rejectTrailingJSON(decoder)
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); errors.Is(err, io.EOF) {
		return nil
	} else if err != nil {
		return err
	}
	return errors.New("multiple JSON values")
}

type PartySummary = model.PartySummary

func builtinPartyDefinitions() []model.PartyDefinition {
	return []model.PartyDefinition{{
		SchemaVersion: partyDefinitionSchema,
		Name:          "standard",
		Description:   "Bugs, code-quality, and documentation reviews over one shared Review Subject.",
		Profiles:      []model.PartyMember{{Profile: "bugs"}, {Profile: "code-quality"}, {Profile: "documentation"}},
	}}
}

func (conductor *Conductor) authoredPartyLibrary(repository string) (configuration.AuthoredLibrary, error) {
	return conductor.profiles.manager().AuthoredLibrary(configuration.LibraryParties, configuration.Repository(repository))
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
	available := conductor.partyNames(lookup.repository)
	return model.PartyDefinition{}, "", UnknownPartyError{Name: lookup.name, Available: available}
}

func (conductor *Conductor) resolveFilesystemParty(lookup partyLookup) (model.PartyDefinition, string, bool, error) {
	library, err := conductor.authoredPartyLibrary(lookup.repository)
	if err != nil {
		return model.PartyDefinition{}, "", false, err
	}
	definition, entry, found, err := readPartyDefinition(library, lookup)
	if err != nil {
		return model.PartyDefinition{}, "", false, err
	}
	if found {
		return definition, entry.Source, true, nil
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

func readPartyDefinition(library configuration.AuthoredLibrary, lookup partyLookup) (model.PartyDefinition, configuration.AuthoredEntry, bool, error) {
	entry, payload, found, err := library.Read(lookup.name)
	if err != nil {
		return model.PartyDefinition{}, entry, false, fmt.Errorf("read party %q: %w", lookup.name, err)
	}
	if !found {
		return model.PartyDefinition{}, entry, false, nil
	}
	definition, err := decodePartyDefinition(payload, lookup.name)
	if err != nil {
		return model.PartyDefinition{}, entry, false, err
	}
	return definition, entry, true, nil
}

func decodePartyDefinition(payload []byte, name string) (model.PartyDefinition, error) {
	var definition model.PartyDefinition
	if err := decodeStrictObject(payload, &definition, "party definition "+name, "schema_version", "name", "description", "profiles"); err != nil {
		return model.PartyDefinition{}, InvalidPartyDefinitionError{Name: name, Reason: err.Error()}
	}
	if err := validatePartyDefinition(definition); err != nil {
		return model.PartyDefinition{}, InvalidPartyDefinitionError{Name: name, Reason: err.Error()}
	}
	if definition.Name != name {
		return model.PartyDefinition{}, InvalidPartyDefinitionError{Name: name, Reason: fmt.Sprintf("name field %q does not match file name", definition.Name)}
	}
	return definition, nil
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
	if library, err := conductor.authoredPartyLibrary(repository); err == nil {
		addAuthoredPartyNames(names, library)
	}
	return sortedPartyNames(names)
}

func addAuthoredPartyNames(names map[string]struct{}, library configuration.AuthoredLibrary) {
	entries, err := library.Entries()
	if err != nil {
		return
	}
	for _, entry := range entries {
		names[entry.Name] = struct{}{}
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
	library, err := conductor.authoredPartyLibrary(root)
	if err != nil {
		return nil, err
	}
	summaries := make([]PartySummary, 0)
	seen := make(map[string]struct{})
	entries, err := library.Entries()
	if err != nil {
		return nil, err
	}
	if err := addAuthoredPartySummaries(&summaries, seen, library, entries); err != nil {
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

func addAuthoredPartySummaries(summaries *[]PartySummary, seen map[string]struct{}, library configuration.AuthoredLibrary, entries []configuration.AuthoredEntry) error {
	for _, entry := range entries {
		name := entry.Name
		if _, exists := seen[name]; exists {
			continue
		}
		summary := authoredPartySummary(library, entry)
		if summary.Source == "" {
			continue
		}
		seen[name] = struct{}{}
		*summaries = append(*summaries, summary)
	}
	return nil
}

// authoredPartySummary summarizes one party file. An empty Source reports a
// missing definition that should not be listed.
func authoredPartySummary(library configuration.AuthoredLibrary, entry configuration.AuthoredEntry) PartySummary {
	summary := PartySummary{Name: entry.Name, Source: string(entry.Scope)}
	payload, found, err := library.ReadEntry(entry)
	switch {
	case err != nil:
		summary.Error = err.Error()
	case !found:
		summary.Source = ""
	default:
		definition, decodeErr := decodePartyDefinition(payload, entry.Name)
		if decodeErr != nil {
			summary.Error = decodeErr.Error()
		} else {
			summary.Description = definition.Description
			summary.Members = partyMemberNames(definition)
		}
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
