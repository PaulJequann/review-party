package configuration

import (
	"bytes"
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
