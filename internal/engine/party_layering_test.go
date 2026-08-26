package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
)

func TestPartyReaderRejectsExtends(t *testing.T) {
	repository := testRepository(t)
	path := filepath.Join(repository, ".reviewparty", "parties", "gate.json")
	writePartyBytes(t, path, []byte(`{"schema_version":1,"name":"gate","extends":["baseline"],"concurrency_limit":1,"profiles":[{"scope":"repository","profile":"bugs"}]}`))
	conductor := testPartyConductor(t, nil)
	_, _, err := conductor.resolveParty(partyLookup{repository: repository, name: "gate"})
	if err == nil || !strings.Contains(err.Error(), `unknown field "extends"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestPartySummaryKeepsScopedMembers(t *testing.T) {
	repository := testRepository(t)
	writeConfigurationParty(t, repository, configuration.Party{
		SchemaVersion: 1, Name: "gate", ConcurrencyLimit: 1,
		Profiles: []configuration.ProfileReference{{Scope: configuration.ScopeRepository, Profile: "bugs"}},
	})
	conductor := testPartyConductor(t, nil)
	summaries, err := conductor.PartiesForRepository(repository)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("summaries = %#v", summaries)
	}
	if len(summaries[0].Members) != 1 {
		t.Fatalf("members = %#v", summaries[0].Members)
	}
	member := summaries[0].Members[0]
	if member.Scope != "repository" || member.Profile != "bugs" {
		t.Fatalf("member = %#v", member)
	}
}

func writeConfigurationParty(t *testing.T, repository string, party configuration.Party) {
	t.Helper()
	payload, err := json.Marshal(party)
	if err != nil {
		t.Fatal(err)
	}
	writePartyBytes(t, filepath.Join(repository, ".reviewparty", "parties", party.Name+".json"), payload)
}

func writePartyBytes(t *testing.T, path string, payload []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
}
