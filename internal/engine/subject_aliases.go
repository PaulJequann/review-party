package engine

import "reviewparty/internal/subject"

// Subject helpers — keep lower-case names used by engine/conductor.
var resolveSubject = subject.ResolveSubject
var resolveRepositoryRoot = subject.ResolveRepositoryRoot
var resolveWorkingChanges = subject.ResolveWorkingChanges
