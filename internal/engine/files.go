package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"reviewparty/internal/configuration"
)

const maximumProfileBytes = 64 * 1024

type profileLayer struct {
	directory string
	anchor    string
	source    string
	packaged  bool
}

type profileLookup struct {
	repository string
	name       string
}

func (library profileLibrary) findProfile(lookup profileLookup) (resolvedProfile, error) {
	candidates := library.profileCandidates(lookup)
	for _, candidate := range candidates {
		instructions, found, err := readProfileCandidate(candidate)
		if err != nil {
			return resolvedProfile{}, err
		}
		if found {
			return resolvedProfileFrom(candidate, lookup.name, instructions), nil
		}
	}
	return resolvedProfile{}, library.missingProfileError(lookup.repository, lookup.name, candidates)
}

func (library profileLibrary) profileCandidates(lookup profileLookup) []profileCandidate {
	filename := lookup.name + ".md"
	var candidates []profileCandidate
	for _, layer := range library.profileLayers(lookup.repository) {
		candidates = append(candidates, profileCandidate{
			source: layerSource(layer, filename),
			path:   layerProfilePath(layer, filename),
			anchor: layer.anchor,
		})
	}
	return candidates
}

func (library profileLibrary) profileLayers(repository string) []profileLayer {
	var layers []profileLayer
	if repository != "" {
		layers = append(layers, profileLayer{directory: filepath.Join(repository, ".reviewparty", "profiles"), anchor: repository, source: "repository"})
	}
	if directory, err := library.manager().ProfilesDirectory(configuration.ScopePersonal, configuration.Repository("")); err == nil {
		layers = append(layers, profileLayer{directory: directory, anchor: filepath.Dir(filepath.Dir(directory)), source: "personal"})
	}
	return append(layers, profileLayer{directory: "profiles", source: "packaged", packaged: true})
}

func resolvedProfileFrom(candidate profileCandidate, name, instructions string) resolvedProfile {
	digest := sha256.Sum256([]byte(instructions))
	return resolvedProfile{
		name:         name,
		instructions: instructions,
		source:       candidate.source,
		path:         candidate.path,
		digest:       hex.EncodeToString(digest[:]),
	}
}

func isMarkdownProfile(entry fs.DirEntry) bool {
	return !entry.IsDir() && filepath.Ext(entry.Name()) == ".md"
}

func (library profileLibrary) list(repository string) ([]ProfileSummary, error) {
	if _, err := library.manager().Load(configuration.Repository(repository)); err != nil {
		return nil, err
	}
	winners := make(map[string]ProfileSummary)
	for _, layer := range library.profileLayers(repository) {
		if err := addLayerProfiles(winners, layer); err != nil {
			return nil, err
		}
	}
	return sortedProfileSummaries(winners), nil
}

func addLayerProfiles(winners map[string]ProfileSummary, layer profileLayer) error {
	entries, err := readProfileDirectory(layer)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !isMarkdownProfile(entry) {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		if _, exists := winners[name]; exists {
			continue
		}
		candidate := profileCandidate{source: layerSource(layer, entry.Name()), path: layerProfilePath(layer, entry.Name()), anchor: layer.anchor}
		if err := validateProfileName(name); err != nil {
			winners[name] = invalidProfileSummary(name, candidate, err)
			continue
		}
		if _, _, err := readProfileCandidate(candidate); err != nil {
			winners[name] = invalidProfileSummary(name, candidate, err)
			continue
		}
		winners[name] = ProfileSummary{Name: name, Source: candidate.source, Path: candidate.path}
	}
	return nil
}

func invalidProfileSummary(name string, candidate profileCandidate, err error) ProfileSummary {
	return ProfileSummary{Name: name, Source: candidate.source, Path: candidate.path, Error: err.Error()}
}

func layerProfilePath(layer profileLayer, filename string) string {
	if layer.packaged {
		return path.Join(layer.directory, filename)
	}
	return filepath.Join(layer.directory, filename)
}

func sortedProfileSummaries(winners map[string]ProfileSummary) []ProfileSummary {
	profiles := make([]ProfileSummary, 0, len(winners))
	for _, summary := range winners {
		profiles = append(profiles, summary)
	}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].Name < profiles[j].Name })
	return profiles
}

func layerSource(layer profileLayer, filename string) string {
	if layer.source == "repository" {
		return "repository:.reviewparty/profiles/" + filename
	}
	if layer.source == "personal" {
		return "personal:profiles/" + filename
	}
	return layer.source + ":profiles/" + filename
}

func readProfileCandidate(candidate profileCandidate) (string, bool, error) {
	if strings.HasPrefix(candidate.source, "packaged:") {
		payload, err := packagedProfileFiles.ReadFile(candidate.path)
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return validateProfilePayload(candidate, payload, err)
	}
	payload, found, err := readLocalProfile(candidate)
	if err != nil || !found {
		return "", found, err
	}
	return validateProfilePayload(candidate, payload, nil)
}

func readLocalProfile(candidate profileCandidate) ([]byte, bool, error) {
	description := fmt.Sprintf("profile %s", candidate.source)
	return readLocalRegularFile(candidate.anchor, candidate.path, description, maximumProfileBytes)
}

func validateProfilePayload(candidate profileCandidate, payload []byte, readErr error) (string, bool, error) {
	if readErr != nil {
		return "", false, fmt.Errorf("read profile %s at %q: %w", candidate.source, candidate.path, readErr)
	}
	if len(payload) > maximumProfileBytes {
		return "", false, fmt.Errorf("profile %s at %q exceeds %d bytes", candidate.source, candidate.path, maximumProfileBytes)
	}
	if !utf8.Valid(payload) {
		return "", false, fmt.Errorf("profile %s at %q is not valid UTF-8", candidate.source, candidate.path)
	}
	instructions := strings.ReplaceAll(string(payload), "\r\n", "\n")
	instructions = strings.TrimSpace(strings.ReplaceAll(instructions, "\r", "\n"))
	if instructions == "" {
		return "", false, fmt.Errorf("profile %s at %q is empty", candidate.source, candidate.path)
	}
	return instructions, true, nil
}

func readProfileDirectory(layer profileLayer) ([]fs.DirEntry, error) {
	var entries []fs.DirEntry
	var err error
	if layer.packaged {
		entries, err = packagedProfileFiles.ReadDir(layer.directory)
	} else {
		entries, err = os.ReadDir(layer.directory)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read profile directory %q: %w", layer.directory, err)
	}
	return entries, nil
}
