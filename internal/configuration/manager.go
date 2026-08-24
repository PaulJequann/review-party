// Package configuration owns Review Party's Personal and Repository
// Configuration: one load returns authored documents and effective values with
// exact provenance, typed intents are staged into validated Plans, and
// confirmed Plans publish atomically. Callers never inspect raw
// configuration maps or implement precedence rules themselves.
package configuration

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Scope identifies the ownership context of a configuration document or value.
type Scope string

const (
	// ScopePersonal is the Caller-owned configuration that applies across repositories.
	ScopePersonal Scope = "personal"
	// ScopeRepository is the team-owned configuration under <repository>/.reviewparty.
	ScopeRepository Scope = "repository"
)

// Source identifies where an effective value originated.
type Source string

const (
	SourcePackaged   Source = "packaged"
	SourcePersonal   Source = "personal"
	SourceRepository Source = "repository"
	SourceExplicit   Source = "explicit"
)

// MaximumDocumentBytes bounds one authored configuration document or Profile.
const MaximumDocumentBytes = 64 * 1024

const maximumPartyBytes = 16 * 1024

// Repository identifies the repository whose team-owned configuration should
// participate in a load or publication. The zero value means no repository.
type Repository string

// Options configures a Manager. The zero value uses the canonical XDG
// personal root and no known reviewers; most callers supply the packaged
// reviewer identifiers so validation can reject unknown references.
type Options struct {
	// PersonalRoot overrides the canonical personal configuration directory.
	PersonalRoot string
	// PersonalConfigPath overrides the personal config.json file name while
	// keeping the rest of the layout beneath PersonalRoot.
	PersonalConfigPath string
	// Reviewers lists the packaged reviewer identifiers that documents may reference.
	Reviewers []string
	// PackagedReviewerModels supplies each reviewer's packaged model when one exists.
	PackagedReviewerModels map[string]string
	// PackagedDefaultReviewer is the effective default reviewer when nothing is authored.
	PackagedDefaultReviewer string
	// PackagedDefaultProfile is the effective default profile when nothing is authored.
	PackagedDefaultProfile string
	// PackagedDefaultParty is the effective default Party when nothing is authored.
	PackagedDefaultParty string
	// ValidateName validates authored Profile and Party references; nil skips the check.
	ValidateName func(string) error
}

// Manager owns Personal and Repository Configuration paths, resolution,
// staged change plans, and atomic publication.
type Manager struct {
	personalRoot       string
	personalConfigPath string
	reviewers          map[string]string
	packaged           packagedDefaults
	nameValidator      func(string) error
	publishWrite       func(*pendingWrite) error
}

type packagedDefaults struct {
	defaultReviewer string
	defaultProfile  string
	defaultParty    string
}

// NewManager constructs a Manager. The personal root may be empty when the
// platform provides no configuration home; operations that need it fail with
// a clear error instead of guessing a location.
func NewManager(options Options) *Manager {
	if options.PersonalRoot == "" {
		if options.PersonalConfigPath != "" {
			options.PersonalRoot = filepath.Dir(options.PersonalConfigPath)
		} else {
			options.PersonalRoot = DefaultPersonalRoot()
		}
	}
	reviewers := make(map[string]string, len(options.Reviewers))
	for _, id := range options.Reviewers {
		reviewers[id] = options.PackagedReviewerModels[id]
	}
	return &Manager{
		personalRoot:       options.PersonalRoot,
		personalConfigPath: options.PersonalConfigPath,
		reviewers:          reviewers,
		packaged: packagedDefaults{
			defaultReviewer: options.PackagedDefaultReviewer,
			defaultProfile:  options.PackagedDefaultProfile,
			defaultParty:    options.PackagedDefaultParty,
		},
		nameValidator: options.ValidateName,
		publishWrite:  writeAtomically,
	}
}

// DefaultPersonalRoot returns the canonical personal configuration directory:
// ${XDG_CONFIG_HOME:-$HOME/.config}/review-party. It is empty when the
// platform provides no home directory.
func DefaultPersonalRoot() string {
	directory, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(directory, "review-party")
}

// ErrPersonalRootUnavailable reports that the platform provides no personal
// configuration home. Reads treat it as absent Personal Configuration;
// writes and library roots surface it as an error.
var ErrPersonalRootUnavailable = errors.New("personal configuration home is unavailable; set XDG_CONFIG_HOME or HOME")

// PersonalRoot returns the personal configuration directory.
func (manager *Manager) PersonalRoot() (string, error) {
	if manager.personalRoot == "" {
		return "", ErrPersonalRootUnavailable
	}
	return manager.personalRoot, nil
}

func (manager *Manager) scopeDirectory(scope Scope, repository Repository) (directory, anchor string, err error) {
	switch scope {
	case ScopePersonal:
		root, err := manager.PersonalRoot()
		if err != nil {
			return "", "", err
		}
		return root, filepath.Dir(root), nil
	case ScopeRepository:
		if repository == "" {
			return "", "", fmt.Errorf("repository configuration requires a repository")
		}
		directory := filepath.Join(string(repository), ".reviewparty")
		return directory, string(repository), nil
	default:
		return "", "", fmt.Errorf("unknown configuration scope %q", scope)
	}
}

// ConfigPath returns the config.json path for one scope.
func (manager *Manager) ConfigPath(scope Scope, repository Repository) (string, error) {
	path, _, err := manager.configPathAndAnchor(scope, repository)
	return path, err
}

func (manager *Manager) configPathAndAnchor(scope Scope, repository Repository) (path, anchor string, err error) {
	directory, anchor, err := manager.scopeDirectory(scope, repository)
	if err != nil {
		return "", "", err
	}
	if scope == ScopePersonal && manager.personalConfigPath != "" {
		return manager.personalConfigPath, filepath.Dir(manager.personalConfigPath), nil
	}
	return filepath.Join(directory, "config.json"), anchor, nil
}

func (manager *Manager) knownReviewers() []string {
	ids := make([]string, 0, len(manager.reviewers))
	for id := range manager.reviewers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (manager *Manager) validateReviewer(id string) error {
	if _, known := manager.reviewers[id]; !known {
		return fmt.Errorf("unknown reviewer %q", id)
	}
	return nil
}

func (manager *Manager) validateName(name string) error {
	if manager.nameValidator == nil {
		return nil
	}
	return manager.nameValidator(name)
}
