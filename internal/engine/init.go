package engine

import (
	"errors"

	"reviewparty/internal/artifact"
	"reviewparty/internal/store"
)

// ReviewPartyInitialization selects the repository and managed-state choices
// for first-use preparation. This step prepares managed state only; Profiles,
// Parties, and the Review selection are separate Plans the CLI publishes
// after it.
type ReviewPartyInitialization struct {
	Repository              string
	StateDirectory          string
	UserConfigurationPath   string
	UseDefaultConfiguration bool
	BackupIncompatible      bool
	Fresh                   bool
}

type ReviewPartyInitializationResult struct {
	Repository     string
	StateDirectory string
	AdvancedState  bool
	AlreadyReady   bool
	Backup         *store.StateBackup
}

func InitializeReviewParty(request ReviewPartyInitialization) (ReviewPartyInitializationResult, error) {
	if request.BackupIncompatible && request.Fresh {
		return ReviewPartyInitializationResult{}, errors.New("backup and fresh initialization require separate requests")
	}
	resolved, err := resolveInitialization(request)
	if err != nil {
		return ReviewPartyInitializationResult{}, err
	}
	if request.BackupIncompatible {
		backup, err := store.BackupIncompatibleReviewRecordState(string(resolved.selection.directory), artifact.Directory)
		if err != nil {
			return ReviewPartyInitializationResult{}, err
		}
		return ReviewPartyInitializationResult{Repository: resolved.repository, StateDirectory: string(resolved.selection.directory), AdvancedState: resolved.selection.advanced, Backup: &backup}, nil
	}
	alreadyReady, err := prepareInitializationState(resolved.manager, resolved.selection, request.Fresh)
	if err != nil {
		return ReviewPartyInitializationResult{}, err
	}
	return ReviewPartyInitializationResult{
		Repository:     resolved.repository,
		StateDirectory: string(resolved.selection.directory),
		AdvancedState:  resolved.selection.advanced,
		AlreadyReady:   alreadyReady,
	}, nil
}
