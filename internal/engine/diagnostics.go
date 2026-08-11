package engine

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

func (library profileLibrary) missingProfileError(repository, name string, candidates []profileCandidate) error {
	available := library.availableProfileNames(repository)
	searched := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		searched = append(searched, "  "+candidate.source)
	}
	return fmt.Errorf("profile %q was not found\nsearched:\n%s\navailable: %s", name, strings.Join(searched, "\n"), listOrNone(available))
}

func (library profileLibrary) availableProfileNames(repository string) []string {
	names := make(map[string]struct{})
	seen := make(map[string]struct{})
	for _, layer := range library.profileLayers(repository) {
		entries, err := readProfileDirectory(layer)
		if err != nil {
			continue
		}
		addAvailableProfileNames(names, seen, layer, entries)
	}
	available := make([]string, 0, len(names))
	for name := range names {
		available = append(available, name)
	}
	sort.Strings(available)
	return available
}

func addAvailableProfileNames(names, seen map[string]struct{}, layer profileLayer, entries []fs.DirEntry) {
	for _, entry := range entries {
		name := strings.TrimSuffix(entry.Name(), ".md")
		if !isAvailableProfileCandidate(entry, name, seen) {
			continue
		}
		seen[name] = struct{}{}
		candidate := profileCandidate{source: layerSource(layer, entry.Name()), path: layerProfilePath(layer, entry.Name()), anchor: layer.anchor}
		if _, found, err := readProfileCandidate(candidate); err == nil && found {
			names[name] = struct{}{}
		}
	}
}

func isAvailableProfileCandidate(entry fs.DirEntry, name string, seen map[string]struct{}) bool {
	_, alreadySeen := seen[name]
	return isMarkdownProfile(entry) && validateProfileName(name) == nil && !alreadySeen
}

func listOrNone(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	return strings.Join(values, ", ")
}
