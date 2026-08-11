package provenance

import "reviewparty/internal/model"

type RuntimeProvenance = model.RuntimeProvenance

// Alias for private helper used by tests (runtimeProvenanceFrom is now exported as RuntimeProvenanceFrom,
// but tests call the private name). Keep backward compat.
var runtimeProvenanceFrom = RuntimeProvenanceFrom
