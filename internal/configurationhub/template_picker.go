package configurationhub

import (
	"io"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"github.com/charmbracelet/x/ansi"

	"reviewparty/internal/configuration"
)

// templatePicker adds section headings without introducing selectable sentinel
// values. Huh still owns selection, keyboard navigation, and accessible input.
type templatePicker struct {
	*huh.Select[string]
	templates []configuration.Template
}

func newTemplatePicker(templates []configuration.Template, value *string) *templatePicker {
	ordered := slices.Clone(templates)
	slices.SortFunc(ordered, func(a, b configuration.Template) int {
		aSkill, bSkill := strings.HasPrefix(a.ID, "skill:"), strings.HasPrefix(b.ID, "skill:")
		if aSkill != bSkill {
			if aSkill {
				return 1
			}
			return -1
		}
		return strings.Compare(a.ID, b.ID)
	})
	options := make([]huh.Option[string], 0, len(ordered))
	for _, template := range ordered {
		options = append(options, huh.NewOption(template.ID+" ("+template.Revision+")", template.ID))
	}
	return &templatePicker{Select: huh.NewSelect[string]().Title("Template").Options(options...).Value(value), templates: ordered}
}

func (picker *templatePicker) Update(message tea.Msg) (huh.Model, tea.Cmd) {
	_, command := picker.Select.Update(message)
	return picker, command
}

func (picker *templatePicker) View() string {
	return picker.sections(picker.Select.View())
}

// Insert a heading above the first visible member of each section, including
// when scrolling has moved the first member out of the viewport.
func (picker *templatePicker) sections(view string) string {
	seen := make(map[string]bool)
	lines := strings.Split(view, "\n")
	var result []string
	for _, line := range lines {
		for _, template := range picker.templates {
			plain := ansi.Strip(line)
			index := strings.Index(plain, template.ID+" (")
			if index < 0 || (index > 0 && plain[index-1] != ' ') {
				continue
			}
			heading := "Review Party Templates"
			if strings.HasPrefix(template.ID, "skill:") {
				heading = "Skills"
			}
			if !seen[heading] {
				result = append(result, "  "+heading)
				seen[heading] = true
			}
			break
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

func (picker *templatePicker) WithTheme(theme huh.Theme) huh.Field {
	picker.Select.WithTheme(theme)
	return picker
}

func (picker *templatePicker) WithKeyMap(keymap *huh.KeyMap) huh.Field {
	picker.Select.WithKeyMap(keymap)
	return picker
}

func (picker *templatePicker) WithWidth(width int) huh.Field {
	picker.Select.WithWidth(width)
	return picker
}

func (picker *templatePicker) WithHeight(height int) huh.Field {
	// Reserve space for both section headings outside Huh's option viewport.
	picker.Select.WithHeight(max(height-2, 1))
	return picker
}

func (picker *templatePicker) WithPosition(position huh.FieldPosition) huh.Field {
	picker.Select.WithPosition(position)
	return picker
}

func (picker *templatePicker) RunAccessible(output io.Writer, input io.Reader) error {
	return picker.Select.RunAccessible(&templateSectionWriter{Writer: output, picker: picker}, input)
}

type templateSectionWriter struct {
	io.Writer
	picker *templatePicker
	seen   map[string]bool
}

func (writer *templateSectionWriter) Write(data []byte) (int, error) {
	// Accessible selects emit each numbered option in one write. Decorate only
	// the first option in each section; headings never receive option numbers.
	if writer.seen == nil {
		writer.seen = make(map[string]bool)
	}
	rendered := writer.picker.sections(string(data))
	for _, heading := range []string{"Review Party Templates", "Skills"} {
		marker := "  " + heading + "\n"
		if strings.Contains(rendered, marker) {
			if writer.seen[heading] {
				rendered = strings.ReplaceAll(rendered, marker, "")
			}
			writer.seen[heading] = true
		}
	}
	_, err := io.WriteString(writer.Writer, rendered)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}
