package configuration

// Overrides are explicit Caller choices supplied at invocation time. They take
// precedence over every authored scope and carry explicit provenance.
type Overrides struct {
	Reviewer string
	Model    string
	Profile  string
}

// Request describes one effective-resolution query.
type Request struct {
	Repository Repository
	Overrides  Overrides
}

// Value is one resolved configuration value with its provenance. Authored is
// false for packaged fallbacks so absent authored values remain
// distinguishable from effective defaults.
type Value[T any] struct {
	Value    T
	Authored bool
	Source   Source
	Path     string
}

// ReviewerSettings resolves one reviewer's policy across scopes.
type ReviewerSettings struct {
	Enabled       Value[bool]
	Model         Value[string]
	AllowedModels Value[[]string]
}

// Effective is the resolved configuration Review Party will use, with every
// value's originating scope and file.
type Effective struct {
	DefaultProfile  Value[string]
	DefaultReviewer Value[string]
	Model           Value[string]
	StateDirectory  Value[string]
	Eval            Value[EvalPolicy]
	Reviewers       map[string]ReviewerSettings
}

// Resolve loads both scopes and returns effective values with exact
// provenance across packaged, Personal, Repository, and explicit sources.
func (manager *Manager) Resolve(request Request) (Effective, error) {
	loaded, err := manager.Load(request.Repository)
	if err != nil {
		return Effective{}, err
	}
	effective := Effective{
		DefaultProfile:  resolveDefault(loaded, defaultChoice(request.Overrides.Profile), profileDefault, defaultChoice(manager.packaged.defaultProfile)),
		DefaultReviewer: resolveDefault(loaded, defaultChoice(request.Overrides.Reviewer), reviewerDefault, defaultChoice(manager.packaged.defaultReviewer)),
		StateDirectory:  personalValue(loaded, stateDirectoryValue),
		Eval:            evalValue(loaded),
		Reviewers:       map[string]ReviewerSettings{},
	}
	if request.Overrides.Model != "" {
		effective.Model = Value[string]{Value: request.Overrides.Model, Authored: true, Source: SourceExplicit}
	}
	for _, id := range manager.knownReviewers() {
		effective.Reviewers[id] = resolveReviewerSettings(loaded, reviewerID(id))
	}
	return effective, nil
}

type documentDefault func(Document) (string, bool)
type configurationText string
type reviewerID string
type defaultChoice string

type valueOrigin struct {
	Source Source
	Path   string
}

func profileDefault(document Document) (string, bool) {
	return authoredString(configurationText(document.Defaults.Profile))
}

func reviewerDefault(document Document) (string, bool) {
	return authoredString(configurationText(document.Defaults.Reviewer))
}

func resolveDefault(loaded Loaded, override defaultChoice, read documentDefault, fallback defaultChoice) Value[string] {
	if override != "" {
		return Value[string]{Value: string(override), Authored: true, Source: SourceExplicit}
	}
	for _, layer := range []LoadedDocument{loaded.Repository, loaded.Personal} {
		if !layer.Present {
			continue
		}
		if value, authored := read(layer.Document); authored {
			return Value[string]{Value: value, Authored: true, Source: sourceFor(layer.Scope), Path: layer.Path}
		}
	}
	return Value[string]{Value: string(fallback), Source: SourcePackaged}
}

func resolveReviewerSettings(loaded Loaded, id reviewerID) ReviewerSettings {
	settings := ReviewerSettings{
		Enabled:       Value[bool]{Value: true, Source: SourcePackaged},
		Model:         Value[string]{Source: SourcePackaged},
		AllowedModels: Value[[]string]{Source: SourcePackaged},
	}
	settings.apply(loaded.Personal, id)
	settings.apply(loaded.Repository, id)
	return settings
}

func (settings *ReviewerSettings) apply(layer LoadedDocument, id reviewerID) {
	if !layer.Present {
		return
	}
	policy, exists := layer.Document.Reviewers[string(id)]
	if !exists {
		return
	}
	origin := valueOrigin{Source: sourceFor(layer.Scope), Path: layer.Path}
	settings.applyEnabled(policy.Enabled, origin)
	settings.applyModel(configurationText(policy.Model), origin)
	settings.applyAllowedModels(policy.AllowedModels, origin)
}

func (settings *ReviewerSettings) applyEnabled(enabled *bool, origin valueOrigin) {
	if enabled == nil || settings.Enabled.Authored {
		return
	}
	settings.Enabled = Value[bool]{Value: *enabled, Authored: true, Source: origin.Source, Path: origin.Path}
}

func (settings *ReviewerSettings) applyModel(model configurationText, origin valueOrigin) {
	if model == "" || settings.Model.Authored {
		return
	}
	settings.Model = Value[string]{Value: string(model), Authored: true, Source: origin.Source, Path: origin.Path}
}

func (settings *ReviewerSettings) applyAllowedModels(models []string, origin valueOrigin) {
	if models == nil || settings.AllowedModels.Authored {
		return
	}
	settings.AllowedModels = Value[[]string]{Value: append([]string(nil), models...), Authored: true, Source: origin.Source, Path: origin.Path}
}

func sourceFor(scope Scope) Source {
	switch scope {
	case ScopeRepository:
		return SourceRepository
	default:
		return SourcePersonal
	}
}

func authoredString(value configurationText) (string, bool) {
	return string(value), value != ""
}

func stateDirectoryValue(document Document) (string, bool) {
	return authoredString(configurationText(document.StateDirectory))
}

type documentValue[T any] func(Document) (T, bool)

func personalValue[T any](loaded Loaded, read documentValue[T]) Value[T] {
	if loaded.Personal.Present {
		if value, authored := read(loaded.Personal.Document); authored {
			return Value[T]{Value: value, Authored: true, Source: SourcePersonal, Path: loaded.Personal.Path}
		}
	}
	return Value[T]{Source: SourcePackaged}
}

func evalValue(loaded Loaded) Value[EvalPolicy] {
	return personalValue(loaded, func(document Document) (EvalPolicy, bool) {
		if document.Eval == nil {
			return EvalPolicy{}, false
		}
		return *document.Eval, true
	})
}
