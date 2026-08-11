package engine

import "reviewparty/internal/store"

// Store aliases — keep unqualified names used by conductor.
type recordStore = store.RecordStore
type fileRecordStore = store.FileRecordStore

var newFileRecordStore = store.NewFileRecordStore

// The helpers encodeReviewRecord, createTemporaryRecord, normalizeLoadedReviewRecord,
// path etc. are methods on fileRecordStore, not free functions, so they move with
// the type. No aliases needed.

// Store also uses model constants already aliased via model_aliases.
