package engine

// ReviewPartyInitialization selects the repository and managed-state choices
// for first-use preparation. Initialization never creates Profile material.
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
	alreadyReady, err := prepareInitializationState(resolved.manager, resolved.selection)
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
