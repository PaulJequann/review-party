package configurationhub

import (
	"fmt"
	"io"
	"strconv"

	"charm.land/huh/v2"

	"reviewparty/internal/configuration"
)

type reviewFormDraft struct {
	operation, scope, kind, name, index, from, to, concurrency string
}

func (e *editor) editReviews() error {
	draft := &e.draftSet().reviews
	if err := e.form(huh.NewSelect[string]().Title("Repository Reviews").Options(
		huh.NewOption("Add Profile or Party", "add"), huh.NewOption("Remove item", "remove"),
		huh.NewOption("Move item", "move"), huh.NewOption("Set concurrency", "concurrency"),
	).Value(&draft.operation)); err != nil {
		return err
	}
	selection, _, err := e.manager.EffectiveReviewSelection(e.Repository)
	if err != nil {
		return err
	}
	if selection.ConcurrencyLimit == 0 {
		selection = configuration.DefaultReviewSelection()
	}
	intent, err := e.reviewIntent(draft.operation, selection)
	if err != nil {
		return err
	}
	plan, err := e.manager.Plan(e.Repository, []configuration.Intent{intent})
	if err != nil {
		return err
	}
	published, err := e.reviewAndPublishWithPreview(
		plan,
		func() error { return e.manager.Publish(plan) },
		func(output io.Writer) error {
			return renderReviewSelectionPreview(output, e.manager, e.Repository, intent.Selection)
		},
	)
	if published {
		e.draftSet().reviews = reviewFormDraft{}
	}
	return err
}

func renderReviewSelectionPreview(output io.Writer, manager *configuration.Manager, repository configuration.Repository, selection configuration.ReviewSelection) error {
	resolved, err := manager.ResolveReviewSelection(repository, selection)
	if err != nil {
		return fmt.Errorf("resolve staged review selection: %w", err)
	}
	return configuration.RenderResolvedReviewsHuman(output, resolved)
}

func (e *editor) reviewIntent(operation string, selection configuration.ReviewSelection) (configuration.SetReviewSelection, error) {
	switch operation {
	case "add":
		return e.addReview(selection)
	case "remove":
		return e.removeReview(selection)
	case "move":
		return e.moveReview(selection)
	case "concurrency":
		return e.concurrencyReview(selection)
	default:
		return configuration.SetReviewSelection{}, fmt.Errorf("unknown Repository Reviews operation %q", operation)
	}
}

func (e *editor) addReview(selection configuration.ReviewSelection) (configuration.SetReviewSelection, error) {
	draft := &e.draftSet().reviews
	err := e.form(huh.NewSelect[string]().Title("Selection group").Options(scopeOptions()...).Value(&draft.scope), huh.NewSelect[string]().Title("Kind").Options(huh.NewOption("Profile", "profile"), huh.NewOption("Party", "party")).Value(&draft.kind), huh.NewInput().Title("Name").Value(&draft.name))
	item := configuration.SelectionItem{Profile: draft.name}
	if draft.kind == "party" {
		item = configuration.SelectionItem{Party: draft.name}
	}
	return configuration.AddReviewSelection(selection, configuration.Scope(draft.scope), item), err
}

func (e *editor) removeReview(selection configuration.ReviewSelection) (configuration.SetReviewSelection, error) {
	draft := &e.draftSet().reviews
	if err := e.reviewScope(draft, huh.NewInput().Title("Zero-based index").Value(&draft.index)); err != nil {
		return configuration.SetReviewSelection{}, err
	}
	index, err := strconv.Atoi(draft.index)
	if err != nil {
		return configuration.SetReviewSelection{}, err
	}
	return configuration.RemoveReviewSelection(selection, configuration.Scope(draft.scope), index)
}

func (e *editor) moveReview(selection configuration.ReviewSelection) (configuration.SetReviewSelection, error) {
	draft := &e.draftSet().reviews
	if err := e.reviewScope(draft, huh.NewInput().Title("From index").Value(&draft.from), huh.NewInput().Title("To index").Value(&draft.to)); err != nil {
		return configuration.SetReviewSelection{}, err
	}
	from, err := strconv.Atoi(draft.from)
	if err != nil {
		return configuration.SetReviewSelection{}, err
	}
	to, err := strconv.Atoi(draft.to)
	if err != nil {
		return configuration.SetReviewSelection{}, err
	}
	return configuration.MoveReviewSelection(selection, configuration.Scope(draft.scope), from, to)
}

func (e *editor) reviewScope(draft *reviewFormDraft, fields ...huh.Field) error {
	fields = append([]huh.Field{huh.NewSelect[string]().Title("Selection group").Options(scopeOptions()...).Value(&draft.scope)}, fields...)
	return e.form(fields...)
}

func (e *editor) concurrencyReview(selection configuration.ReviewSelection) (configuration.SetReviewSelection, error) {
	draft := &e.draftSet().reviews
	if err := e.form(huh.NewInput().Title("Concurrency limit").Value(&draft.concurrency)); err != nil {
		return configuration.SetReviewSelection{}, err
	}
	limit, err := strconv.Atoi(draft.concurrency)
	if err != nil {
		return configuration.SetReviewSelection{}, err
	}
	selection.ConcurrencyLimit = limit
	return configuration.SetReviewSelection{Selection: selection}, nil
}
