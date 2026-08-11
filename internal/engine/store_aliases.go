package engine

import "reviewparty/internal/store"

// Store aliases — keep unqualified names used by conductor.
type recordStore = store.RecordStore
type ledgerRecordStore = store.LedgerRecordStore
type deferredLedgerRecordStore = store.DeferredLedgerRecordStore

var newLedgerRecordStore = store.NewLedgerRecordStore
var newDeferredLedgerRecordStore = store.NewDeferredLedgerRecordStore

// The helpers encodeReviewRecord, createTemporaryRecord, normalizeLoadedReviewRecord,
// path etc. are methods on fileRecordStore, not free functions, so they move with
// the type. No aliases needed.

// Store also uses model constants already aliased via model_aliases.
