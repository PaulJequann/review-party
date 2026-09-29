package model

import (
	"strings"
	"testing"
)

func TestParseVerdictReasonTrimsAndBoundsOneLine(t *testing.T) {
	cases := map[string]struct {
		text, want, err string
	}{
		"trimmed":       {text: "  guarded upstream \t", want: "guarded upstream"},
		"at the limit":  {text: strings.Repeat("a", 240), want: strings.Repeat("a", 240)},
		"empty":         {text: " \t ", err: "the reason is empty"},
		"two lines":     {text: "first\nsecond", err: "the reason spans more than one line"},
		"carriage":      {text: "first\rsecond", err: "the reason spans more than one line"},
		"over by bytes": {text: strings.Repeat("é", 121), err: "the reason is 242 bytes; shorten it to 240"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			reason, err := ParseVerdictReason(test.text)
			if test.err != "" {
				if err == nil || err.Error() != test.err {
					t.Fatalf("error = %v, want %q", err, test.err)
				}
				return
			}
			if err != nil || reason.String() != test.want {
				t.Fatalf("reason = %q, %v; want %q", reason.String(), err, test.want)
			}
		})
	}
}

func TestParseVerdictVerbAcceptsOnlyTheThreeVerbs(t *testing.T) {
	for verb, want := range map[string]Verdict{"accept": VerdictAccepted, "reject": VerdictRejected, "defer": VerdictDeferred} {
		if got, ok := ParseVerdictVerb(verb); !ok || got != want {
			t.Fatalf("ParseVerdictVerb(%q) = %q, %v; want %q", verb, got, ok, want)
		}
	}
	for _, verb := range []string{"accepted", "Accept", "ok", ""} {
		if got, ok := ParseVerdictVerb(verb); ok {
			t.Fatalf("ParseVerdictVerb(%q) = %q, want no verdict", verb, got)
		}
	}
}

func TestFindingDigestChangesWithEveryTextField(t *testing.T) {
	base := Finding{Ordinal: 1, Severity: "high", Category: "correctness", Location: "a.go:1", Failure: "f", Evidence: "e", Fix: "x", Test: "t"}
	digest := FindingDigest(base)
	if len(digest) != 16 || strings.Trim(digest, "0123456789abcdef") != "" {
		t.Fatalf("digest = %q, want 16 lowercase hex characters", digest)
	}
	renumbered := base
	renumbered.Ordinal = 2
	if FindingDigest(renumbered) != digest {
		t.Fatal("digest depends on the ordinal, which is not Finding text")
	}
	edits := []func(*Finding){
		func(f *Finding) { f.Severity = "low" },
		func(f *Finding) { f.Category = "security" },
		func(f *Finding) { f.Location = "a.go:2" },
		func(f *Finding) { f.Failure = "g" },
		func(f *Finding) { f.Evidence = "d" },
		func(f *Finding) { f.Fix = "y" },
		func(f *Finding) { f.Test = "u" },
		func(f *Finding) { f.Failure, f.Evidence = "fe", "" },
	}
	for index, edit := range edits {
		changed := base
		edit(&changed)
		if FindingDigest(changed) == digest {
			t.Fatalf("edit %d kept the digest %q", index, digest)
		}
	}
}
