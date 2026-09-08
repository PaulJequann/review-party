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

	bubbleshelp "charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"reviewparty/internal/configuration"
)

// Area is a stable Hub destination.
type Area string

const (
	AreaOverview    Area = "Overview"
	AreaProfiles    Area = "Profiles"
	AreaParties     Area = "Parties"
	AreaReviews     Area = "Repository Reviews"
	AreaChanges     Area = "Review Changes"
	areaCopyProfile Area = "Copy Profile"
	areaSearch      Area = "Search"
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
		{area: AreaChanges, description: "Review or discard unfinished drafts.", menu: true, action: (*editor).reviewDrafts, render: renderActionArea},
		// Unlisted: reachable from the accessible-mode action select, not the menu.
		{area: areaCopyProfile, description: "Copy a Repository Profile to Global Configuration.", action: (*editor).copyProfile, render: renderActionArea},
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
	viewPlanPreview
)

const menuGutterWidth = 2

// Model is a Bubble Tea shell. It deliberately owns no filesystem handle and
// cannot publish configuration while a user merely navigates or searches.
type Model struct {
	snapshot     Snapshot
	area         int
	browserArea  Area
	cursor       int
	moveReview   bool
	moveFrom     int
	rowReview    bool
	query        string
	searching    bool
	width        int
	height       int
	viewport     viewport.Model
	view         viewState
	drafts       draftSet
	form         formAdapter
	formKind     formKind
	session      *formSession
	status       string
	outcome      string
	outcomeGood  bool
	planSummary  string
	planWarnings []string
	pending      tea.Cmd
	pendingKind  planKind
	runtime      *hubRuntime
	helpView     bubbleshelp.Model
	showHelp     bool
}

func New(snapshot Snapshot) Model {
	view := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	model := Model{
		snapshot: snapshot,
		width:    80,
		height:   20,
		viewport: view,
		view:     viewMenu,
		session:  &formSession{},
		helpView: newHubHelp(),
	}
	model.refreshViewport()
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
		return refreshHubModel(updated), command
	}
	if key, ok := message.(tea.KeyPressMsg); ok {
		updated, command := model.updateKey(key)
		return refreshHubModel(updated), command
	}
	if model.view == viewForm {
		updated, command := model.updateForm(message)
		return refreshHubModel(updated), command
	}
	return model, nil
}

func isCtrlC(message tea.Msg) bool {
	key, ok := message.(tea.KeyPressMsg)
	return ok && key.String() == "ctrl+c"
}

func (model Model) updateWindow(message tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	model.width = max(message.Width, 1)
	model.height = max(message.Height, 1)
	if model.view == viewForm {
		updated, command := model.updateForm(message)
		return refreshHubModel(updated), command
	}
	model.refreshViewport()
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
	if model.outcome != "" {
		model.outcome = ""
	}
	if message.String() == "?" {
		model.showHelp = !model.showHelp
		return model, nil
	}
	if model.view == viewPlanPreview {
		return model.updatePlanPreviewKey(message.String())
	}
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

func (model Model) updatePlanPreviewKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "p":
		if model.pending == nil {
			model.status = "No publish command is available."
			return model, nil
		}
		command := model.pending
		model.pending = nil
		return model, command
	case "e":
		return model.reopenPlan(model.pendingKind)
	case "esc":
		model.toMenu()
		return model, nil
	}
	return model, nil
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
	if model.browserArea != AreaProfiles {
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
	model.viewport.GotoTop()
	return *model, nil
}

func (model *Model) beginSearch() {
	model.area = 0
	model.query = ""
	model.searching = true
	model.viewport.GotoTop()
}

func (model *Model) clearSearch() {
	model.query = ""
	model.viewport.GotoTop()
}

func (model *Model) moveArea(offset int) {
	areas := menuAreaSpecs()
	next := model.area + offset
	if next >= 0 && next < len(areas) {
		model.area = next
		model.viewport.GotoTop()
	}
}

func (model Model) View() tea.View {
	var content string
	switch model.view {
	case viewForm:
		content = model.renderFormFrame(renderOptions{styled: true})
	case viewPlanPreview:
		content = model.renderPlanPreviewFrame(renderOptions{styled: true})
	case viewMenu, viewBrowser:
		model.refreshViewport()
		content = model.renderViewportFrame(model.renderViewportWindow(), renderOptions{styled: true})
	}
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "Review Party Configuration Hub"
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
	if model.view == viewPlanPreview {
		return stripANSI(model.renderPlanPreviewFrame(renderOptions{styled: true}))
	}
	return stripANSI(model.renderMenuFrame(renderOptions{}))
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
	return lipgloss.Wrap(title+"  "+badge+" "+repository, max(model.width, 1), " ")
}

func (model Model) renderMenuRow(index int, spec areaSpec, rowWidth int, options renderOptions) string {
	label := string(spec.area)
	indicator := model.menuDraftIndicator(spec, options)
	row := model.menuCursor(index, options) + model.menuLabel(index, label, options) + indicator
	if count := model.menuAreaCount(spec); count > 0 {
		text := fmt.Sprintf("%d", count)
		gap := max(rowWidth-menuGutterWidth-lipgloss.Width(label+indicator)-lipgloss.Width(text), 2)
		row += strings.Repeat(" ", gap) + model.menuCountLabel(text, options)
	}
	return row
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
		return 0 // the overview pane is contextual, not a count
	case areaCopyProfile:
		return 0 // unlisted from the menu; never counted
	case AreaProfiles:
		return model.countItems(itemProfile)
	case AreaParties:
		return model.countItems(itemParty)
	case AreaReviews:
		return model.countItems(itemReview)
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

func (model Model) renderSearchLine(options renderOptions) string {
	line := "Search: " + model.query
	if options.styled {
		line = metaStyle.Render("Search: ") + focusStyle.Render(model.query)
	} else {
		return lipgloss.Wrap(line, max(model.width, 1), " ")
	}
	return lipgloss.Wrap(line, max(model.width, 1), " ")
}

func (model Model) renderViewportBody(options renderOptions, footer string) string {
	if model.showHelp {
		return model.renderHelpOverlay(options)
	}
	switch model.view {
	case viewMenu:
		return model.renderMenuBody(options, footer)
	case viewBrowser:
		return model.renderBrowserBody(options, footer)
	case viewForm, viewPlanPreview:
		return ""
	}
	return ""
}

func (model Model) renderMenuBody(options renderOptions, footer string) string {
	width := max(model.width, minBoxWidth)
	if model.query != "" || model.searching {
		return model.renderMenuSearchPane(width, options, footer)
	}
	layout := newResponsiveLayout(width)
	return layout.render(responsivePanes{
		left:  responsivePane{content: model.renderMenuPane(layout.leftWidth, options, footer)},
		right: responsivePane{content: model.renderMenuContextPane(layout.rightWidth, options)},
	})
}

func (model Model) renderMenuPane(width int, options renderOptions, footer string) string {
	var content strings.Builder
	rowWidth := max(width-4, 1)
	for index, spec := range menuAreaSpecs() {
		content.WriteString(model.renderMenuRow(index, spec, rowWidth, options))
		content.WriteString("\n")
	}
	title := "Menu"
	if options.styled {
		title = sectionTitleStyle.Render(title)
	}
	return renderSizedBox(title, content.String(), width, footer)
}

func (model Model) renderMenuContextPane(width int, options renderOptions) string {
	areas := menuAreaSpecs()
	if model.area < 0 || model.area >= len(areas) {
		return renderSizedBox("Overview", "", width, "")
	}
	spec := areas[model.area]
	title := string(spec.area)
	if options.styled {
		title = sectionTitleStyle.Render(title)
	}
	return renderSizedBox(title, model.menuContextContent(spec, options), width, "")
}

func (model Model) menuContextContent(spec areaSpec, options renderOptions) string {
	content := spec.description
	switch {
	case spec.area == AreaOverview:
		if len(model.snapshot.Overview) > 0 || len(model.snapshot.Warnings) > 0 {
			content += "\n\n" + model.overviewContext(options)
		}
	case spec.kind != "":
		content += "\n\nInventory\n" + model.inventoryContext(spec.kind, options)
	}
	return content
}

func (model Model) overviewContext(options renderOptions) string {
	var content strings.Builder
	model.renderOverview(&content, options)
	return content.String()
}

func (model Model) inventoryContext(kind ItemKind, options renderOptions) string {
	items := model.visibleItems(kind)
	if len(items) == 0 {
		return "No matching items.\n"
	}
	var content strings.Builder
	for _, item := range items {
		content.WriteString(renderInventoryItem(item, options))
		content.WriteString("\n")
	}
	return content.String()
}

func (model Model) renderMenuSearchPane(width int, options renderOptions, footer string) string {
	var content strings.Builder
	model.renderSearchResults(&content, options)
	title := "Search"
	if options.styled {
		title = sectionTitleStyle.Render(title)
	}
	return renderSizedBox(title, content.String(), width, footer)
}

func (model Model) renderBrowserBody(options renderOptions, footer string) string {
	width := max(model.width, minBoxWidth)
	spec, ok := areaSpecFor(model.browserArea)
	if !ok {
		return ""
	}
	if spec.kind == "" {
		var content strings.Builder
		if spec.render != nil {
			spec.render(model, spec, &content, options)
		}
		title := string(model.browserArea)
		if options.styled {
			title = sectionTitleStyle.Render(title)
		}
		return renderSizedBox(title, content.String(), width, footer)
	}
	layout := newResponsiveLayout(width)
	return layout.render(responsivePanes{
		left:  responsivePane{content: model.renderBrowserListPane(spec, layout.leftWidth, options, footer)},
		right: responsivePane{content: model.renderBrowserDetailPane(spec, layout.rightWidth, options)},
	})
}

func (model Model) renderBrowserListPane(spec areaSpec, width int, options renderOptions, footer string) string {
	var content strings.Builder
	items := model.visibleItems(spec.kind)
	if len(items) == 0 {
		content.WriteString("No matching items.\n")
	} else if model.browserArea == AreaReviews {
		renderReviewRows(model, items, &content, options)
	} else {
		renderRows(model, items, &content, options)
	}
	title := string(spec.area)
	if options.styled {
		title = sectionTitleStyle.Render(title)
	}
	return renderSizedBox(title, content.String(), width, footer)
}

func (model Model) renderBrowserDetailPane(spec areaSpec, width int, options renderOptions) string {
	items := model.visibleItems(spec.kind)
	var content strings.Builder
	if len(items) == 0 {
		content.WriteString("No row selected.")
	} else {
		item := items[min(model.cursor, len(items)-1)]
		content.WriteString(renderInventoryItem(item, options))
		if item.Kind == itemProfile || item.Kind == itemParty {
			for _, line := range model.snapshot.Overview {
				content.WriteString("\n")
				content.WriteString(line)
			}
		}
	}
	return renderSizedBox("Detail", content.String(), width, "")
}

func (model Model) menuCursorLine() int {
	width := max(model.width, minBoxWidth)
	innerWidth := max(width-4, 1)
	line := 1
	for index, spec := range menuAreaSpecs() {
		if index == model.area {
			return line
		}
		line += wrappedLineCount(model.renderMenuRow(index, spec, innerWidth, renderOptions{styled: true}), innerWidth)
	}
	return line
}

func (model Model) browserCursorLine() int {
	spec, ok := areaSpecFor(model.browserArea)
	if !ok || spec.kind == "" {
		return 0
	}
	items := model.visibleItems(spec.kind)
	innerWidth := max(newResponsiveLayout(max(model.width, minBoxWidth)).leftWidth-4, 1)
	if model.browserArea == AreaReviews {
		return model.reviewCursorLine(items, innerWidth)
	}
	return model.itemCursorLine(items, innerWidth)
}

func (model Model) itemCursorLine(items []Item, width int) int {
	line := 1
	for index, item := range items {
		if index == model.cursor {
			return line
		}
		line += wrappedLineCount(renderRow(model, item, index, renderOptions{styled: true}), width)
	}
	return line
}

func (model Model) reviewCursorLine(items []Item, width int) int {
	line := 1
	for _, scope := range []ItemScope{"global", "repository"} {
		line++
		for index, item := range items {
			if item.Scope != scope {
				continue
			}
			if index == model.cursor {
				return line
			}
			line += wrappedLineCount(renderRow(model, item, index, renderOptions{styled: true}), width)
		}
	}
	return line
}

func wrappedLineCount(content string, width int) int {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return 1
	}
	return lipgloss.Height(lipgloss.Wrap(content, max(width, 1), " "))
}

func (model Model) renderBrowserFrame(options renderOptions) string {
	body := model.renderBrowserBody(options, "")
	if model.showHelp {
		body = model.renderHelpOverlay(options)
	}
	return model.renderViewportFrame(body, options)
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
		label += " selection"
		if options.styled {
			label = sectionTitleStyle.Render(label)
		}
		fmt.Fprintf(output, "%s\n", label)
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
		line = style.Render(line)
	}
	return lipgloss.Wrap(line, max(model.width, 1), " ")
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
	if err := e.refresh(); err != nil {
		return hubAction{}, err
	}
	model := New(e.snapshot)
	if e.drafts != nil {
		model.drafts = *e.drafts
	}
	if _, err := io.WriteString(e.Output, model.Render()); err != nil {
		return hubAction{}, err
	}
	return e.chooseAction()
}
