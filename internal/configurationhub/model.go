// Package configurationhub implements the recurring terminal shell for
// inspecting and editing Review Party configuration. Publication remains owned
// by configuration.Manager; this package keeps UI state in memory.
package configurationhub

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"reviewparty/internal/configuration"
)

// Area is a stable Hub destination.
type Area string

const (
	AreaOverview Area = "Overview"
	AreaProfiles Area = "Profiles"
	AreaParties  Area = "Parties"
	AreaReviews  Area = "Repository Reviews"
	AreaAdvanced Area = "Advanced"
	AreaChanges  Area = "Review Changes"
	areaSearch   Area = "Search"
)

// ItemKind identifies one Hub inventory category.
type ItemKind string

const (
	itemTemplate ItemKind = "Template"
	itemProfile  ItemKind = "Profile"
	itemParty    ItemKind = "Party"
	itemReview   ItemKind = "Review"
)

// ItemScope labels the source of one Hub inventory item.
type ItemScope string

// Item is one searchable, explicitly scoped configuration value.
type Item struct {
	Scope  ItemScope
	Kind   ItemKind
	Name   string
	Detail string
}

// Snapshot is the read-only material shown by the shell.
type Snapshot struct {
	Repository string
	Items      []Item
	Overview   []string
	Warnings   []string
}

type areaRenderer func(Model, areaSpec, *strings.Builder, renderOptions)

type renderOptions struct {
	styled bool
}

type areaSpec struct {
	area        Area
	kind        ItemKind
	description string
	menu        bool
	action      func(*editor) error
	render      areaRenderer
}

func newAreaSpecs() []areaSpec {
	return []areaSpec{
		{area: AreaOverview, description: "See configuration counts, resolved reviews, and warnings.", menu: true, render: renderOverviewArea},
		{area: AreaProfiles, kind: itemProfile, description: "Browse Profiles and create or copy one.", menu: true, action: (*editor).createProfile, render: renderInventoryArea},
		{area: AreaParties, kind: itemParty, description: "Browse flat Parties and create one.", menu: true, action: (*editor).createParty, render: renderInventoryArea},
		{area: AreaReviews, kind: itemReview, description: "Assemble the ordered Repository Review selection.", menu: true, action: (*editor).editReviews, render: renderInventoryArea},
		{area: AreaAdvanced, description: "Copy a Repository Profile to Global Configuration.", menu: true, action: (*editor).copyProfile, render: renderActionArea},
		{area: AreaChanges, description: "Review or discard unfinished drafts.", menu: true, action: (*editor).reviewDrafts, render: renderActionArea},
		{area: areaSearch, action: (*editor).search, render: renderSearchArea},
	}
}

func menuAreaSpecs() []areaSpec {
	all := newAreaSpecs()
	result := make([]areaSpec, 0, len(all))
	for _, spec := range all {
		if spec.menu {
			result = append(result, spec)
		}
	}
	return result
}

func areaSpecFor(area Area) (areaSpec, bool) {
	for _, spec := range newAreaSpecs() {
		if spec.area == area {
			return spec, true
		}
	}
	return areaSpec{}, false
}

type hubAction struct {
	area Area
	exit bool
}

// viewState identifies the active surface in the long-lived Hub program.
type viewState uint8

const (
	viewMenu viewState = iota
	viewForm
	viewBrowser
)

const menuGutterWidth = 2

// Model is a Bubble Tea shell. It deliberately owns no filesystem handle and
// cannot publish configuration while a user merely navigates or searches.
type Model struct {
	snapshot    Snapshot
	area        int
	browserArea Area
	cursor      int
	moveReview  bool
	moveFrom    int
	rowReview   bool
	query       string
	searching   bool
	width       int
	height      int
	viewport    viewport.Model
	ready       bool
	view        viewState
	drafts      draftSet
	form        formAdapter
	formKind    formKind
	session     *formSession
	status      string
	pending     tea.Cmd
	pendingKind planKind
	runtime     *hubRuntime
}

func New(snapshot Snapshot) Model {
	view := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	model := Model{snapshot: snapshot, width: 80, height: 20, viewport: view, view: viewMenu, session: &formSession{}}
	model.viewport.SetContent(model.viewportContent())
	return model
}
func (Model) Init() tea.Cmd { return nil }

func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	model.ensureSession()
	if isCtrlC(message) {
		return model, tea.Quit
	}
	if size, ok := message.(tea.WindowSizeMsg); ok {
		return model.updateWindow(size)
	}
	if updated, command, handled := model.updateAsync(message); handled {
		return updated, command
	}
	if key, ok := message.(tea.KeyPressMsg); ok {
		return model.updateKey(key)
	}
	if model.view == viewForm {
		return model.updateForm(message)
	}
	return model, nil
}

func isCtrlC(message tea.Msg) bool {
	key, ok := message.(tea.KeyPressMsg)
	return ok && key.String() == "ctrl+c"
}

func (model Model) updateWindow(message tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	model.width = message.Width
	model.height = message.Height
	if model.view == viewForm {
		return model.updateForm(message)
	}
	model.viewport.SetWidth(message.Width)
	// Keep the menu header outside the viewport. Measure the rendered sections
	// so scrolling cannot duplicate the header or consume an accidental line.
	options := renderOptions{styled: true}
	headerHeight := lipgloss.Height(model.renderHeader(options)) + 1
	footerHeight := lipgloss.Height(model.renderActionBar(options))
	viewportHeight := message.Height - headerHeight - footerHeight - 1
	viewportHeight = max(viewportHeight, 1)
	model.viewport.SetHeight(viewportHeight)
	model.viewport.SetContent(model.viewportContent())
	model.ready = true
	// Also forward to viewport for internal offset handling
	var command tea.Cmd
	model.viewport, command = model.viewport.Update(message)
	return model, command
}

func (model Model) updateAsync(message tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch message := message.(type) {
	case planReadyMsg:
		model, command := model.receivePlan(message)
		return model, command, true
	case planFailedMsg:
		model, command := model.receivePlanFailure(message)
		return model, command, true
	case publishResultMsg:
		model, command := model.receivePublishResult(message)
		return model, command, true
	case reviewSelectionLoadedMsg:
		model, command := model.receiveReviewSelection(message)
		return model, command, true
	case templateLoadedMsg:
		model, command := model.receiveTemplate(message)
		return model, command, true
	case instructionEditResultMsg:
		model, command := model.receiveInstructionEdit(message)
		return model, command, true
	default:
		return model, nil, false
	}
}

func (model Model) updateKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if model.view == viewForm {
		return model.updateForm(message)
	}
	if model.searching {
		return model.updateSearch(message.String()), nil
	}
	if model.view == viewBrowser {
		return model.updateBrowser(message.String())
	}
	return model.updateNavigation(message.String())
}

func (model Model) updateBrowser(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		return model, tea.Quit
	case "up", "k", "down", "j":
		model.moveBrowserCursor(key)
	case "esc":
		model.toMenu()
	case "/":
		model.beginSearch()
	case "enter":
		if model.moveReview {
			model.moveReview = false
			return model.startRowReview("move")
		}
	default:
		return model.browserAction(key)
	}
	model.viewport.SetContent(model.browserContent(renderOptions{styled: true}))
	return model, nil
}

func (model Model) browserAction(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "n":
		return model.browserNew()
	case "p":
		return model.browserCopy()
	case "a":
		return model.browserReviewForm("add")
	default:
		return model.browserReviewAction(key)
	}
}

func (model Model) browserReviewAction(key string) (tea.Model, tea.Cmd) {
	if model.browserArea != AreaReviews {
		return model, nil
	}
	switch key {
	case "r":
		return model.startRowReview("remove")
	case "m":
		model.moveFrom = model.currentReviewIndex()
		model.moveReview = true
		model.status = "Choose a destination, then press enter."
	case "c":
		return model.browserReviewForm("concurrency")
	}
	return model, nil
}

func (model *Model) moveBrowserCursor(key string) {
	items := model.visibleItems(model.browserKind())
	if len(items) == 0 {
		return
	}
	delta := 1
	if key == "up" || key == "k" {
		delta = -1
	}
	model.cursor = max(0, min(len(items)-1, model.cursor+delta))
}

func (model Model) browserNew() (tea.Model, tea.Cmd) {
	if model.browserArea != AreaProfiles && model.browserArea != AreaParties {
		return model, nil
	}
	return model.withForm(model.openArea(model.browserArea))
}

func (model Model) browserCopy() (tea.Model, tea.Cmd) {
	if model.browserArea != AreaProfiles && model.browserArea != AreaAdvanced {
		return model, nil
	}
	return model.withForm(model.openCopyForm())
}

func (model Model) browserReviewForm(operation string) (tea.Model, tea.Cmd) {
	if model.browserArea != AreaReviews {
		return model, nil
	}
	return model.withForm(model.openReviewOperation(operation))
}

func (model Model) updateSearch(key string) Model {
	switch key {
	case "esc":
		model.searching = false
		model.query = ""
	case "enter":
		model.searching = false
	case "backspace":
		runes := []rune(model.query)
		if len(runes) > 0 {
			model.query = string(runes[:len(runes)-1])
		}
	default:
		if len([]rune(key)) == 1 {
			model.query += key
		}
	}
	model.viewport.SetContent(model.viewportContent())
	model.viewport.GotoTop()
	return model
}

func (model Model) updateNavigation(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		return model, tea.Quit
	case "up", "k", "down", "j":
		model.moveAreaForKey(key)
	case "enter":
		return model.openSelectedArea()
	case "/":
		model.beginSearch()
	case "esc":
		model.clearSearch()
	}
	return model, nil
}

func (model *Model) moveAreaForKey(key string) {
	offset := 1
	if key == "up" || key == "k" {
		offset = -1
	}
	model.moveArea(offset)
}

func (model *Model) openSelectedArea() (tea.Model, tea.Cmd) {
	areas := menuAreaSpecs()
	if model.area < 0 || model.area >= len(areas) {
		return *model, nil
	}
	model.browserArea = areas[model.area].area
	model.cursor = 0
	model.view = viewBrowser
	model.searching = false
	model.query = ""
	model.viewport.SetContent(model.browserContent(renderOptions{styled: true}))
	return *model, nil
}

func (model *Model) beginSearch() {
	model.area = 0
	model.query = ""
	model.searching = true
	model.viewport.SetContent(model.viewportContent())
	model.viewport.GotoTop()
}

func (model *Model) clearSearch() {
	model.query = ""
	model.viewport.SetContent(model.viewportContent())
	model.viewport.GotoTop()
}

func (model *Model) moveArea(offset int) {
	areas := menuAreaSpecs()
	next := model.area + offset
	if next >= 0 && next < len(areas) {
		model.area = next
		model.viewport.SetContent(model.viewportContent())
		model.viewport.GotoTop()
	}
}

func (model Model) viewportContent() string {
	return model.renderSelectedArea(renderOptions{styled: true})
}

func (model Model) View() tea.View {
	var content string
	switch {
	case model.view == viewForm:
		content = model.renderFormFrame(renderOptions{styled: true})
	case model.view == viewBrowser:
		content = model.renderBrowserFrame(renderOptions{styled: true})
	case !model.ready:
		content = model.renderMenuFrame(renderOptions{styled: true})
	default:
		content = lipgloss.JoinVertical(
			lipgloss.Left,
			model.renderHeader(renderOptions{styled: true}),
			"",
			model.viewport.View(),
			"",
			model.renderActionBar(renderOptions{styled: true}),
		)
	}
	view := tea.NewView(content)
	view.AltScreen = true
	return view
}

// Render returns a deterministic text view and is also used by accessible mode.
func (model Model) Render() string {
	if model.view == viewForm {
		return model.renderForm()
	}
	if model.view == viewBrowser {
		return stripANSI(model.renderBrowserFrame(renderOptions{}))
	}
	return stripANSI(model.renderMenuFrame(renderOptions{}))
}

func (model Model) renderMenuFrame(options renderOptions) string {
	header := model.renderHeader(options)
	body := model.renderSelectedArea(options)
	actionBar := model.renderActionBar(options)
	if options.styled {
		return lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", actionBar)
	}
	return strings.Join([]string{header, body, actionBar}, "\n\n")
}

func (model Model) renderHeader(options renderOptions) string {
	lines := []string{model.renderTitleRow(options)}
	if model.status != "" {
		lines = append(lines, model.renderStatus(options))
	}
	if model.query != "" || model.searching {
		lines = append(lines, "", model.renderSearchLine(options))
	}
	return strings.Join(lines, "\n")
}

func (model Model) renderTitleRow(options renderOptions) string {
	title := "Review Party Configuration Hub"
	badge := "[repository]"
	repository := model.snapshot.Repository
	if options.styled {
		title = sectionTitleStyle.Render(title)
		badge = repositoryScopeStyle().Render(badge)
		repository = metaStyle.Render(repository)
	}
	return title + "  " + badge + " " + repository
}

func (model Model) renderMenuRow(index int, spec areaSpec, rowWidth int, options renderOptions) string {
	label := string(spec.area)
	indicator := model.menuDraftIndicator(spec, options)
	count := fmt.Sprintf("%d", model.menuAreaCount(spec))
	gap := max(rowWidth-menuGutterWidth-lipgloss.Width(label+indicator)-lipgloss.Width(count), 2)
	return model.menuCursor(index, options) + model.menuLabel(index, label, options) + indicator +
		strings.Repeat(" ", gap) + model.menuCountLabel(count, options)
}

func (model Model) menuDraftIndicator(spec areaSpec, options renderOptions) string {
	if spec.area != AreaChanges || model.drafts.empty() {
		return ""
	}
	if options.styled {
		return " " + warningStyle.Render("⏸")
	}
	return " ⏸"
}

func (model Model) menuCursor(index int, options renderOptions) string {
	if index != model.area {
		return "  "
	}
	if options.styled {
		return focusStyle.Render("› ")
	}
	return "› "
}

func (model Model) menuLabel(index int, label string, options renderOptions) string {
	if options.styled && index == model.area {
		return focusStyle.Render(label)
	}
	return label
}

func (model Model) menuCountLabel(count string, options renderOptions) string {
	if options.styled {
		return metaStyle.Render(count)
	}
	return count
}

func (model Model) menuAreaCount(spec areaSpec) int {
	switch spec.area {
	case AreaOverview:
		return len(model.snapshot.Items)
	case AreaProfiles:
		return model.countItems(itemProfile)
	case AreaParties:
		return model.countItems(itemParty)
	case AreaReviews:
		return model.countItems(itemReview)
	case AreaAdvanced:
		return model.countScopedItems(itemProfile, ItemScope("repository"))
	case AreaChanges:
		return len(model.drafts.descriptions())
	case areaSearch:
		return 0
	default:
		return 0
	}
}

func (model Model) countItems(kind ItemKind) int {
	count := 0
	for _, item := range model.snapshot.Items {
		if item.Kind == kind {
			count++
		}
	}
	return count
}

func (model Model) countScopedItems(kind ItemKind, scope ItemScope) int {
	count := 0
	for _, item := range model.snapshot.Items {
		if item.Kind == kind && item.Scope == scope {
			count++
		}
	}
	return count
}

func (model Model) renderSearchLine(options renderOptions) string {
	line := "Search: " + model.query
	if options.styled {
		return metaStyle.Render("Search: ") + focusStyle.Render(model.query)
	}
	return line
}

func (model Model) renderActionBar(options renderOptions) string {
	if model.view == viewBrowser {
		hints := []actionHint{{key: "↑/↓", label: "navigate"}}
		switch model.browserArea {
		case AreaProfiles:
			hints = append(hints, actionHint{key: "n", label: "new"}, actionHint{key: "p", label: "copy"})
		case AreaParties:
			hints = append(hints, actionHint{key: "n", label: "new"})
		case AreaReviews:
			hints = append(hints, actionHint{key: "a", label: "add"}, actionHint{key: "r", label: "remove"}, actionHint{key: "m", label: "move"}, actionHint{key: "c", label: "concurrency"})
		case AreaOverview, AreaAdvanced, AreaChanges, areaSearch:
		}
		hints = append(hints, actionHint{key: "/", label: "filter"}, actionHint{key: "esc", label: "back"}, actionHint{key: "q", label: "quit"})
		bar := renderActionHints(options, hints)
		if !model.drafts.empty() {
			bar += "  " + warningStyle.Render("⏸ drafts")
		}
		return bar
	}
	return renderActionHints(options, []actionHint{
		{key: "↑/↓", label: "navigate"},
		{key: "⏎", label: "open"},
		{key: "/", label: "search"},
		{key: "esc", label: "clear"},
		{key: "q", label: "quit"},
	})
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

func (model Model) renderSelectedArea(options renderOptions) string {
	if model.view == viewMenu {
		if model.query != "" {
			return model.renderMenuSearch(options)
		}
		return model.renderMenu(options)
	}
	areas := menuAreaSpecs()
	if model.area < 0 || model.area >= len(areas) {
		return ""
	}
	spec := areas[model.area]
	if spec.render == nil {
		return ""
	}
	var content strings.Builder
	spec.render(model, spec, &content, options)
	title := string(spec.area)
	if options.styled {
		title = sectionTitleStyle.Render(title)
	}
	return renderBox(title, content.String(), max(model.width, minBoxWidth), "")
}

func (model Model) renderMenuSearch(options renderOptions) string {
	var content strings.Builder
	model.renderSearchResults(&content, options)
	title := "Search"
	if options.styled {
		title = sectionTitleStyle.Render(title)
	}
	return renderBox(title, content.String(), max(model.width, minBoxWidth), "")
}

func (model Model) renderMenu(options renderOptions) string {
	var content strings.Builder
	for index, spec := range menuAreaSpecs() {
		fmt.Fprintf(&content, "%s\n   %s\n", model.renderMenuRow(index, spec, max(model.width, minBoxWidth), options), spec.description)
	}
	if model.area == 0 {
		model.renderOverview(&content, options)
	}
	title := "Areas"
	if options.styled {
		title = sectionTitleStyle.Render(title)
	}
	return renderBox(title, content.String(), max(model.width, minBoxWidth), "")
}

func (model Model) renderBrowserFrame(options renderOptions) string {
	return strings.Join([]string{model.renderHeader(options), model.browserContent(options), model.renderActionBar(options)}, "\n\n")
}

func (model Model) browserKind() ItemKind {
	spec, _ := areaSpecFor(model.browserArea)
	return spec.kind
}

func (model Model) currentReviewIndex() int {
	items := model.visibleItems(itemReview)
	if len(items) == 0 || model.cursor >= len(items) {
		return -1
	}
	index := 0
	for _, item := range items[:model.cursor] {
		if item.Scope == items[model.cursor].Scope {
			index++
		}
	}
	return index
}

func (model Model) browserContent(options renderOptions) string {
	spec, ok := areaSpecFor(model.browserArea)
	if !ok {
		return ""
	}
	var content strings.Builder
	if spec.kind != "" {
		renderInventoryBrowser(model, spec, &content, options)
	} else if spec.render != nil {
		spec.render(model, spec, &content, options)
	}
	title := string(model.browserArea)
	if options.styled {
		title = sectionTitleStyle.Render(title)
	}
	return renderBox(title, content.String(), max(model.width, minBoxWidth), "")
}

func renderInventoryBrowser(model Model, spec areaSpec, output *strings.Builder, options renderOptions) {
	items := model.visibleItems(spec.kind)
	if len(items) == 0 {
		output.WriteString("No matching items.\n")
		return
	}
	if model.browserArea == AreaReviews {
		renderReviewRows(model, items, output, options)
	} else {
		renderRows(model, items, output, options)
	}
	renderItemDetail(model, items[min(model.cursor, len(items)-1)], output, options)
}

func renderRows(model Model, items []Item, output *strings.Builder, options renderOptions) {
	for index, item := range items {
		fmt.Fprint(output, renderRow(model, item, index, options))
	}
}

func renderReviewRows(model Model, items []Item, output *strings.Builder, options renderOptions) {
	for _, scope := range []ItemScope{"global", "repository"} {
		label := "Global"
		if scope == ItemScope("repository") {
			label = "Repository"
		}
		fmt.Fprintf(output, "%s selection\n", label)
		for index, item := range items {
			if item.Scope == scope {
				fmt.Fprint(output, renderRow(model, item, index, options))
			}
		}
	}
}

func renderRow(model Model, item Item, index int, options renderOptions) string {
	cursor := "  "
	if index == model.cursor {
		cursor = "› "
	}
	return fmt.Sprintf("%s%s\n", cursor, renderInventoryItem(item, options))
}

func renderItemDetail(model Model, item Item, output *strings.Builder, options renderOptions) {
	output.WriteString("\nDetail\n")
	fmt.Fprintf(output, "%s\n", renderInventoryItem(item, options))
	if item.Kind == itemProfile || item.Kind == itemParty {
		for _, line := range model.snapshot.Overview {
			fmt.Fprintf(output, "%s\n", line)
		}
	}
}

func renderOverviewArea(model Model, _ areaSpec, output *strings.Builder, options renderOptions) {
	if model.query != "" {
		model.renderSearchResults(output, options)
		return
	}
	model.renderOverview(output, options)
}

func renderInventoryArea(model Model, spec areaSpec, output *strings.Builder, options renderOptions) {
	items := model.visibleItems(spec.kind)
	for _, item := range items {
		fmt.Fprintf(output, "  %s\n", renderInventoryItem(item, options))
	}
	if len(items) == 0 {
		output.WriteString("No matching items.\n")
	}
}

func renderInventoryItem(item Item, options renderOptions) string {
	badge := "[" + string(item.Scope) + "]"
	if options.styled {
		badge = renderScopeBadge(item.Scope)
	}
	line := badge + " " + item.Name
	if item.Detail == "" {
		return line
	}
	if strings.HasPrefix(strings.ToLower(item.Detail), "invalid:") {
		detail := "✗ " + item.Detail
		if options.styled {
			detail = dangerStyle.Render(detail)
		}
		return line + " " + detail
	}
	detail := " — " + item.Detail
	if options.styled {
		detail = metaStyle.Render(detail)
	}
	return line + detail
}

func renderScopeBadge(scope ItemScope) string {
	badge := "[" + string(scope) + "]"
	switch scope {
	case ItemScope("global"):
		return globalScopeStyle().Render(badge)
	case ItemScope("repository"):
		return repositoryScopeStyle().Render(badge)
	default:
		return metaStyle.Render(badge)
	}
}

func renderActionArea(_ Model, spec areaSpec, output *strings.Builder, _ renderOptions) {
	fmt.Fprintf(output, "%s\n", spec.description)
}

func renderSearchArea(model Model, _ areaSpec, output *strings.Builder, options renderOptions) {
	model.renderSearchResults(output, options)
}

func (model Model) renderSearchResults(output *strings.Builder, options renderOptions) {
	matched := 0
	for _, item := range model.snapshot.Items {
		if !matches(item, model.query) {
			continue
		}
		matched++
		fmt.Fprintf(output, "  %s\n", renderInventoryItem(item, options))
	}
	if matched == 0 {
		output.WriteString("No matching items.\n")
	}
}

func (model Model) renderOverview(output *strings.Builder, options renderOptions) {
	for _, line := range model.snapshot.Overview {
		fmt.Fprintf(output, "%s\n", line)
	}
	for _, warning := range model.snapshot.Warnings {
		warningLine := "▲ " + warning
		if options.styled {
			warningLine = warningStyle.Render(warningLine)
		}
		fmt.Fprintf(output, "%s\n", warningLine)
	}
}

func (model Model) renderStatus(options renderOptions) string {
	status := model.status
	line := "✗ configuration error: " + status
	style := dangerStyle
	if strings.HasPrefix(status, "Configuration published.") {
		line = "✓ " + status
		style = successStyle
	}
	if strings.HasPrefix(status, "No packaged") {
		line = "▲ " + status
		style = warningStyle
	}
	if options.styled {
		return style.Render(line)
	}
	return line
}

func (model Model) visibleItems(kind ItemKind) []Item {
	var visible []Item
	for _, item := range model.snapshot.Items {
		if itemVisibleInArea(item.Kind, kind) && matches(item, model.query) {
			visible = append(visible, item)
		}
	}
	return visible
}

func itemVisibleInArea(item, area ItemKind) bool {
	return item == area || (area == itemProfile && item == itemTemplate)
}

func matches(item Item, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	return query == "" || strings.Contains(strings.ToLower(string(item.Scope)+" "+item.Name+" "+item.Detail), query)
}

// RunOptions contains the terminal boundary required by the Hub.
type RunOptions struct {
	Context          context.Context
	Repository       configuration.Repository
	Input            io.ReadCloser
	Output           io.Writer
	Accessible       bool
	ModelChoiceCheck func(reviewer, model string) configuration.ModelChoiceCheck
}

// Run opens the terminal shell over a read-only Manager snapshot.
func Run(manager *configuration.Manager, options RunOptions) error {
	if options.Context == nil {
		options.Context = context.Background()
	}
	snapshot, err := buildSnapshot(manager, options.Repository)
	if err != nil {
		return err
	}
	if options.Accessible {
		model := New(snapshot)
		editor := &editor{RunOptions: options, manager: manager, drafts: &model.drafts, snapshot: snapshot}
		return editor.runHub()
	}
	model := New(snapshot)
	model.runtime = newHubRuntime(options, manager)
	result, err := tea.NewProgram(
		model,
		tea.WithContext(options.Context),
		tea.WithInput(options.Input),
		tea.WithOutput(options.Output),
	).Run()
	if err != nil {
		return err
	}
	if _, ok := result.(Model); !ok {
		return fmt.Errorf("Configuration Hub returned %T", result)
	}
	return nil
}

func (e *editor) runHub() error {
	for {
		exit, err := e.runHubStep()
		if err != nil {
			return err
		}
		if exit {
			return nil
		}
	}
}

func (e *editor) runHubStep() (bool, error) {
	action, err := e.nextAction()
	if errors.Is(err, huh.ErrUserAborted) {
		return e.abortExits(), nil
	}
	if err != nil {
		return false, err
	}
	resume, err := e.run(action)
	if errors.Is(err, huh.ErrUserAborted) {
		return e.abortExits(), nil
	}
	if err != nil {
		if _, writeErr := fmt.Fprintf(e.Output, "configuration error: %v\n", err); writeErr != nil {
			return false, writeErr
		}
		return false, nil
	}
	if resume {
		return false, nil
	}
	exit, err := e.confirmExit()
	return e.resolveExitConfirmation(exit, err)
}

func (e *editor) resolveExitConfirmation(exit bool, err error) (bool, error) {
	if errors.Is(err, huh.ErrUserAborted) {
		return e.abortExits(), nil
	}
	return exit, err
}

func (e *editor) abortExits() bool {
	return e.Accessible || e.Context.Err() != nil
}

func (e *editor) nextAction() (hubAction, error) {
	if !e.Accessible {
		return hubAction{}, errors.New("interactive Hub navigation must be hosted by the Configuration Hub")
	}
	snapshot, err := buildSnapshot(e.manager, e.Repository)
	if err != nil {
		return hubAction{}, err
	}
	model := New(snapshot)
	if e.drafts != nil {
		model.drafts = *e.drafts
	}
	if _, err := io.WriteString(e.Output, model.Render()); err != nil {
		return hubAction{}, err
	}
	return e.chooseAction()
}
