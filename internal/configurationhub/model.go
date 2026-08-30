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

type areaRenderer func(Model, areaSpec, *strings.Builder)

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
		{area: AreaOverview, menu: true, render: renderOverviewArea},
		{area: AreaProfiles, kind: itemProfile, menu: true, action: (*editor).createProfile, render: renderInventoryArea},
		{area: AreaParties, kind: itemParty, menu: true, action: (*editor).createParty, render: renderInventoryArea},
		{area: AreaReviews, kind: itemReview, menu: true, action: (*editor).editReviews, render: renderInventoryArea},
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
)

// Model is a Bubble Tea shell. It deliberately owns no filesystem handle and
// cannot publish configuration while a user merely navigates or searches.
type Model struct {
	snapshot    Snapshot
	area        int
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
	// Header ≈ title + global line + blank + 6 menu items + blank + optional search + footer
	headerHeight := 2 + len(menuAreaSpecs()) + 2
	if model.query != "" || model.searching {
		headerHeight++
	}
	footerHeight := 1
	viewportHeight := message.Height - headerHeight - footerHeight - 1
	if viewportHeight < 5 {
		viewportHeight = 5
	}
	if viewportHeight > message.Height-4 {
		viewportHeight = message.Height - 4
	}
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
	return model.updateNavigation(message.String())
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
	return model.withForm(model.openArea(areas[model.area].area))
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
	var builder strings.Builder
	model.renderArea(&builder)
	return builder.String()
}

func (model Model) View() tea.View {
	if model.view == viewForm || !model.ready {
		view := tea.NewView(model.Render())
		view.AltScreen = true
		return view
	}
	title := lipgloss.NewStyle().Bold(true).Render("Review Party Configuration Hub")
	var header strings.Builder
	fmt.Fprintf(&header, "%s\nGlobal Configuration · Repository %s\n\n", title, model.snapshot.Repository)
	for index, spec := range menuAreaSpecs() {
		marker := "  "
		if index == model.area {
			marker = "› "
		}
		fmt.Fprintf(&header, "%s%s\n", marker, spec.area)
	}
	header.WriteString("\n")
	if model.query != "" || model.searching {
		fmt.Fprintf(&header, "Search: %s\n", model.query)
	}
	view := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, header.String(), model.viewport.View(), "↑/↓ navigate  enter open  / search  esc clear  q quit"))
	view.AltScreen = true
	return view
}

// Render returns a deterministic text view and is also used by accessible mode.
func (model Model) Render() string {
	if model.view == viewForm {
		return model.renderForm()
	}
	title := lipgloss.NewStyle().Bold(true).Render("Review Party Configuration Hub")
	var output strings.Builder
	fmt.Fprintf(&output, "%s\nGlobal Configuration · Repository %s\n\n", title, model.snapshot.Repository)
	for index, spec := range menuAreaSpecs() {
		marker := "  "
		if index == model.area {
			marker = "› "
		}
		fmt.Fprintf(&output, "%s%s\n", marker, spec.area)
	}
	output.WriteString("\n")
	if model.query != "" || model.searching {
		fmt.Fprintf(&output, "Search: %s\n", model.query)
	}
	model.renderArea(&output)
	output.WriteString("\n↑/↓ navigate  enter open  / search  esc clear  q quit\n")
	return output.String()
}

func (model Model) renderArea(output *strings.Builder) {
	areas := menuAreaSpecs()
	if model.area < 0 || model.area >= len(areas) {
		return
	}
	spec := areas[model.area]
	if spec.render != nil {
		spec.render(model, spec, output)
	}
}

func renderOverviewArea(model Model, _ areaSpec, output *strings.Builder) {
	if model.query != "" {
		model.renderSearchResults(output)
		return
	}
	model.renderOverview(output)
}

func renderInventoryArea(model Model, spec areaSpec, output *strings.Builder) {
	items := model.visibleItems(spec.kind)
	for _, item := range items {
		fmt.Fprintf(output, "[%s] %s", item.Scope, item.Name)
		if item.Detail != "" {
			fmt.Fprintf(output, " — %s", item.Detail)
		}
		output.WriteString("\n")
	}
	if len(items) == 0 {
		output.WriteString("No matching items.\n")
	}
}

func renderActionArea(_ Model, spec areaSpec, output *strings.Builder) {
	fmt.Fprintf(output, "%s\n", spec.description)
}

func renderSearchArea(model Model, _ areaSpec, output *strings.Builder) {
	model.renderSearchResults(output)
}

func (model Model) renderSearchResults(output *strings.Builder) {
	matched := 0
	for _, item := range model.snapshot.Items {
		if !matches(item, model.query) {
			continue
		}
		matched++
		fmt.Fprintf(output, "[%s] %s — %s\n", item.Scope, item.Name, item.Detail)
	}
	if matched == 0 {
		output.WriteString("No matching items.\n")
	}
}

func (model Model) renderOverview(output *strings.Builder) {
	for _, line := range model.snapshot.Overview {
		fmt.Fprintf(output, "%s\n", line)
	}
	for _, warning := range model.snapshot.Warnings {
		fmt.Fprintf(output, "warning: %s\n", warning)
	}
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
		editor := &editor{RunOptions: options, manager: manager, drafts: &model.drafts}
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
	if _, err := io.WriteString(e.Output, New(snapshot).Render()); err != nil {
		return hubAction{}, err
	}
	return e.chooseAction()
}
