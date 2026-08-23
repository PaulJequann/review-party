package engine

import (
	"fmt"
	"sort"
	"strings"

	"reviewparty/internal/configuration"
)

func (library profileLibrary) missingProfileError(repository, name string, searched []string) error {
	available := library.availableProfileNames(repository)
	formatted := make([]string, 0, len(searched))
	for _, source := range searched {
		formatted = append(formatted, "  "+source)
	}
	return fmt.Errorf("profile %q was not found\nsearched:\n%s\navailable: %s", name, strings.Join(formatted, "\n"), listOrNone(available))
}

func (library profileLibrary) availableProfileNames(repository string) []string {
	names := make(map[string]struct{})
	seen := make(map[string]struct{})
	authored, err := library.authoredProfileLibrary(configuration.Repository(repository))
	if err == nil {
		if entries, entriesErr := authored.Entries(); entriesErr == nil {
			addAvailableProfileNames(names, seen, authored, entries)
		}
	}
	addAvailablePackagedProfileNames(names, seen)
	available := make([]string, 0, len(names))
	for name := range names {
		available = append(available, name)
	}
	sort.Strings(available)
	return available
}

func addAvailableProfileNames(names, seen map[string]struct{}, authored configuration.AuthoredLibrary, entries []configuration.AuthoredEntry) {
	for _, entry := range entries {
		name := entry.Name
		if !isAvailableProfileCandidate(name, seen) {
			continue
		}
		seen[name] = struct{}{}
		if _, found, err := readAuthoredProfile(authored, entry); err == nil && found {
			names[name] = struct{}{}
		}
	}
}

func addAvailablePackagedProfileNames(names, seen map[string]struct{}) {
	entries, err := packagedProfileEntries()
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !isAvailableProfileCandidate(entry.name, seen) {
			continue
		}
		seen[entry.name] = struct{}{}
		if _, found, err := readPackagedProfile(entry); err == nil && found {
			names[entry.name] = struct{}{}
		}
	}
}

func isAvailableProfileCandidate(name string, seen map[string]struct{}) bool {
	_, alreadySeen := seen[name]
	return validateProfileName(name) == nil && !alreadySeen
}

func listOrNone(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ", ")
}
