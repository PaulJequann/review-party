package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"unicode/utf8"

	"reviewparty/internal/configuration"
)

type profileLookup struct {
	repository string
	name       string
}

func (library profileLibrary) findProfile(lookup profileLookup) (resolvedProfile, error) {
	authored, err := library.authoredProfileLibrary(configuration.Repository(lookup.repository))
	if err != nil {
		return resolvedProfile{}, err
	}
	entries, err := authored.Candidates(lookup.name)
	if err != nil {
		return resolvedProfile{}, err
	}
	searched := authoredSources(entries)
	for _, entry := range entries {
		instructions, found, err := readAuthoredProfile(authored, entry)
		if err != nil {
			return resolvedProfile{}, err
		}
		if found {
			return resolvedProfileFrom(profileLocationFrom(entry), instructions), nil
		}
	}
	packaged := profileLocation{
		name:   lookup.name,
		path:   "profiles/" + lookup.name + ".md",
		source: "packaged:profiles/" + lookup.name + ".md",
	}
	searched = append(searched, packaged.source)
	instructions, found, err := readPackagedProfile(packaged)
	if err != nil {
		return resolvedProfile{}, err
	}
	if found {
		return resolvedProfileFrom(packaged, instructions), nil
	}
	return resolvedProfile{}, library.missingProfileError(lookup.repository, lookup.name, searched)
}

func authoredSources(entries []configuration.AuthoredEntry) []string {
	sources := make([]string, 0, len(entries))
	for _, entry := range entries {
		sources = append(sources, entry.Source)
	}
	return sources
}

func resolvedProfileFrom(location profileLocation, instructions string) resolvedProfile {
	digest := sha256.Sum256([]byte(instructions))
	return resolvedProfile{
		name:         location.name,
		instructions: instructions,
		source:       location.source,
		path:         location.path,
		digest:       hex.EncodeToString(digest[:]),
	}
}

func (library profileLibrary) list(repository string) ([]ProfileSummary, error) {
	manager := library.manager()
	if _, err := manager.Resolve(configuration.Request{Repository: configuration.Repository(repository)}); err != nil {
		return nil, err
	}
	authored, err := library.authoredProfileLibrary(configuration.Repository(repository))
	if err != nil {
		return nil, err
	}
	entries, err := authored.Entries()
	if err != nil {
		return nil, err
	}
	winners := make(map[string]ProfileSummary)
	if err := addAuthoredProfiles(winners, authored, entries); err != nil {
		return nil, err
	}
	packaged, err := packagedProfileSummaries(winners)
	if err != nil {
		return nil, err
	}
	for name, summary := range packaged {
		if _, exists := winners[name]; !exists {
			winners[name] = summary
		}
	}
	return sortedProfileSummaries(winners), nil
}

func addAuthoredProfiles(winners map[string]ProfileSummary, authored configuration.AuthoredLibrary, entries []configuration.AuthoredEntry) error {
	for _, entry := range entries {
		name := entry.Name
		location := profileLocationFrom(entry)
		if _, exists := winners[name]; exists {
			continue
		}
		if err := validateAuthoredName(name); err != nil {
			winners[name] = invalidProfileSummary(location, err)
			continue
		}
		if _, found, err := readAuthoredProfile(authored, entry); err != nil {
			winners[name] = invalidProfileSummary(location, err)
			continue
		} else if !found {
			continue
		}
		winners[name] = ProfileSummary{Name: location.name, Source: location.source, Path: location.path}
	}
	return nil
}

func packagedProfileSummaries(winners map[string]ProfileSummary) (map[string]ProfileSummary, error) {
	entries, err := packagedProfileEntries()
	if err != nil {
		return nil, err
	}
	summaries := make(map[string]ProfileSummary)
	for _, entry := range entries {
		if summary, found := summarizePackagedProfile(entry, winners); found {
			summaries[summary.Name] = summary
		}
	}
	return summaries, nil
}

func summarizePackagedProfile(entry profileLocation, winners map[string]ProfileSummary) (ProfileSummary, bool) {
	if _, exists := winners[entry.name]; exists {
		return ProfileSummary{}, false
	}
	if err := validateAuthoredName(entry.name); err != nil {
		return invalidProfileSummary(entry, err), true
	}
	if _, found, err := readPackagedProfile(entry); err != nil {
		return invalidProfileSummary(entry, err), true
	} else if found {
		return ProfileSummary{Name: entry.name, Source: entry.source, Path: entry.path}, true
	}
	return ProfileSummary{}, false
}

func invalidProfileSummary(location profileLocation, err error) ProfileSummary {
	return ProfileSummary{Name: location.name, Source: location.source, Path: location.path, Error: err.Error()}
}

func sortedProfileSummaries(winners map[string]ProfileSummary) []ProfileSummary {
	profiles := make([]ProfileSummary, 0, len(winners))
	for _, summary := range winners {
		profiles = append(profiles, summary)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles
}

func readAuthoredProfile(library configuration.AuthoredLibrary, entry configuration.AuthoredEntry) (string, bool, error) {
	payload, found, err := library.ReadEntry(entry)
	location := profileLocationFrom(entry)
	if err != nil {
		return validateProfilePayload(location, nil, err)
	}
	if !found {
		return "", false, nil
	}
	return validateProfilePayload(location, payload, nil)
}

func readPackagedProfile(entry profileLocation) (string, bool, error) {
	payload, err := packagedProfileFiles.ReadFile(entry.path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return validateProfilePayload(entry, nil, err)
	}
	if len(payload) > configuration.MaximumDocumentBytes {
		return "", false, fmt.Errorf("profile %s at %q exceeds %d bytes", entry.source, entry.path, configuration.MaximumDocumentBytes)
	}
	return validateProfilePayload(entry, payload, nil)
}

func validateProfilePayload(location profileLocation, payload []byte, readErr error) (string, bool, error) {
	if readErr != nil {
		return "", false, fmt.Errorf("read profile %s at %q: %w", location.source, location.path, readErr)
	}
	if !utf8.Valid(payload) {
		return "", false, fmt.Errorf("profile %s at %q is not valid UTF-8", location.source, location.path)
	}
	instructions := strings.ReplaceAll(string(payload), "\r\n", "\n")
	instructions = strings.TrimSpace(strings.ReplaceAll(instructions, "\r", "\n"))
	if instructions == "" {
		return "", false, fmt.Errorf("profile %s at %q is empty", location.source, location.path)
	}
	return instructions, true, nil
}
