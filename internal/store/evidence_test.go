package store

import (
	"errors"
	"reflect"
	"testing"

	"reviewparty/internal/model"
)

func TestResaveDoesNotRestoreExpiredEvidence(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	record := ledgerFixture(model.LifecycleIncomplete)
	if err := ledger.Save(record); err != nil {
		t.Fatal(err)
	}

	expired := expireAll(t, ledger)
	if got := evidencePaths(expired); !reflect.DeepEqual(got, []string{"artifacts/a"}) {
		t.Fatalf("expired = %v, want the one saved artifact", got)
	}
	record.Passes[0].Attempts = append(record.Passes[0].Attempts, model.AttemptRecord{Number: 2, Outcome: model.AttemptInvalidResult, Artifacts: []model.ArtifactReference{{Kind: "assistant-text", Path: "artifacts/b", Size: 1, Digest: "digest"}}, StartedAt: record.CreatedAt, CompletedAt: record.UpdatedAt})
	if err := ledger.Save(record); err != nil {
		t.Fatal(err)
	}

	stored, err := ledger.Load(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got [][]string
	for _, attempt := range stored.Passes[0].Attempts {
		got = append(got, evidencePaths(attempt.Artifacts))
	}
	if want := [][]string{{}, {"artifacts/b"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("attempt evidence = %v, want %v: a re-save must not restore expired evidence", got, want)
	}
}

func TestFailedEvidenceRemovalKeepsTheReferencesForTheNextExpiry(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	record := ledgerFixture(model.LifecycleIncomplete)
	if err := ledger.Save(record); err != nil {
		t.Fatal(err)
	}

	failure := errors.New("artifact busy")
	if err := ledger.ExpireEvidence(0, func([]model.ArtifactReference) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("expire error = %v, want the removal failure", err)
	}
	if got := evidencePaths(expireAll(t, ledger)); !reflect.DeepEqual(got, []string{"artifacts/a"}) {
		t.Fatalf("retried expiry removed %v, want the evidence the failed removal kept", got)
	}
	if got := evidencePaths(expireAll(t, ledger)); len(got) != 0 {
		t.Fatalf("expiry after a removal offered %v again", got)
	}
}

func expireAll(t *testing.T, ledger *LedgerRecordStore) []model.ArtifactReference {
	t.Helper()
	var removed []model.ArtifactReference
	if err := ledger.ExpireEvidence(0, func(expired []model.ArtifactReference) error {
		removed = append(removed, expired...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return removed
}

func evidencePaths(references []model.ArtifactReference) []string {
	paths := []string{}
	for _, reference := range references {
		paths = append(paths, reference.Path)
	}
	return paths
}
