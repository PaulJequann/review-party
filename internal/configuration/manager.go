// Package configuration owns Review Party's Global and Repository
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
	// ScopeGlobal is the Caller-owned configuration that applies across repositories.
	ScopeGlobal Scope = "global"
	// ScopeRepository is the team-owned configuration under <repository>/.reviewparty.
	ScopeRepository Scope = "repository"
)

// Source identifies where an effective value originated.
type Source string

const (
	SourcePackaged   Source = "packaged"
	SourceGlobal     Source = "global"
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
// global root and no known reviewers; most callers supply the packaged
// reviewer identifiers so validation can reject unknown references.
type Options struct {
	// GlobalRoot overrides the canonical global configuration directory.
	GlobalRoot string
	// GlobalConfigPath overrides the global config.json file name while
	// keeping the rest of the layout beneath GlobalRoot.
	GlobalConfigPath string
	// Reviewers lists the packaged reviewer identifiers that documents may reference.
	Reviewers []string
	// PackagedReviewerModels supplies each reviewer's packaged model when one exists.
	PackagedReviewerModels map[string]string
	// PackagedDefaultReviewer is the effective default reviewer when nothing is authored.
	PackagedDefaultReviewer string
	Templates               []Template
	SkippedTemplates        []SkippedTemplate
	// ValidateName validates authored Profile and Party references; nil skips the check.
	ValidateName func(string) error
}

// RuntimeResolver resolves one executable request into a stable runtime view.
// The Manager is the package's implementation; the interface keeps callers
// dependent on the typed runtime contract rather than its storage details.
type RuntimeResolver interface {
	ResolveRuntime(RunRequest) (RuntimeSnapshot, error)
}

// RuntimeSnapshot is the immutable configuration view used while planning one
// run. It contains the resolved selection, effective reviewer policy, and the
// exact executable Profile material selected for that run.
type RuntimeSnapshot interface {
	Selection() ResolvedReviews
	Effective() Effective
	ProfileFor(ExpandedProfile) (Profile, bool)
}

// Manager owns Global and Repository Configuration paths, resolution,
// staged change plans, and atomic publication.
type Manager struct {
	globalRoot       string
	globalConfigPath string
	reviewers        map[string]string
	packaged         packagedDefaults
	nameValidator    func(string) error
	templates        []Template
	skippedTemplates []SkippedTemplate
	publication      *publicationModule
}

var _ RuntimeResolver = (*Manager)(nil)

type packagedDefaults struct {
	defaultReviewer string
}

// NewManager constructs a Manager. The global root may be empty when the
// platform provides no configuration home; operations that need it fail with
// a clear error instead of guessing a location.
func NewManager(options Options) *Manager {
	if options.GlobalRoot == "" {
		if options.GlobalConfigPath != "" {
			options.GlobalRoot = filepath.Dir(options.GlobalConfigPath)
		} else {
			options.GlobalRoot = DefaultGlobalRoot()
		}
	}
	reviewers := make(map[string]string, len(options.Reviewers))
	for _, id := range options.Reviewers {
		reviewers[id] = options.PackagedReviewerModels[id]
	}
	return &Manager{
		globalRoot:       options.GlobalRoot,
		globalConfigPath: options.GlobalConfigPath,
		reviewers:        reviewers,
		packaged:         packagedDefaults{defaultReviewer: options.PackagedDefaultReviewer},
		nameValidator:    options.ValidateName,
		templates:        append([]Template(nil), options.Templates...),
		skippedTemplates: append([]SkippedTemplate(nil), options.SkippedTemplates...),
		publication:      newPublicationModule(),
	}
}

// PackagedReviewerModel returns the packaged model choice for one Reviewer.
// An empty result means the packaged catalog has no model for that Reviewer.
func (manager *Manager) PackagedReviewerModel(reviewer string) string {
	if manager == nil {
		return ""
	}
	return manager.reviewers[reviewer]
}

// DefaultGlobalRoot returns the canonical global configuration directory:
// ${XDG_CONFIG_HOME:-$HOME/.config}/review-party. It is empty when the
// platform provides no home directory.
func DefaultGlobalRoot() string {
	directory, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(directory, "review-party")
}

// ErrGlobalRootUnavailable reports that the platform provides no global
// configuration home. Reads treat it as absent Global Configuration;
// writes and library roots surface it as an error.
var ErrGlobalRootUnavailable = errors.New("global configuration home is unavailable; set XDG_CONFIG_HOME or HOME")

// GlobalRoot returns the global configuration directory.
func (manager *Manager) GlobalRoot() (string, error) {
	if manager.globalRoot == "" {
		return "", ErrGlobalRootUnavailable
	}
	return manager.globalRoot, nil
}

func (manager *Manager) scopeDirectory(scope Scope, repository Repository) (directory, anchor string, err error) {
	switch scope {
	case ScopeGlobal:
		root, err := manager.GlobalRoot()
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
	if scope == ScopeGlobal && manager.globalConfigPath != "" {
		return manager.globalConfigPath, filepath.Dir(manager.globalConfigPath), nil
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
