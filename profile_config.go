package reviewparty

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const maximumConfigBytes = 64 * 1024

type profileConfig struct {
	Schema          int    `json:"schema"`
	DefaultProfile  string `json:"defaultProfile"`
	DefaultReviewer string `json:"defaultReviewer"`
}

type profileSettings struct {
	global     profileConfig
	repository profileConfig
}

type profileSelection struct {
	name     string
	reviewer string
}

type namedProfileValue struct {
	field string
	value string
}

type profileConfigLocation struct {
	directory string
	scope     string
}

func (library profileLibrary) loadSettings(repository string) (profileSettings, error) {
	global, err := loadOptionalProfileConfig(profileConfigLocation{directory: library.globalDirectory, scope: "global"})
	if err != nil {
		return profileSettings{}, err
	}
	repositoryDirectory := ""
	if repository != "" {
		repositoryDirectory = filepath.Join(repository, ".reviewparty")
	}
	local, err := loadOptionalProfileConfig(profileConfigLocation{directory: repositoryDirectory, scope: "repository"})
	if err != nil {
		return profileSettings{}, err
	}
	return profileSettings{global: global, repository: local}, nil
}

func (settings profileSettings) selectProfile(catalog reviewerCatalog, request profileSelection) (profileSelection, error) {
	name := firstNonempty(request.name, settings.repository.DefaultProfile, settings.global.DefaultProfile, "bugs")
	if err := validateProfileName(name); err != nil {
		return profileSelection{}, fmt.Errorf("profile name %q: %w", name, err)
	}
	reviewer := firstNonempty(request.reviewer, settings.repository.DefaultReviewer, settings.global.DefaultReviewer, defaultReviewer)
	if _, err := catalog.resolve(reviewer); err != nil {
		return profileSelection{}, err
	}
	return profileSelection{name: name, reviewer: reviewer}, nil
}

func loadOptionalProfileConfig(location profileConfigLocation) (profileConfig, error) {
	if location.directory == "" {
		return profileConfig{}, nil
	}
	path := filepath.Join(location.directory, "config.json")
	payload, found, err := readLocalRegularFile(filepath.Dir(location.directory), path, location.scope+" profile config", maximumConfigBytes)
	if err != nil {
		return profileConfig{}, err
	}
	if !found {
		return profileConfig{}, nil
	}
	return parseProfileConfig(payload, path, location)
}

func parseProfileConfig(payload []byte, path string, location profileConfigLocation) (profileConfig, error) {
	if len(payload) > maximumConfigBytes {
		return profileConfig{}, fmt.Errorf("%s profile config %q exceeds %d bytes", location.scope, path, maximumConfigBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var config profileConfig
	if err := decoder.Decode(&config); err != nil {
		return profileConfig{}, profileConfigError(location, path, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return profileConfig{}, profileConfigError(location, path, err)
	}
	if err := validateProfileConfig(config); err != nil {
		return profileConfig{}, profileConfigError(location, path, err)
	}
	return config, nil
}

func validateProfileConfig(config profileConfig) error {
	if config.Schema != profileConfigSchema {
		return fmt.Errorf("unsupported schema %d; expected %d", config.Schema, profileConfigSchema)
	}
	if err := validateOptionalProfileName(namedProfileValue{field: "defaultProfile", value: config.DefaultProfile}); err != nil {
		return err
	}
	return validateOptionalProfileName(namedProfileValue{field: "defaultReviewer", value: config.DefaultReviewer})
}

func validateOptionalProfileName(named namedProfileValue) error {
	if named.value == "" {
		return nil
	}
	if err := validateProfileName(named.value); err != nil {
		return fmt.Errorf("%s: %w", named.field, err)
	}
	return nil
}

func profileConfigError(location profileConfigLocation, path string, err error) error {
	return fmt.Errorf("parse %s profile config %q: %w", location.scope, path, err)
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func validateProfileName(name string) error {
	if len(name) == 0 || len(name) > 64 {
		return errors.New("must contain 1 to 64 characters")
	}
	for index, character := range name {
		if !validProfileNameCharacter(character, index) {
			return errors.New("must match [a-z0-9][a-z0-9-]*")
		}
	}
	return nil
}

func validProfileNameCharacter(character rune, index int) bool {
	if character >= 'a' && character <= 'z' {
		return true
	}
	if character >= '0' && character <= '9' {
		return true
	}
	return character == '-' && index > 0
}

func defaultGlobalProfileDirectory() string {
	if configured := os.Getenv("REVIEW_PARTY_HOME"); configured != "" {
		return configured
	}
	home, err := os.UserHomeDir()
	if err == nil {
		return filepath.Join(home, ".reviewparty")
	}
	return ""
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
