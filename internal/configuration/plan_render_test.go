package configuration

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestManagerTemplateReturnsPackagedInstructions(t *testing.T) {
	manager := NewManager(Options{Templates: []Template{{ID: "bugs", Revision: "v1", Instructions: "Find bugs."}}})
	template, found := manager.Template("bugs")
	if !found || template.Instructions != "Find bugs." {
		t.Fatalf("template = %#v, found %v", template, found)
	}
}

func TestRenderPlanHumanIncludesValuesAndWarnings(t *testing.T) {
	plan := Plan{state: &planState{changes: []Change{{Scope: ScopeGlobal, Field: "profiles.quality", Path: "/config/profiles/quality", After: "model=luna", HadAfter: true}}, warnings: []string{"model is undiscovered"}}}
	var output bytes.Buffer
	if err := RenderPlanHuman(&output, plan); err != nil {
		t.Fatalf("render plan: %v", err)
	}
	if err := RenderPlanWarningsHuman(&output, plan.Warnings()); err != nil {
		t.Fatalf("render warnings: %v", err)
	}
	for _, want := range []string{"<absent> -> model=luna", "warning: model is undiscovered"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output %q does not contain %q", output.String(), want)
		}
	}
}

func TestRenderResolvedReviewsHuman(t *testing.T) {
	tests := []struct {
		name     string
		resolved ResolvedReviews
		want     string
	}{
		{
			name: "standard expansion",
			resolved: ResolvedReviews{Expanded: []ExpandedProfile{
				{Scope: ScopeGlobal, Profile: "bugs", Origin: "reviews.global[0]"},
				{Scope: ScopeRepository, Profile: "security", Origin: "reviews.repository[0]"},
			}},
			want: "resolved review selection:\n" +
				"  1. [global] bugs (reviews.global[0])\n" +
				"  2. [repository] security (reviews.repository[0])\n",
		},
		{
			name: "deduplication with origins",
			resolved: ResolvedReviews{
				Expanded:     []ExpandedProfile{{Scope: ScopeGlobal, Profile: "bugs", Origin: "reviews.global[0]"}},
				Deduplicated: []SkippedProfile{{Scope: ScopeGlobal, Profile: "bugs", Origin: "party baseline#0", KeptOrigin: "reviews.global[0]"}},
			},
			want: "resolved review selection:\n" +
				"  1. [global] bugs (reviews.global[0])\n" +
				"  deduplicated [global] bugs from party baseline#0 (kept by reviews.global[0])\n",
		},
		{
			name: "identical deduplication origin",
			resolved: ResolvedReviews{Deduplicated: []SkippedProfile{{
				Scope: ScopeRepository, Profile: "security", Origin: "same", KeptOrigin: "same",
			}}},
			want: "resolved review selection:\n" +
				"  deduplicated [repository] security from same\n",
		},
		{
			name:     "warnings",
			resolved: ResolvedReviews{Warnings: []ResolverWarning{{Message: "same name crosses scopes"}}},
			want:     "resolved review selection:\n  warning: same name crosses scopes\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := RenderResolvedReviewsHuman(&output, test.resolved); err != nil {
				t.Fatalf("render resolved reviews: %v", err)
			}
			if got := output.String(); got != test.want {
				t.Fatalf("output = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRenderResolvedReviewLinesHuman(t *testing.T) {
	resolved := ResolvedReviews{
		Expanded: []ExpandedProfile{
			{Scope: ScopeGlobal, Profile: "bugs", Origin: "reviews.global[0]"},
			{Scope: ScopeRepository, Profile: "security", Origin: "party baseline#1"},
		},
		Deduplicated: []SkippedProfile{
			{Scope: ScopeGlobal, Profile: "bugs", Origin: "party baseline#0", KeptOrigin: "reviews.global[0]"},
			{Scope: ScopeRepository, Profile: "security", Origin: "same", KeptOrigin: "same"},
		},
		Warnings: []ResolverWarning{{Message: "kept out of overview"}},
	}
	want := []string{
		"  1. [global] bugs (reviews.global[0])",
		"  2. [repository] security (party baseline#1)",
		"  deduplicated [global] bugs from party baseline#0 (kept by reviews.global[0])",
		"  deduplicated [repository] security from same",
	}
	if got := RenderResolvedReviewLinesHuman(resolved); !reflect.DeepEqual(got, want) {
		t.Fatalf("lines = %#v, want %#v", got, want)
	}
}
