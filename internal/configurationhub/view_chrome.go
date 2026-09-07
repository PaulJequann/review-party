package configurationhub

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

func (model Model) renderPlanPreviewFrame(options renderOptions) string {
	if model.showHelp {
		return model.renderViewportFrame(model.renderHelpOverlay(options), options, model.renderPlanActionBar(options))
	}
	var warnings strings.Builder
	for _, warning := range model.planWarnings {
		line := "▲ " + warning
		if options.styled {
			line = warningStyle.Render(line)
		}
		warnings.WriteString(line + "\n")
	}
	content := model.planSummary
	title := "Plan preview"
	if options.styled {
		title = sectionTitleStyle.Render(title)
	}
	if warnings.Len() > 0 {
		content = strings.TrimSuffix(warnings.String(), "\n") + "\n\n" + content
	}
	return model.renderViewportFrame(renderSizedBox(title, content, max(model.width, minBoxWidth), ""), options, model.renderPlanActionBar(options))
}

func (model Model) renderPlanActionBar(options renderOptions) string {
	bar := renderActionHints(options, []actionHint{{key: "p", label: "publish"}, {key: "e", label: "revise"}, {key: "esc", label: "cancel"}})
	return model.renderActionBarWithOutcome(bar, options)
}

func (model Model) renderMenuFrame(options renderOptions) string {
	body := model.renderMenuBody(options, "")
	if model.showHelp {
		body = model.renderHelpOverlay(options)
	}
	return model.renderViewportFrame(body, options)
}

func (model Model) renderHeader(options renderOptions) string {
	lines := []string{model.renderTitleRow(options)}
	if breadcrumb := model.renderBreadcrumb(options); breadcrumb != "" {
		lines = append(lines, breadcrumb)
	}
	if model.status != "" {
		lines = append(lines, model.renderStatus(options))
	}
	if model.query != "" || model.searching {
		lines = append(lines, "", model.renderSearchLine(options))
	}
	return strings.Join(lines, "\n")
}

func (model Model) renderBreadcrumb(options renderOptions) string {
	area := model.focusedArea()
	if area == "" {
		return ""
	}
	line := "Hub › " + string(area)
	if options.styled {
		return metaStyle.Render(line)
	}
	return line
}

func (model Model) focusedArea() Area {
	if model.view == viewMenu {
		return ""
	}
	if model.browserArea != "" {
		return model.browserArea
	}
	if area := planArea(model.pendingKind); area != "" {
		return area
	}
	return formArea(model.formKind)
}

func planArea(kind planKind) Area {
	switch kind {
	case planProfile, planCopy:
		return AreaProfiles
	case planParty:
		return AreaParties
	case planReviews:
		return AreaReviews
	default:
		return ""
	}
}

func formArea(kind formKind) Area {
	switch kind {
	case formProfileFields, formProfileSource, formProfileTemplate, formProfileTemplateLoading, formProfileInstructions, formEditor:
		return AreaProfiles
	case formParty:
		return AreaParties
	case formReviewOperation, formReviewLoading, formReviewFields:
		return AreaReviews
	case formCopy:
		return AreaProfiles
	case formOverview:
		return AreaOverview
	case formChanges:
		return AreaChanges
	case formNone, formPlanning:
		return ""
	default:
		return ""
	}
}

func (model Model) publishedTarget(kind planKind) string {
	switch kind {
	case planProfile:
		return fmt.Sprintf("profile %q", model.drafts.profile.Name)
	case planParty:
		return fmt.Sprintf("party %q", model.drafts.party.name)
	case planCopy:
		return fmt.Sprintf("profile %q", model.drafts.copyName)
	case planReviews:
		return "Repository Reviews"
	default:
		return "configuration"
	}
}

func (model Model) renderActionBar(options renderOptions) string {
	bar := renderActionHints(options, model.actionHints())
	if model.view == viewBrowser && !model.drafts.empty() {
		drafts := "⏸ drafts"
		if options.styled {
			drafts = warningStyle.Render(drafts)
		}
		bar += "  " + drafts
	}
	return model.renderActionBarWithOutcome(bar, options)
}

func (model Model) actionHints() []actionHint {
	if model.view != viewBrowser {
		return []actionHint{
			{key: "↑/↓", label: "navigate"},
			{key: "⏎", label: "open"},
			{key: "/", label: "search"},
			{key: "esc", label: "clear"},
			{key: "q", label: "quit"},
		}
	}
	hints := []actionHint{{key: "↑/↓", label: "navigate"}}
	switch model.browserArea {
	case AreaProfiles:
		hints = append(hints, actionHint{key: "n", label: "new"}, actionHint{key: "p", label: "copy"})
	case AreaParties:
		hints = append(hints, actionHint{key: "n", label: "new"})
	case AreaReviews:
		hints = append(hints, actionHint{key: "a", label: "add"}, actionHint{key: "r", label: "remove"}, actionHint{key: "m", label: "move"}, actionHint{key: "c", label: "concurrency"})
	case AreaOverview, AreaChanges, areaSearch, areaCopyProfile:
	}
	return append(hints,
		actionHint{key: "/", label: "filter"},
		actionHint{key: "esc", label: "back"},
		actionHint{key: "q", label: "quit"},
	)
}

func (model Model) renderActionBarWithOutcome(bar string, options renderOptions) string {
	if model.outcome != "" {
		bar = model.renderOutcome(options) + "  " + bar
	}
	return lipgloss.Wrap(bar, max(model.width, 1), " ")
}

func (model Model) renderOutcome(options renderOptions) string {
	line := "✗ " + model.outcome
	style := dangerStyle
	if model.outcomeGood {
		line = "✓ " + model.outcome
		style = successStyle
	}
	if options.styled {
		return style.Render(line)
	}
	return line
}

func (model Model) renderFooter(options renderOptions) string {
	footer := "? help"
	if options.styled {
		return metaStyle.Render(footer)
	}
	return footer
}

type actionHint struct {
	key   string
	label string
}

func renderActionHints(options renderOptions, hints []actionHint) string {
	parts := make([]string, 0, len(hints))
	for _, hint := range hints {
		key := hint.key
		if options.styled {
			key = renderActionKey(key)
		}
		parts = append(parts, key+" "+hint.label)
	}
	return strings.Join(parts, "  ")
}

func (model Model) renderForm() string {
	return stripANSI(model.renderFormFrame(renderOptions{styled: true}))
}

func (model Model) renderFormFrame(options renderOptions) string {
	content := model.form.View()
	if content == "" {
		switch model.formKind {
		case formPlanning:
			content = "Planning configuration changes..."
		case formEditor:
			content = "Editing instructions..."
		case formNone, formOverview, formProfileFields, formProfileSource, formProfileTemplate,
			formProfileTemplateLoading, formProfileInstructions, formParty, formReviewOperation,
			formReviewLoading, formReviewFields, formCopy, formChanges:
			// The active form supplies the content.
		}
	}
	if model.showHelp {
		content = model.renderHelpOverlay(options)
	}
	return model.renderViewportFrame(content, options, model.renderFormActionBar(options))
}

func (model Model) renderFormActionBar(options renderOptions) string {
	bar := renderActionHints(options, []actionHint{
		{key: "esc", label: "back"},
		{key: "ctrl+c", label: "quit"},
	})
	return model.renderActionBarWithOutcome(bar, options)
}
