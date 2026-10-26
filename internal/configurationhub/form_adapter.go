package configurationhub

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// formAdapter makes a Huh v2 form usable as one view inside the Hub's
// Bubble Tea v2 program. Huh keeps the v1-shaped Model contract, so it cannot
// be embedded directly in the tea.Model tree.
type formAdapter struct {
	form   *huh.Form
	group  *huh.Group
	width  int
	height int
}

func newFormAdapter(fields []huh.Field, width, height int, theme huh.Theme) formAdapter {
	width = max(width, 1)
	height = max(height, 1)
	group := huh.NewGroup(fields...)
	form := huh.NewForm(group).
		WithWidth(max(width-4, 1)).
		WithHeight(max(height-2, 1)).
		WithTheme(theme).
		WithShowHelp(false)
	return formAdapter{form: form, group: group, width: width, height: height}
}

func (adapter formAdapter) Init() tea.Cmd {
	if adapter.form == nil {
		return nil
	}
	// Activate the single group synchronously, then bind its messages to this
	// form. Unowned Huh next-field messages can otherwise advance a newer form.
	_ = adapter.form.Init()
	return tea.Batch(adapter.command(adapter.group.Init(), true), tea.RequestWindowSize)
}

func (adapter formAdapter) Update(message tea.Msg) (formAdapter, tea.Cmd) {
	if adapter.form == nil {
		return adapter, nil
	}
	owned, scoped := message.(formMessage)
	if scoped {
		message = owned.message
	}
	if size, ok := message.(tea.WindowSizeMsg); ok {
		adapter.width = max(size.Width, 1)
		adapter.height = max(size.Height, 1)
		// The adapter owns the box; Huh owns scrolling inside its padding.
		adapter.form.WithWidth(max(adapter.width-4, 1)).WithHeight(max(adapter.height-2, 1))
	}

	updated, command := adapter.form.Update(message)
	if form, ok := updated.(*huh.Form); ok {
		adapter.form = form
	}
	if key, ok := message.(tea.KeyPressMsg); ok && key.String() == "esc" {
		adapter.form.State = huh.StateAborted
	}
	return adapter, adapter.command(command, scoped)
}

func (adapter formAdapter) View() string {
	if adapter.form == nil {
		return ""
	}
	return renderBox(sectionTitleStyle.Render("Configuration"), adapter.form.View(), max(adapter.width, minBoxWidth), "")
}

func (adapter formAdapter) State() huh.FormState {
	if adapter.form == nil {
		return huh.StateAborted
	}
	return adapter.form.State
}

type formMessage struct {
	form    *huh.Form
	message tea.Msg
}

func (adapter formAdapter) command(command tea.Cmd, scoped bool) tea.Cmd {
	if command == nil {
		return nil
	}
	return func() tea.Msg {
		message := command()
		if batch, ok := message.(tea.BatchMsg); ok {
			commands := make([]tea.Cmd, 0, len(batch))
			for _, child := range batch {
				commands = append(commands, adapter.command(child, scoped))
			}
			return tea.BatchMsg(commands)
		}
		// Huh navigation emits private group transitions after NextField and
		// PrevField. Keep that command tree owned; ordinary field commands must
		// reach Bubble Tea intact so its runtime can execute them.
		if scoped || message == huh.NextField() || message == huh.PrevField() {
			return formMessage{form: adapter.form, message: message}
		}
		return message
	}
}
