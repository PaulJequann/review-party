package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reviewparty/internal/subject"

	"reviewparty/internal/artifact"
	"reviewparty/internal/configuration"
	"reviewparty/internal/store"
)

type resolvedInitialization struct {
	repository string
	selection  initializationStateSelection
	manager    *configuration.Manager
}

func resolveInitialization(request ReviewPartyInitialization) (resolvedInitialization, error) {
	repository, err := subject.ResolveRepositoryRoot(request.Repository)
	if err != nil {
		return resolvedInitialization{}, err
	}
	if err := validateConfigurationPath(request.UserConfigurationPath); err != nil {
		return resolvedInitialization{}, err
	}
	manager := newConfigurationManager(request.UserConfigurationPath)
	configuredState, err := manager.ResolveStateDirectory()
	if err != nil {
		return resolvedInitialization{}, err
	}
	selection, err := selectInitializationState(request, statePath(configuredState.Value))
	if err != nil {
		return resolvedInitialization{}, err
	}
	return resolvedInitialization{repository: repository, selection: selection, manager: manager}, nil
}

func validateConfigurationPath(path string) error {
	if path == "" {
		return nil
	}
	return rejectSpecialConfigurationPath(path)
}

func prepareInitializationState(manager *configuration.Manager, selection initializationStateSelection, fresh bool) (bool, error) {
	if err := refuseUnconfirmedRecovery(selection.directory, fresh); err != nil {
		return false, err
	}
	if fresh {
		return false, prepareFreshInitializationState(manager, selection)
	}
	return prepareExistingInitializationState(manager, selection)
}

func refuseUnconfirmedRecovery(directory statePath, fresh bool) error {
	pending, err := store.ReviewRecordStateRecoveryPending(string(directory))
	if err != nil {
		return err
	}
	if pending && !fresh {
		return errors.New("incompatible state was backed up; run init --fresh with a separate confirmation")
	}
	return nil
}

func prepareFreshInitializationState(manager *configuration.Manager, selection initializationStateSelection) error {
	if err := store.PrepareFreshReviewRecordState(string(selection.directory)); err != nil {
		return err
	}
	return rememberInitializedState(manager, selection)
}

func prepareExistingInitializationState(manager *configuration.Manager, selection initializationStateSelection) (bool, error) {
	alreadyReady, err := store.ReviewRecordStatePrepared(string(selection.directory))
	if err != nil && !errors.Is(err, store.ErrReviewRecordStateRequiresPreparation) {
		return false, err
	}
	if errors.Is(err, store.ErrReviewRecordStateRequiresPreparation) {
		alreadyReady = false
	}
	if err := store.PrepareReviewRecordState(string(selection.directory)); err != nil {
		return false, err
	}
	if !alreadyReady {
		if err := trimEvidence(selection.directory); err != nil {
			return false, err
		}
	}
	if err := rememberInitializedState(manager, selection); err != nil {
		return false, err
	}
	return alreadyReady, nil
}

func trimEvidence(directory statePath) (returnErr error) {
	ledger, err := store.NewLedgerRecordStore(string(directory))
	if err != nil {
		return err
	}
	defer func() {
		returnErr = errors.Join(returnErr, ledger.Close())
	}()
	if _, err := ledger.ExpireEvidence(retainedFailureEvidence); err != nil {
		return err
	}
	referenced, err := ledger.EvidencePaths()
	if err != nil {
		return err
	}
	artifacts, err := artifact.NewStore(string(directory))
	if err != nil {
		return err
	}
	return artifacts.RemoveUnreferenced(referenced)
}

func rememberInitializedState(manager *configuration.Manager, selection initializationStateSelection) error {
	if !selection.remember {
		return nil
	}
	return rememberStateDirectory(manager, string(selection.directory))
}

type initializationStateSelection struct {
	directory statePath
	advanced  bool
	remember  bool
}

type statePath string

func selectInitializationState(request ReviewPartyInitialization, configuredStateDirectory statePath) (initializationStateSelection, error) {
	explicit, err := absoluteOptionalPath(request.StateDirectory)
	if err != nil {
		return initializationStateSelection{}, err
	}
	if configuredStateDirectory != "" {
		return selectConfiguredState(configuredStateDirectory, explicit)
	}
	defaultDirectory, err := defaultStateDirectory()
	if err != nil {
		return initializationStateSelection{}, err
	}
	if explicit == "" || explicit == defaultDirectory {
		return initializationStateSelection{directory: defaultDirectory}, nil
	}
	return selectAdvancedState(request, defaultDirectory, explicit)
}

func selectConfiguredState(configured, explicit statePath) (initializationStateSelection, error) {
	if explicit != "" && explicit != configured {
		return initializationStateSelection{}, fmt.Errorf("Review Party is already configured to use state at %q; refusing to switch to %q", configured, explicit)
	}
	return initializationStateSelection{directory: configured, advanced: true}, nil
}

func selectAdvancedState(request ReviewPartyInitialization, defaultDirectory, explicit statePath) (initializationStateSelection, error) {
	if request.UserConfigurationPath == "" {
		return initializationStateSelection{}, errors.New("remember advanced state location: user configuration path is unavailable")
	}
	if !request.UseDefaultConfiguration {
		return initializationStateSelection{directory: explicit, advanced: true, remember: true}, nil
	}
	defaultReady, err := store.ReviewRecordStatePrepared(string(defaultDirectory))
	if err != nil {
		return initializationStateSelection{}, err
	}
	if defaultReady {
		return initializationStateSelection{}, fmt.Errorf("Review Party is already initialized with managed state at %q; refusing to switch to %q", defaultDirectory, explicit)
	}
	return initializationStateSelection{directory: explicit, advanced: true, remember: true}, nil
}

func absoluteOptionalPath(path string) (statePath, error) {
	if path == "" {
		return "", nil
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve state location %q: %w", path, err)
	}
	return statePath(filepath.Clean(absolute)), nil
}

// rememberStateDirectory stages and publishes one typed intent so the
// managed state location becomes an authored Global Configuration value.
func rememberStateDirectory(manager *configuration.Manager, stateDirectory string) error {
	plan, err := manager.Plan(configuration.Repository(""), []configuration.Intent{
		configuration.SetStateDirectory{Directory: stateDirectory},
	})
	if err != nil {
		return fmt.Errorf("stage managed state location: %w", err)
	}
	if err := manager.Publish(plan); err != nil {
		return fmt.Errorf("remember managed state location: %w", err)
	}
	return nil
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
