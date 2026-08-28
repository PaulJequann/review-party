package configuration

import "io"

// AuthoredFile is the validated result of reading one scoped configuration
// file. The file bytes and decoded Document remain private to configuration.
type AuthoredFile struct {
	Scope           Scope
	Path            string
	Present         bool
	payload         []byte
	trailingNewline bool
}

// WritePayload writes the exact validated bytes authored in the file. An
// absent file writes nothing.
func (file AuthoredFile) WritePayload(output io.Writer) error {
	if !file.Present {
		return nil
	}
	_, err := output.Write(file.payload)
	return err
}

// HasTrailingNewline reports whether the authored payload already terminates
// with a newline, so presentation adapters can preserve the file's output
// without inspecting its bytes.
func (file AuthoredFile) HasTrailingNewline() bool {
	return file.Present && file.trailingNewline
}

// AuthoredInspection is an opaque snapshot of the requested authored scopes.
// Its validated Documents are retained for configuration-owned resolution but
// are not exposed to command adapters.
type AuthoredInspection struct {
	files  map[Scope]AuthoredFile
	loaded Loaded
}

// File returns the requested scope's authored-file facts. The second result
// reports whether that scope was requested, not whether a file is present.
func (inspection AuthoredInspection) File(scope Scope) (AuthoredFile, bool) {
	file, found := inspection.files[scope]
	return file, found
}

// InspectAuthored reads and validates the requested authored scopes once.
// Partial file facts are retained when a later read fails so adapters can
// report the same scope and path context alongside the original error.
func (manager *Manager) InspectAuthored(repository Repository, scopes []Scope) (AuthoredInspection, error) {
	inspection := AuthoredInspection{files: make(map[Scope]AuthoredFile, len(scopes))}
	for _, scope := range scopes {
		file, loaded, err := manager.readAuthoredFile(scope, repository)
		inspection.files[scope] = file
		switch scope {
		case ScopeGlobal:
			inspection.loaded.Global = loaded
		case ScopeRepository:
			inspection.loaded.Repository = loaded
		}
		if err != nil {
			return inspection, err
		}
	}
	return inspection, nil
}

// ResolveAuthored resolves effective values from an authored inspection
// without exposing the internal Loaded representation to the caller.
func (manager *Manager) ResolveAuthored(request Request, inspection AuthoredInspection) (Effective, error) {
	return manager.ResolveLoaded(request, inspection.loaded)
}

// ResolveRunAuthored resolves the Review selection from an authored
// inspection without exposing the internal Loaded representation to the
// caller.
func (manager *Manager) ResolveRunAuthored(request RunRequest, inspection AuthoredInspection) (ResolvedReviews, error) {
	return manager.ResolveRunLoaded(request, inspection.loaded)
}
