package configurationhub

import "testing"

func TestCopyProfileOptionsUseRepositoryNames(t *testing.T) {
	model := New(Snapshot{Items: []Item{
		{Scope: "global", Kind: itemProfile, Name: "global-profile"},
		{Scope: "repository", Kind: itemProfile, Name: "repository-profile"},
	}})
	options := model.repositoryProfileOptions()
	if len(options) != 1 {
		t.Fatalf("repository copy options = %#v, want one option", options)
	}
	if options[0].Value != "repository-profile" {
		t.Fatalf("repository copy value = %q, want unqualified name", options[0].Value)
	}
}
