package store

import (
	"reflect"
	"testing"
	"time"

	"reviewparty/internal/model"
)

func waiverFixture(id model.WaiverID, key model.WaiverKey, minute int) model.CheckpointWaiver {
	created := time.Date(2026, 10, 1, 12, minute, 0, 0, time.UTC)
	return model.CheckpointWaiver{ID: id, Key: key, Repository: "/work/repo", Reason: "hotfix " + string(id), WaivedBy: model.WaivedByTerminal, CreatedAt: created}
}

func withRepository(waiver model.CheckpointWaiver, repository string) model.CheckpointWaiver {
	waiver.Repository = repository
	return waiver
}

func waiversSince(t *testing.T, ledger *LedgerRecordStore, repository string, since time.Time) []model.CheckpointWaiver {
	t.Helper()
	waivers, err := ledger.CheckpointWaiversSince(repository, since)
	if err != nil {
		t.Fatalf("waivers since %s in %s = %v", since, repository, err)
	}
	return waivers
}

func TestCheckpointWaiverMatchesOnlyItsKey(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	key := model.WaiverKey{Checkpoint: "pre-push", ContentDigest: model.ContentChangesDigest(coveredChanges)}
	older, newer := waiverFixture("wv_1", key, 1), waiverFixture("wv_2", key, 2)
	for _, waiver := range []model.CheckpointWaiver{newer, older} {
		if err := ledger.RecordCheckpointWaiver(waiver); err != nil {
			t.Fatal(err)
		}
	}

	if found, ok := lookupWaiver(t, ledger, key); !ok || !reflect.DeepEqual(found, newer) {
		t.Fatalf("waiver = %#v (found %v), want %#v", found, ok, newer)
	}
	for name, other := range map[string]model.WaiverKey{
		"other checkpoint": {Checkpoint: "pre-commit", ContentDigest: key.ContentDigest},
		"other content":    {Checkpoint: "pre-push", ContentDigest: model.ContentChangesDigest(coveredChanges[:1])},
	} {
		if found, ok := lookupWaiver(t, ledger, other); ok {
			t.Fatalf("%s matched waiver %#v", name, found)
		}
	}
}

func TestCheckpointWaiverPrefersATerminalWaiverOverANewerNonInteractiveOne(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	key := model.WaiverKey{Checkpoint: "pre-push", ContentDigest: model.ContentChangesDigest(coveredChanges)}
	terminal, agent := waiverFixture("wv_1", key, 1), waiverFixture("wv_2", key, 2)
	agent.WaivedBy = model.WaivedByNonInteractive
	for _, waiver := range []model.CheckpointWaiver{terminal, agent} {
		if err := ledger.RecordCheckpointWaiver(waiver); err != nil {
			t.Fatal(err)
		}
	}

	if found, ok := lookupWaiver(t, ledger, key); !ok || !reflect.DeepEqual(found, terminal) {
		t.Fatalf("waiver = %#v (found %v), want the terminal waiver %#v", found, ok, terminal)
	}
}

func lookupWaiver(t *testing.T, ledger *LedgerRecordStore, key model.WaiverKey) (model.CheckpointWaiver, bool) {
	t.Helper()
	found, ok, err := ledger.CheckpointWaiver(key)
	if err != nil {
		t.Fatal(err)
	}
	return found, ok
}

func TestPrepareUpgradesSchemaThirteenLedgerToRecordWaivers(t *testing.T) {
	directory := t.TempDir()
	review := writeLedgerAtSchema(t, directory, 13)

	if err := PrepareReviewRecordState(directory); err != nil {
		t.Fatalf("prepare = %v", err)
	}
	if version := readSchemaVersion(t, directory); version != currentLedgerSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentLedgerSchemaVersion)
	}
	ledger, err := openLedgerRecordStore(directory, false)
	if err != nil {
		t.Fatalf("open upgraded ledger = %v", err)
	}
	defer closeTestResource(t, ledger.Close)
	if _, err := ledger.Load(review.ID); err != nil {
		t.Fatalf("load preserved review = %v", err)
	}
	key := model.WaiverKey{Checkpoint: "pre-push", ContentDigest: model.ContentChangesDigest(coveredChanges)}
	if err := ledger.RecordCheckpointWaiver(waiverFixture("wv_1", key, 1)); err != nil {
		t.Fatalf("record waiver after upgrade = %v", err)
	}
	if _, ok, err := ledger.CheckpointWaiver(key); err != nil || !ok {
		t.Fatalf("waiver after upgrade = %v, %v", ok, err)
	}
}

func TestCheckpointWaiversSinceListsOneRepositoryNewestFirst(t *testing.T) {
	ledger := newTestLedger(t, t.TempDir())
	defer closeTestResource(t, ledger.Close)
	key := model.WaiverKey{Checkpoint: "pre-push", ContentDigest: model.ContentChangesDigest(coveredChanges)}
	recorded := map[model.WaiverID]model.CheckpointWaiver{}
	for id, waiver := range map[model.WaiverID]struct {
		repository string
		minute     int
	}{"wv_old": {"/repo", 1}, "wv_edge": {"/repo", 10}, "wv_new": {"/repo", 20}, "wv_other": {"/other", 30}} {
		recorded[id] = waiverFixture(id, key, waiver.minute)
		recorded[id] = withRepository(recorded[id], waiver.repository)
		if err := ledger.RecordCheckpointWaiver(recorded[id]); err != nil {
			t.Fatal(err)
		}
	}

	if waivers, want := waiversSince(t, ledger, "/repo", recorded["wv_edge"].CreatedAt), []model.CheckpointWaiver{recorded["wv_new"], recorded["wv_edge"]}; !reflect.DeepEqual(waivers, want) {
		t.Fatalf("waivers = %#v, want %#v", waivers, want)
	}
	if none := waiversSince(t, ledger, "/absent", time.Time{}); !reflect.DeepEqual(none, []model.CheckpointWaiver{}) {
		t.Fatalf("absent repository waivers = %#v, want an empty list", none)
	}
}
