package model

import "testing"

func TestContentChangesDigestIdentifiesTheSet(t *testing.T) {
	first := ContentChange{Path: "a.go", Before: ZeroObjectID, After: "1111111111111111111111111111111111111111"}
	second := ContentChange{Path: "b.go", Before: "2222222222222222222222222222222222222222", After: ZeroObjectID}
	digest := ContentChangesDigest([]ContentChange{first, second})
	if len(digest) != 64 {
		t.Fatalf("digest = %q, want 64 hex characters", digest)
	}
	if reordered := ContentChangesDigest([]ContentChange{second, first}); reordered != digest {
		t.Fatalf("reordered digest = %s, want %s", reordered, digest)
	}
	edited := second
	edited.After = "3333333333333333333333333333333333333333"
	for name, changes := range map[string][]ContentChange{
		"subset":       {first},
		"edited entry": {first, edited},
		"empty":        nil,
	} {
		if ContentChangesDigest(changes) == digest {
			t.Errorf("%s shares the digest of a different set", name)
		}
	}
}
