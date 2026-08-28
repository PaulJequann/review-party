package engine

import (
	"strings"
	"testing"
	"time"

	"reviewparty/internal/configuration"
	"reviewparty/internal/model"
)

func TestCompileSlotsUsesCapturedRuntimeProfile(t *testing.T) {
	slot := configuration.ExpandedProfile{Scope: configuration.ScopeGlobal, Profile: "bugs", Origin: "explicit"}
	snapshot := capturedRuntimeSnapshot{
		selection: configuration.ResolvedReviews{Expanded: []configuration.ExpandedProfile{slot}},
		profile: configuration.Profile{
			SchemaVersion: 1, Name: "bugs", Reviewer: defaultReviewer, Model: "grok-4.5",
			ReasoningEffort: "high", AttemptDeadline: "1m", Instructions: "captured instructions\n",
			Scope: configuration.ScopeGlobal, Source: "global:profiles/bugs", SourceDigest: "captured-digest",
		},
	}
	conductor := &Conductor{reviewers: defaultReviewerCatalog(), now: time.Now}

	slots, err := conductor.compileSlots(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 1 || !strings.Contains(slots[0].profile.prompt(model.ReviewSubject{}), "captured instructions") {
		t.Fatalf("compiled slots = %#v", slots)
	}
}

type capturedRuntimeSnapshot struct {
	selection configuration.ResolvedReviews
	profile   configuration.Profile
}

func (snapshot capturedRuntimeSnapshot) Selection() configuration.ResolvedReviews {
	return snapshot.selection
}

func (capturedRuntimeSnapshot) Effective() configuration.Effective {
	return configuration.Effective{}
}

func (snapshot capturedRuntimeSnapshot) ProfileFor(configuration.ExpandedProfile) (configuration.Profile, bool) {
	return snapshot.profile, true
}
