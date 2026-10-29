package configurationhub

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

type reviewOperation int

const (
	reviewAdd reviewOperation = iota + 1
	reviewRemove
	reviewMove
	reviewConcurrency
	reviewInherit
)

func (operation reviewOperation) String() string {
	switch operation {
	case reviewAdd:
		return "add"
	case reviewRemove:
		return "remove"
	case reviewMove:
		return "move"
	case reviewConcurrency:
		return "concurrency"
	case reviewInherit:
		return "inherit"
	}
	return fmt.Sprintf("reviewOperation(%d)", int(operation))
}

type reviewEntry struct {
	group  configuration.Scope
	index  int
	expect configuration.SelectionItem
}

type reviewFormDraft struct {
	target    configuration.Scope
	operation reviewOperation
	reviewEntry
	to          int
	choice      configuration.SelectedDefinition
	concurrency string
}

var errStaleReviewSelection = errors.New("The selection changed; reopen Reviews.")

func planReviewEdit(manager *configuration.Manager, repository configuration.Repository, draft reviewFormDraft) (configuration.Plan, configuration.ReviewSelection, error) {
	base := draft.target
	if draft.operation == reviewInherit {
		base = configuration.ScopeGlobal
	}
	selection, err := manager.ReviewSelectionToEdit(base, repository)
	if err != nil {
		return configuration.Plan{}, configuration.ReviewSelection{}, err
	}
	selection, err = draft.apply(selection)
	if err != nil {
		return configuration.Plan{}, configuration.ReviewSelection{}, err
	}
	var intent configuration.Intent = configuration.SetReviewSelection{Target: draft.target, Selection: selection}
	if draft.operation == reviewInherit {
		intent = configuration.InheritGlobalReviewSelection{}
	}
	plan, err := manager.Plan(repository, []configuration.Intent{intent})
	return plan, selection, err
}

func (draft reviewFormDraft) apply(selection configuration.ReviewSelection) (configuration.ReviewSelection, error) {
	switch draft.operation {
	case reviewAdd:
		item, err := selectionItemFor(draft.choice)
		if err != nil {
			return configuration.ReviewSelection{}, err
		}
		return configuration.AddReviewSelection(selection, draft.choice.Scope, item), nil
	case reviewRemove:
		if err := draft.verify(selection); err != nil {
			return configuration.ReviewSelection{}, err
		}
		return configuration.RemoveReviewSelection(selection, draft.group, draft.index)
	case reviewMove:
		if err := draft.verify(selection); err != nil {
			return configuration.ReviewSelection{}, err
		}
		return configuration.MoveReviewSelection(selection, draft.group, draft.index, draft.to)
	case reviewConcurrency:
		limit, err := strconv.Atoi(strings.TrimSpace(draft.concurrency))
		if err != nil {
			return configuration.ReviewSelection{}, fmt.Errorf("invalid concurrency limit: %w", err)
		}
		selection.ConcurrencyLimit = limit
		return selection, nil
	case reviewInherit:
		return selection, nil
	}
	return configuration.ReviewSelection{}, fmt.Errorf("unknown Repository Reviews operation %q", draft.operation)
}

func (entry reviewEntry) verify(selection configuration.ReviewSelection) error {
	items := selection.Global
	if entry.group == configuration.ScopeRepository {
		items = selection.Repository
	}
	if entry.index < 0 || entry.index >= len(items) || items[entry.index] != entry.expect {
		return errStaleReviewSelection
	}
	return nil
}

func selectionItemFor(choice configuration.SelectedDefinition) (configuration.SelectionItem, error) {
	if choice.Scope != configuration.ScopeGlobal && choice.Scope != configuration.ScopeRepository {
		return configuration.SelectionItem{}, errors.New("choose an existing scoped Profile or Party")
	}
	switch choice.Kind {
	case configuration.ItemProfile:
		return configuration.SelectionItem{Profile: choice.Name}, nil
	case configuration.ItemParty:
		return configuration.SelectionItem{Party: choice.Name}, nil
	}
	return configuration.SelectionItem{}, errors.New("choose a Profile or Party")
}

func reviewOperationField(draft *reviewFormDraft) huh.Field {
	return huh.NewSelect[reviewOperation]().Title("Repository Reviews").Options(
		huh.NewOption("Add Profile or Party", reviewAdd),
		huh.NewOption("Remove item", reviewRemove),
		huh.NewOption("Move item", reviewMove),
		huh.NewOption("Set concurrency", reviewConcurrency),
	).Value(&draft.operation)
}

func reviewFields(draft *reviewFormDraft, snapshot Snapshot) ([]huh.Field, error) {
	switch draft.operation {
	case reviewAdd:
		options := reviewChoiceOptions(snapshot.ReviewChoices)
		if len(options) == 0 {
			return nil, errors.New(emptyReviewChoices)
		}
		return []huh.Field{huh.NewSelect[configuration.SelectedDefinition]().Title("Profile or Party to add").
			Description(addReviewDescription).Options(options...).Value(&draft.choice).WithHeight(8)}, nil
	case reviewRemove, reviewMove:
		options := reviewEntryOptions(snapshot.Items)
		if len(options) == 0 {
			return nil, errors.New("No Repository Reviews are selected.")
		}
		return []huh.Field{huh.NewSelect[reviewEntry]().Title("Selected item").Options(options...).Value(&draft.reviewEntry)}, nil
	case reviewConcurrency:
		return []huh.Field{huh.NewInput().Title("Concurrency limit").Validate(validatePositiveInteger).Value(&draft.concurrency)}, nil
	case reviewInherit:
		return nil, nil
	}
	return nil, fmt.Errorf("unknown Repository Reviews operation %q", draft.operation)
}

func reviewDestinationField(draft *reviewFormDraft, items []Item) huh.Field {
	positions := func() []huh.Option[int] {
		var options []huh.Option[int]
		for _, option := range reviewEntryOptions(items) {
			if option.Value.group == draft.group {
				options = append(options, huh.NewOption(option.Key, option.Value.index))
			}
		}
		return options
	}
	return huh.NewSelect[int]().Title("Move to").Options(positions()...).OptionsFunc(positions, &draft.group).Value(&draft.to)
}

func reviewEntryOptions(items []Item) []huh.Option[reviewEntry] {
	var options []huh.Option[reviewEntry]
	for _, item := range items {
		if item.Kind != itemReview {
			continue
		}
		options = append(options, huh.NewOption(fmt.Sprintf("[%s] %d. %s (%s)", item.Scope, item.Index+1, item.Name, item.Detail), item.entry()))
	}
	return options
}

func (e *editor) editReviews() error {
	draft := &e.draftSet().reviews
	draft.target = configuration.ScopeRepository
	if err := e.form(reviewOperationField(draft)); err != nil {
		return err
	}
	fields, err := reviewFields(draft, e.snapshot)
	if err != nil {
		return err
	}
	if draft.operation == reviewAdd {
		if _, err := fmt.Fprintln(e.Output, addReviewDescription); err != nil {
			return err
		}
	}
	if err := e.form(fields...); err != nil {
		return err
	}
	if draft.operation == reviewMove {
		if err := e.form(reviewDestinationField(draft, e.snapshot.Items)); err != nil {
			return err
		}
	}
	plan, selection, err := planReviewEdit(e.manager, e.Repository, *draft)
	if err != nil {
		return err
	}
	return e.publishReviewPlan(plan, selection)
}

func (e *editor) publishReviewPlan(plan configuration.Plan, selection configuration.ReviewSelection) error {
	published, err := e.reviewAndPublishWithPreview(
		plan,
		func() error { return e.manager.Publish(plan) },
		func(output io.Writer) error {
			return renderReviewSelectionPreview(output, e.manager, e.Repository, selection)
		},
	)
	if published {
		e.draftSet().reviews = reviewFormDraft{}
	}
	return err
}

func renderReviewSelectionPreview(output io.Writer, manager *configuration.Manager, repository configuration.Repository, selection configuration.ReviewSelection) error {
	if len(selection.Global) == 0 && len(selection.Repository) == 0 {
		_, err := fmt.Fprintf(output, "resolved review selection:\n  No reviews selected.\n  effective concurrency: %d\n", selection.ConcurrencyLimit)
		return err
	}
	resolved, err := manager.ResolveReviewSelection(repository, selection)
	if err != nil {
		return fmt.Errorf("resolve staged review selection: %w", err)
	}
	return configuration.RenderResolvedReviewsHuman(output, resolved)
}
