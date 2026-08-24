package engine

// artifact lifecycle helpers are now owned by reviewRunner (review.go).
// Conductor's wrappers in conductor.go delegate to the runner, preserving the
// public VerifyArtifacts seam while keeping attempt construction local to the
// deep Review module.
