package configurationhub

import (
	bubbleshelp "charm.land/bubbles/v2/help"
	bubbleskey "charm.land/bubbles/v2/key"
)

type hubHelpKeyMap struct {
	bindings []bubbleskey.Binding
}

func (keyMap hubHelpKeyMap) ShortHelp() []bubbleskey.Binding {
	return keyMap.bindings
}

func (keyMap hubHelpKeyMap) FullHelp() [][]bubbleskey.Binding {
	return [][]bubbleskey.Binding{keyMap.bindings}
}

func newHubHelp() bubbleshelp.Model {
	view := bubbleshelp.New()
	view.ShowAll = true
	view.FullSeparator = "  "
	view.Styles = bubbleshelp.Styles{
		Ellipsis:       metaStyle,
		ShortKey:       sectionTitleStyle,
		ShortDesc:      metaStyle,
		ShortSeparator: metaStyle,
		FullKey:        sectionTitleStyle,
		FullDesc:       metaStyle,
		FullSeparator:  metaStyle,
	}
	return view
}

func helpBinding(keyText, description string, keys ...string) bubbleskey.Binding {
	return bubbleskey.NewBinding(
		bubbleskey.WithKeys(keys...),
		bubbleskey.WithHelp(keyText, description),
	)
}

func (model Model) helpKeyMap() hubHelpKeyMap {
	bindings := []bubbleskey.Binding{helpBinding("?", "toggle help", "?")}
	switch model.view {
	case viewForm:
		bindings = append(bindings, formHelpBindings()...)
	case viewPlanPreview:
		bindings = append(bindings, planHelpBindings()...)
	case viewBrowser:
		bindings = append(bindings, model.browserHelpBindings()...)
	case viewMenu:
		bindings = append(bindings, menuHelpBindings(model.searching)...)
	}
	return hubHelpKeyMap{bindings: bindings}
}

func formHelpBindings() []bubbleskey.Binding {
	return []bubbleskey.Binding{
		helpBinding("↑/↓", "move between fields", "up", "down"),
		helpBinding("tab", "next field", "tab"),
		helpBinding("shift+tab", "previous field", "shift+tab"),
		helpBinding("space", "select or toggle", " "),
		helpBinding("enter", "continue", "enter"),
		helpBinding("esc", "back", "esc"),
		helpBinding("ctrl+c", "quit", "ctrl+c"),
	}
}

func planHelpBindings() []bubbleskey.Binding {
	return []bubbleskey.Binding{
		helpBinding("p", "publish", "p"),
		helpBinding("e", "revise", "e"),
		helpBinding("esc", "cancel", "esc"),
		helpBinding("ctrl+c", "quit", "ctrl+c"),
	}
}

func (model Model) browserHelpBindings() []bubbleskey.Binding {
	bindings := []bubbleskey.Binding{
		helpBinding("↑/↓", "navigate", "up", "down", "j", "k"),
		helpBinding("/", "filter", "/"),
		helpBinding("esc", "back", "esc"),
		helpBinding("q", "quit", "q"),
		helpBinding("ctrl+c", "quit", "ctrl+c"),
	}
	if model.moveReview {
		bindings = append(bindings, helpBinding("enter", "choose destination", "enter"))
	}
	switch model.browserArea {
	case AreaProfiles:
		bindings = append(bindings,
			helpBinding("n", "new profile", "n"),
			helpBinding("p", "copy profile", "p"),
		)
	case AreaParties:
		bindings = append(bindings, helpBinding("n", "new party", "n"))
	case AreaReviews:
		bindings = append(bindings,
			helpBinding("a", "add review", "a"),
			helpBinding("r", "remove review", "r"),
			helpBinding("m", "move review", "m"),
			helpBinding("c", "set concurrency", "c"),
		)
	case AreaOverview, AreaChanges, areaSearch, areaCopyProfile:
	}
	return bindings
}

func menuHelpBindings(searching bool) []bubbleskey.Binding {
	if searching {
		return []bubbleskey.Binding{
			helpBinding("enter", "accept search", "enter"),
			helpBinding("backspace", "delete character", "backspace"),
			helpBinding("esc", "clear search", "esc"),
			helpBinding("ctrl+c", "quit", "ctrl+c"),
		}
	}
	return []bubbleskey.Binding{
		helpBinding("↑/↓", "navigate", "up", "down", "j", "k"),
		helpBinding("enter", "open", "enter"),
		helpBinding("/", "search", "/"),
		helpBinding("esc", "clear search", "esc"),
		helpBinding("q", "quit", "q"),
		helpBinding("ctrl+c", "quit", "ctrl+c"),
	}
}

func (model Model) renderHelpOverlay(options renderOptions) string {
	helpView := model.helpView
	helpView.ShowAll = true
	helpView.SetWidth(0)
	content := helpView.View(model.helpKeyMap())
	if !options.styled {
		content = stripANSI(content)
	}
	title := "Help"
	if options.styled {
		title = sectionTitleStyle.Render(title)
	}
	return renderSizedBox(title, content, max(model.width, minBoxWidth), "")
}
