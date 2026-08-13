package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"reviewparty/internal/store"
)

type ReviewPartyInitialization struct {
	Repository              string
	StateDirectory          string
	UserConfigurationPath   string
	UseDefaultConfiguration bool
}

type ReviewPartyInitializationResult struct {
	Repository     string
	StateDirectory string
	AdvancedState  bool
	AlreadyReady   bool
}

func InitializeReviewParty(request ReviewPartyInitialization) (ReviewPartyInitializationResult, error) {
	resolved, err := resolveInitialization(request)
	if err != nil {
		return ReviewPartyInitializationResult{}, err
	}
	alreadyReady, err := prepareInitializationState(request.UserConfigurationPath, resolved.selection)
	if err != nil {
		return ReviewPartyInitializationResult{}, err
	}
	return ReviewPartyInitializationResult{
		Repository:     resolved.repository,
		StateDirectory: resolved.selection.directory,
		AdvancedState:  resolved.selection.advanced,
		AlreadyReady:   alreadyReady,
	}, nil
}

type resolvedInitialization struct {
	repository string
	selection  initializationStateSelection
}

func resolveInitialization(request ReviewPartyInitialization) (resolvedInitialization, error) {
	repository, err := resolveRepositoryRoot(request.Repository)
	if err != nil {
		return resolvedInitialization{}, err
	}
	if err := validateConfigurationPath(request.UserConfigurationPath); err != nil {
		return resolvedInitialization{}, err
	}
	configuration, err := loadUserConfiguration(request.UserConfigurationPath)
	if err != nil {
		return resolvedInitialization{}, err
	}
	selection, err := selectInitializationState(request, configuration)
	if err != nil {
		return resolvedInitialization{}, err
	}
	return resolvedInitialization{repository: repository, selection: selection}, nil
}

func validateConfigurationPath(path string) error {
	if path == "" {
		return nil
	}
	return rejectSpecialConfigurationPath(path)
}

func prepareInitializationState(configurationPath string, selection initializationStateSelection) (bool, error) {
	alreadyReady, err := store.ReviewRecordStatePrepared(selection.directory)
	if err != nil && !errors.Is(err, store.ErrReviewRecordStateRequiresPreparation) {
		return false, err
	}
	if errors.Is(err, store.ErrReviewRecordStateRequiresPreparation) {
		alreadyReady = false
	}
	if err := store.PrepareReviewRecordState(selection.directory); err != nil {
		return false, err
	}
	if selection.remember {
		if err := persistStateDirectory(configurationPath, selection.directory); err != nil {
			return false, err
		}
	}
	return alreadyReady, nil
}

type initializationStateSelection struct {
	directory string
	advanced  bool
	remember  bool
}

func selectInitializationState(request ReviewPartyInitialization, configuration userConfiguration) (initializationStateSelection, error) {
	explicit, err := absoluteOptionalPath(request.StateDirectory)
	if err != nil {
		return initializationStateSelection{}, err
	}
	if configuration.StateDirectory != "" {
		return selectConfiguredState(configuration.StateDirectory, explicit)
	}
	defaultDirectory := defaultStateDirectory()
	if explicit == "" || explicit == defaultDirectory {
		return initializationStateSelection{directory: defaultDirectory}, nil
	}
	return selectAdvancedState(request, defaultDirectory, explicit)
}

func selectConfiguredState(configured, explicit string) (initializationStateSelection, error) {
	if explicit != "" && explicit != configured {
		return initializationStateSelection{}, fmt.Errorf("Review Party is already configured to use state at %q; refusing to switch to %q", configured, explicit)
	}
	return initializationStateSelection{directory: configured, advanced: true}, nil
}

func selectAdvancedState(request ReviewPartyInitialization, defaultDirectory, explicit string) (initializationStateSelection, error) {
	if request.UserConfigurationPath == "" {
		return initializationStateSelection{}, errors.New("remember advanced state location: user configuration path is unavailable")
	}
	if !request.UseDefaultConfiguration {
		return initializationStateSelection{directory: explicit, advanced: true, remember: true}, nil
	}
	defaultReady, err := store.ReviewRecordStatePrepared(defaultDirectory)
	if err != nil {
		return initializationStateSelection{}, err
	}
	if defaultReady {
		return initializationStateSelection{}, fmt.Errorf("Review Party is already initialized with managed state at %q; refusing to switch to %q", defaultDirectory, explicit)
	}
	return initializationStateSelection{directory: explicit, advanced: true, remember: true}, nil
}

func absoluteOptionalPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve state location %q: %w", path, err)
	}
	return filepath.Clean(absolute), nil
}

func persistStateDirectory(configurationPath, stateDirectory string) error {
	fields, err := readConfigurationFields(configurationPath)
	if err != nil {
		return err
	}
	encodedState, err := json.Marshal(stateDirectory)
	if err != nil {
		return err
	}
	fields["state_directory"] = encodedState
	payload, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		return fmt.Errorf("encode user configuration: %w", err)
	}
	payload = append(payload, '\n')
	return writeUserConfiguration(configurationPath, payload)
}

func readConfigurationFields(path string) (map[string]json.RawMessage, error) {
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]json.RawMessage{"version": json.RawMessage("1")}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read user configuration %q: %w", path, err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return nil, fmt.Errorf("decode user configuration %q: %w", path, err)
	}
	return fields, nil
}

func writeUserConfiguration(path string, payload []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create user configuration directory %q: %w", directory, err)
	}
	if err := rejectSpecialConfigurationPath(path); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary user configuration: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := writeConfigurationPayload(temporary, payload); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish user configuration %q: %w", path, err)
	}
	return syncDirectory(directory)
}

func rejectSpecialConfigurationPath(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect user configuration %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("user configuration %q must be a regular file, not a symlink or special file", path)
	}
	return nil
}

func writeConfigurationPayload(file *os.File, payload []byte) error {
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("restrict temporary user configuration: %w", err)
	}
	if _, err := file.Write(payload); err != nil {
		file.Close()
		return fmt.Errorf("write temporary user configuration: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync temporary user configuration: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary user configuration: %w", err)
	}
	return nil
}

func syncDirectory(directory string) error {
	opened, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open user configuration directory for sync: %w", err)
	}
	defer opened.Close()
	if err := opened.Sync(); err != nil {
		return fmt.Errorf("sync user configuration directory: %w", err)
	}
	return nil
}
