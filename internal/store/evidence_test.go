package store

import (
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

	expired, err := ledger.ExpireEvidence(0)
	if err != nil {
		t.Fatal(err)
	}
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

func evidencePaths(references []model.ArtifactReference) []string {
	paths := []string{}
	for _, reference := range references {
		paths = append(paths, reference.Path)
	}
	return paths
}
