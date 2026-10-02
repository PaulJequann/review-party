package configurationhub

import (
	"bytes"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
)

func TestAccessiblePartyCreationPublishesTheChosenProfiles(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	repository := configuration.Repository(t.TempDir())
	publishRepositoryProfile(t, manager, repository, "bugs")
	publishRepositoryProfile(t, manager, repository, "docs")
	editor := &editor{manager: manager, RunOptions: RunOptions{
		Repository: repository, Accessible: true, Output: &bytes.Buffer{},
		Input: newLineInput("2\nteam\n\n2\n0\n1\ny\n"),
	}}
	if err := editor.refresh(); err != nil {
		t.Fatal(err)
	}
	if err := editor.createParty(); err != nil {
		t.Fatalf("createParty: %v\n%s", err, editor.Output)
	}
	party, found, err := manager.LoadParty(configuration.ScopeRepository, repository, "team")
	if err != nil || !found {
		t.Fatalf("party team found=%v err=%v", found, err)
	}
	want := configuration.ProfileReference{Scope: configuration.ScopeRepository, Profile: "docs"}
	if len(party.Profiles) != 1 || party.Profiles[0] != want {
		t.Fatalf("party members = %#v, want only %#v", party.Profiles, want)
	}
}

func TestAccessibleGlobalPartyCreationOffersOnlyGlobalProfiles(t *testing.T) {
	manager := configuration.NewManager(configuration.Options{
		GlobalRoot: t.TempDir(), Reviewers: []string{"codex"},
		ValidateName: func(string) error { return nil },
	})
	repository := configuration.Repository(t.TempDir())
	publishGlobalProfile(t, manager, "docs")
	publishRepositoryProfile(t, manager, repository, "bugs")
	output := &bytes.Buffer{}
	editor := &editor{manager: manager, RunOptions: RunOptions{
		Repository: repository, Accessible: true, Output: output,
		Input: newLineInput("1\ncrew\n\n1\n0\n1\ny\n"),
	}}
	if err := editor.refresh(); err != nil {
		t.Fatal(err)
	}
	if err := editor.createParty(); err != nil {
		t.Fatalf("createParty: %v\n%s", err, output)
	}
	if _, members, _ := strings.Cut(output.String(), "Profiles"); strings.Contains(members, "bugs") {
		t.Fatalf("a Global Party offered a Repository Profile:\n%s", members)
	}
	party, found, err := manager.LoadParty(configuration.ScopeGlobal, repository, "crew")
	if err != nil || !found {
		t.Fatalf("party crew found=%v err=%v", found, err)
	}
	want := configuration.ProfileReference{Scope: configuration.ScopeGlobal, Profile: "docs"}
	if len(party.Profiles) != 1 || party.Profiles[0] != want {
		t.Fatalf("party members = %#v, want only %#v", party.Profiles, want)
	}
}
