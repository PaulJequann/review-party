package configurationhub

import (
	"strings"
	"testing"
)

func TestAttachReceiptsAnnotatesProfilesOnly(t *testing.T) {
	snapshot := Snapshot{Items: []Item{
		{Scope: "global", Kind: itemProfile, Name: "bugs", Detail: "grok / grok-4.5"},
		{Scope: "global", Kind: itemTemplate, Name: "bugs", Detail: "Review Profile Template v1"},
		{Scope: "global", Kind: itemParty, Name: "baseline", Detail: "1 Profile"},
	}}
	attachReceipts(&snapshot, func(profile string) (ProfileReceipt, bool) {
		if profile != "bugs" {
			return ProfileReceipt{}, false
		}
		return ProfileReceipt{Runs: 2, MedianDuration: "2m0s", TotalFindings: 3}, true
	})
	profile := snapshot.Items[0]
	if !strings.Contains(profile.Detail, "grok / grok-4.5") || !strings.Contains(profile.Detail, "last 2 runs") {
		t.Fatalf("detail = %q", profile.Detail)
	}
	for _, item := range snapshot.Items[1:] {
		if strings.Contains(item.Detail, "last") {
			t.Fatalf("non-profile annotated: %#v", item)
		}
	}
}

func TestAttachReceiptsNilProviderRendersUnchanged(t *testing.T) {
	snapshot := Snapshot{Items: []Item{
		{Scope: "global", Kind: itemProfile, Name: "bugs", Detail: "grok / grok-4.5"},
	}}
	attachReceipts(&snapshot, nil)
	if snapshot.Items[0].Detail != "grok / grok-4.5" {
		t.Fatalf("detail = %q", snapshot.Items[0].Detail)
	}
}

func TestFormatReceiptUsesSingularForms(t *testing.T) {
	got := FormatReceipt(ProfileReceipt{Runs: 1, MedianDuration: "1m0s", TotalFindings: 1})
	if got != "last 1 run · med 1m0s · 1 finding" {
		t.Fatalf("receipt = %q", got)
	}
}

func TestFormatReceiptCarriesIncompleteAndOmitsEmptyMedian(t *testing.T) {
	got := FormatReceipt(ProfileReceipt{Runs: 3, MedianDuration: "2m0s", TotalFindings: 1, IncompleteRuns: 2})
	if got != "last 3 runs · 2 incomplete · med 2m0s · 1 finding" {
		t.Fatalf("receipt = %q", got)
	}
	onlyIncomplete := FormatReceipt(ProfileReceipt{Runs: 1, IncompleteRuns: 1})
	if onlyIncomplete != "last 1 run · 1 incomplete · 0 findings" {
		t.Fatalf("receipt = %q", onlyIncomplete)
	}
}
