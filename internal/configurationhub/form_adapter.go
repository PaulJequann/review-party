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
	width  int
	height int
}

func newFormAdapter(fields []huh.Field, width, height int, theme huh.Theme) formAdapter {
	width = max(width, 1)
	height = max(height, 1)
	form := huh.NewForm(huh.NewGroup(fields...)).
		WithWidth(width).
		WithHeight(height).
		WithTheme(theme).
		WithShowHelp(false)
	return formAdapter{form: form, width: width, height: height}
}

func (adapter formAdapter) Init() tea.Cmd {
	if adapter.form == nil {
		return nil
	}
	return adapter.form.Init()
}

func (adapter formAdapter) Update(message tea.Msg) (formAdapter, tea.Cmd) {
	if adapter.form == nil {
		return adapter, nil
	}
	if size, ok := message.(tea.WindowSizeMsg); ok {
		adapter.width = max(size.Width, 1)
		adapter.height = max(size.Height, 1)
		adapter.form.WithWidth(adapter.width).WithHeight(adapter.height)
	}

	updated, command := adapter.form.Update(message)
	if form, ok := updated.(*huh.Form); ok {
		adapter.form = form
	}
	if key, ok := message.(tea.KeyPressMsg); ok && key.String() == "esc" {
		adapter.form.State = huh.StateAborted
	}
	return adapter, command
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
