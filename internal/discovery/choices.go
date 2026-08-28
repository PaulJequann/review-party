package discovery

import (
	"context"
	"strings"
)

// ChoiceSource explains why a model is immediately available to a consumer.
type ChoiceSource string

const (
	ChoiceSourceCached     ChoiceSource = "cached"
	ChoiceSourceConfigured ChoiceSource = "configured"
	ChoiceSourcePackaged   ChoiceSource = "packaged"
	ChoiceSourceDiscovered ChoiceSource = "discovered"
)

// ModelChoice is a deduplicated picker entry with all known provenance.
type ModelChoice struct {
	Model   Model          `json:"model"`
	Sources []ChoiceSource `json:"sources"`
}

// ChoiceRequest supplies non-discovery choices that must remain usable while
// a live harness observation is pending.
type ChoiceRequest struct {
	Reviewer   string
	Configured []string
	Packaged   []string
}

// ChoiceSnapshot is the canonical immediate model-choice view. It preserves
// source provenance and answers exact-ID membership without starting discovery.
type ChoiceSnapshot struct {
	choices []ModelChoice
}

// ChoiceSnapshot builds cached, configured, and packaged choices in display
// order. The returned snapshot is independent of later source mutations.
func (service *Service) ChoiceSnapshot(request ChoiceRequest) ChoiceSnapshot {
	choices := []ModelChoice{}
	if cached, found := service.Cached(request.Reviewer); found {
		choices = MergeChoices(choices, cached.Models, ChoiceSourceCached)
	}
	choices = MergeChoices(choices, stringModels(request.Configured), ChoiceSourceConfigured)
	choices = MergeChoices(choices, stringModels(request.Packaged), ChoiceSourcePackaged)
	return ChoiceSnapshot{choices: choices}
}

// Choices returns the snapshot's choices with copied mutable fields.
func (snapshot ChoiceSnapshot) Choices() []ModelChoice {
	return cloneChoices(snapshot.choices)
}

// Contains reports whether the exact model ID appears in the snapshot.
func (snapshot ChoiceSnapshot) Contains(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	for _, choice := range snapshot.choices {
		if choice.Model.ID == model {
			return true
		}
	}
	return false
}

// ChoiceSession is returned without waiting for live discovery. Refresh is a
// one-result buffered channel and closes after the observation is delivered.
type ChoiceSession struct {
	Choices []ModelChoice
	Refresh <-chan Result
	cancel  context.CancelFunc
}

// Open builds immediate choices from cached, configured, and packaged models,
// then starts a bounded live observation in the background. Call Close when
// the consumer abandons the picker before the refresh arrives.
func (service *Service) Open(ctx context.Context, request ChoiceRequest) ChoiceSession {
	if ctx == nil {
		ctx = context.Background()
	}
	choices := service.ChoiceSnapshot(request).Choices()
	refreshContext, cancel := context.WithCancel(ctx)
	refresh := make(chan Result, 1)
	go func() {
		defer close(refresh)
		results := service.DiscoverMany(refreshContext, []string{request.Reviewer})
		refresh <- results[0]
	}()
	return ChoiceSession{Choices: choices, Refresh: refresh, cancel: cancel}
}

// Close abandons the live refresh and releases any adapter resources owned by
// the session. The refresh channel closes after the adapter returns.
func (session ChoiceSession) Close() {
	if session.cancel != nil {
		session.cancel()
	}
}

// MergeChoices adds models while preserving first-seen order and records each
// source once. It is safe for Hub code to call again with the live result.
func MergeChoices(existing []ModelChoice, models []Model, source ChoiceSource) []ModelChoice {
	choices := cloneChoices(existing)
	indexes := make(map[string]int, len(choices))
	for index, choice := range choices {
		indexes[choice.Model.ID] = index
	}
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		if model.ID == "" {
			continue
		}
		index, found := indexes[model.ID]
		if !found {
			choices = append(choices, ModelChoice{Model: cloneModel(model), Sources: []ChoiceSource{source}})
			indexes[model.ID] = len(choices) - 1
			continue
		}
		choices[index].Model = mergeModelMetadata(choices[index].Model, model)
		if !containsSource(choices[index].Sources, source) {
			choices[index].Sources = append(choices[index].Sources, source)
		}
	}
	return choices
}

func cloneChoices(choices []ModelChoice) []ModelChoice {
	result := make([]ModelChoice, len(choices))
	for index, choice := range choices {
		result[index] = ModelChoice{Model: cloneModel(choice.Model), Sources: append([]ChoiceSource(nil), choice.Sources...)}
	}
	return result
}

func mergeModelMetadata(current, incoming Model) Model {
	if current.DisplayName == "" {
		current.DisplayName = incoming.DisplayName
	}
	if !current.Default {
		current.Default = incoming.Default
	}
	if len(current.ReasoningEfforts) == 0 {
		current.ReasoningEfforts = append([]string(nil), incoming.ReasoningEfforts...)
	}
	return current
}

func containsSource(sources []ChoiceSource, wanted ChoiceSource) bool {
	for _, source := range sources {
		if source == wanted {
			return true
		}
	}
	return false
}

func stringModels(values []string) []Model {
	models := make([]Model, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			models = append(models, Model{ID: strings.TrimSpace(value)})
		}
	}
	return models
}

func cloneModel(model Model) Model {
	model.ReasoningEfforts = append([]string(nil), model.ReasoningEfforts...)
	return model
}

func cloneModels(models []Model) []Model {
	result := make([]Model, len(models))
	for index, model := range models {
		result[index] = cloneModel(model)
	}
	return result
}
