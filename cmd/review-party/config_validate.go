package main

import (
	"errors"
	"fmt"
	"strings"

	"reviewparty/internal/configuration"
)

type configurationValidationOptions struct {
	scope         string
	repository    string
	format        string
	configuration string
}

type configurationValidationResult struct {
	Valid  bool                      `json:"valid"`
	Scopes []string                  `json:"scopes"`
	Files  []configurationFileStatus `json:"files"`
	Error  string                    `json:"error,omitempty"`
}

func executeConfigurationValidate(options configurationValidationOptions, streams commandIO) int {
	return runConfigurationCommand(options.format, options.configuration, streams, func(manager *configuration.Manager) (int, error) {
		result, err := validateConfiguration(manager, options.scope, configuration.Repository(options.repository))
		if err != nil {
			return printValidationFailure(result, err, options, streams), nil
		}
		return printValidationSuccess(result, options, streams), nil
	})
}

func printValidationFailure(result configurationValidationResult, validationErr error, options configurationValidationOptions, streams commandIO) int {
	if options.format != "json" {
		return printConfigFailure(options.format, streams.output, streams.errors, validationErr)
	}
	result.Error = validationErr.Error()
	if err := writeJSON(streams.output, result); err != nil {
		return printConfigFailure(options.format, streams.output, streams.errors, err)
	}
	return 1
}

func printValidationSuccess(result configurationValidationResult, options configurationValidationOptions, streams commandIO) int {
	result.Valid = true
	if options.format == "json" {
		if err := writeJSON(streams.output, result); err != nil {
			return printConfigFailure(options.format, streams.output, streams.errors, err)
		}
		return 0
	}
	fmt.Fprintf(streams.output, "valid configuration: %s\n", strings.Join(result.Scopes, ", "))
	for _, file := range result.Files {
		fmt.Fprintf(streams.output, "  %s: %s (%s)\n", file.Scope, file.Path, validationFileStatus(file))
	}
	return 0
}

func validationFileStatus(file configurationFileStatus) string {
	if file.Present {
		return "present"
	}
	return "absent"
}

func validateConfiguration(manager *configuration.Manager, requested string, repository configuration.Repository) (configurationValidationResult, error) {
	scopes, err := validationScopes(requested)
	if err != nil {
		return configurationValidationResult{}, err
	}
	result, loaded, err := loadValidationFiles(manager, scopes, repository)
	if err != nil {
		return result, err
	}
	definitionRepository := repository
	if !containsScope(scopes, configuration.ScopeRepository) {
		definitionRepository = ""
	}
	if err := validateAuthoredDefinitions(manager, definitionRepository, scopes); err != nil {
		return result, err
	}
	if err := validateEffectiveConfiguration(manager, repository, scopes, loaded); err != nil {
		return result, err
	}
	return result, nil
}

func validateEffectiveConfiguration(manager *configuration.Manager, repository configuration.Repository, scopes []configuration.Scope, inspection configuration.AuthoredInspection) error {
	if _, err := manager.ResolveAuthored(configuration.Request{Repository: repository}, inspection); err != nil {
		return err
	}
	if !containsScope(scopes, configuration.ScopeRepository) {
		return nil
	}
	var err error
	_, err = manager.ResolveRunAuthored(configuration.RunRequest{Repository: repository}, inspection)
	if errors.Is(err, configuration.ErrNoRepositorySelection) {
		return nil
	}
	return err
}

func loadValidationFiles(manager *configuration.Manager, scopes []configuration.Scope, repository configuration.Repository) (configurationValidationResult, configuration.AuthoredInspection, error) {
	result := configurationValidationResult{
		Scopes: make([]string, 0, len(scopes)), Files: make([]configurationFileStatus, 0, len(scopes)),
	}
	inspectionScopes := scopes
	if containsScope(scopes, configuration.ScopeRepository) && !containsScope(scopes, configuration.ScopeGlobal) {
		inspectionScopes = []configuration.Scope{configuration.ScopeGlobal, configuration.ScopeRepository}
	}
	inspection, err := manager.InspectAuthored(repository, inspectionScopes)
	for _, scope := range scopes {
		file, found := inspection.File(scope)
		if !found {
			break
		}
		result.Scopes = append(result.Scopes, string(scope))
		result.Files = append(result.Files, configurationFileStatus{Scope: string(scope), Path: file.Path, Present: file.Present})
	}
	return result, inspection, err
}

func validateAuthoredDefinitions(manager *configuration.Manager, repository configuration.Repository, scopes []configuration.Scope) error {
	if err := validateInventory(repository, scopes, manager.ProfileInventory, "Profile"); err != nil {
		return err
	}
	return validateInventory(repository, scopes, manager.PartyInventory, "Party")
}

func validateInventory[T any](repository configuration.Repository, scopes []configuration.Scope, inventory func(configuration.Repository) ([]configuration.Definition[T], error), kind string) error {
	definitions, err := inventory(repository)
	if err != nil {
		return err
	}
	for _, definition := range definitions {
		if containsScope(scopes, definition.Scope) && definition.Err != nil {
			return fmt.Errorf("invalid %s %s %q: %w", definition.Scope, kind, definition.Name, definition.Err)
		}
	}
	return nil
}

func validationScopes(requested string) ([]configuration.Scope, error) {
	if requested == "" {
		return []configuration.Scope{configuration.ScopeGlobal, configuration.ScopeRepository}, nil
	}
	scope, err := parseConfigurationScope(requested)
	if err != nil {
		return nil, err
	}
	return []configuration.Scope{scope}, nil
}

func containsScope(scopes []configuration.Scope, wanted configuration.Scope) bool {
	for _, scope := range scopes {
		if scope == wanted {
			return true
		}
	}
	return false
}
