package configurationhub

import (
	"bytes"
	"strings"
	"testing"

	"reviewparty/internal/configuration"
)

func TestTemplatePickerGroupsAccessibleChoicesWithoutNumberingHeadings(t *testing.T) {
	templates := []configuration.Template{
		{ID: "skill:audit", Revision: "skill-v1"},
		{ID: "test-audit", Revision: "test-v1"},
		{ID: "bugs", Revision: "bugs-v1"},
		{ID: "skill:bugs", Revision: "skill-v2"},
	}
	for _, test := range []struct{ input, want string }{
		{"2\n", "test-audit"},
		{"3\n", "skill:audit"},
	} {
		t.Run(test.want, func(t *testing.T) {
			var value string
			var output bytes.Buffer
			picker := newTemplatePicker(templates, &value)
			if err := picker.RunAccessible(&output, strings.NewReader(test.input)); err != nil {
				t.Fatal(err)
			}
			if value != test.want {
				t.Fatalf("selected %q, want %q", value, test.want)
			}
			rendered := output.String()
			fragments := []string{"Review Party Templates", "1. bugs", "2. test-audit", "Skills", "3. skill:audit", "4. skill:bugs"}
			previous := -1
			for _, fragment := range fragments {
				index := strings.Index(rendered, fragment)
				if index <= previous {
					t.Fatalf("missing or misplaced %q:\n%s", fragment, rendered)
				}
				previous = index
			}
			if strings.Count(rendered, "\n  Skills\n") != 1 {
				t.Fatalf("repeated skill heading:\n%s", rendered)
			}
		})
	}
}

func TestTemplatePickerLabelsVisibleSectionAfterScrolling(t *testing.T) {
	var value string
	picker := newTemplatePicker([]configuration.Template{{ID: "bugs"}, {ID: "skill:bugs"}}, &value)
	got := picker.sections("Template\n> skill:bugs (v1)\n")
	if got != "Template\n  Skills\n> skill:bugs (v1)\n" {
		t.Fatalf("unexpected visible section: %q", got)
	}
}
