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

// Model is a Bubble Tea shell. It deliberately owns no filesystem handle and
// cannot publish configuration while a user merely navigates or searches.
type Model struct {
	snapshot  Snapshot
	area      int
	query     string
	searching bool
	width     int
	action    hubAction
}

func New(snapshot Snapshot) Model { return Model{snapshot: snapshot, width: 80} }
func (Model) Init() tea.Cmd       { return nil }

func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width = message.Width
	case tea.KeyPressMsg:
		if model.searching {
			if message.String() == "ctrl+c" {
				return model, tea.Quit
			}
			return model.updateSearch(message.String()), nil
		}
		return model.updateNavigation(message.String())
	}
	return model, nil
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
	return model
}

func (model Model) updateNavigation(key string) (tea.Model, tea.Cmd) {
	areas := menuAreaSpecs()
	switch key {
	case "q", "ctrl+c":
		return model, tea.Quit
	case "up", "k":
		model.moveArea(-1)
	case "down", "j":
		model.moveArea(1)
	case "enter":
		model.action = hubAction{area: areas[model.area].area}
		return model, tea.Quit
	case "/":
		model.area = 0
		model.query = ""
		model.searching = true
	case "esc":
		model.query = ""
	}
	return model, nil
}

func (model *Model) moveArea(offset int) {
	areas := menuAreaSpecs()
	next := model.area + offset
	if next >= 0 && next < len(areas) {
		model.area = next
	}
}

func (model Model) View() tea.View { return tea.NewView(model.Render()) }

// Render returns a deterministic text view and is also used by accessible mode.
func (model Model) Render() string {
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
	Context    context.Context
	Repository configuration.Repository
	Input      io.ReadCloser
	Output     io.Writer
	Accessible bool
}

// Run opens the terminal shell over a read-only Manager snapshot.
func Run(manager *configuration.Manager, options RunOptions) error {
	if options.Context == nil {
		options.Context = context.Background()
	}
	editor := &editor{RunOptions: options, manager: manager}
	return editor.runHub()
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
	snapshot, err := buildSnapshot(e.manager, e.Repository)
	if err != nil {
		return hubAction{}, err
	}
	if e.Accessible {
		if _, err := io.WriteString(e.Output, New(snapshot).Render()); err != nil {
			return hubAction{}, err
		}
		return e.chooseAction()
	}
	result, err := tea.NewProgram(New(snapshot), tea.WithContext(e.Context), tea.WithInput(e.Input), tea.WithOutput(e.Output)).Run()
	if err != nil {
		return hubAction{}, err
	}
	model, ok := result.(Model)
	if !ok {
		return hubAction{}, fmt.Errorf("Configuration Hub returned %T", result)
	}
	return model.action, nil
}
