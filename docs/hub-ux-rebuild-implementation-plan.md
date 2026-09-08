# Hub UX rebuild implementation plan

Status: **Slices R1-R5 complete** — R5 landed 2026-08-30. Reconciled against
[configuration-hub-implementation-plan.md](configuration-hub-implementation-plan.md)
(Slices 1-8 complete); see "Relationship to the existing plan" below.

The [Profile setup follow-up](#profile-setup-follow-up-before-slice-9) scopes
unimplemented discovery-backed selection before the remaining Slice 9 work.
R5 form repairs now budget header, actions, footer, and box padding on creation
and resize. The action bar exposes Tab, Shift+Tab, and Enter; form-layout tests
cover wrapped headers and focused inputs at narrow and wide sizes.

Post-R5 menu simplification (2026-09-07): the menu pane is one line per
destination, area descriptions render only in the focused context pane, the
redundant Advanced menu row was removed (Profile copy lives in the Profiles
browser via `p`, and accessible mode gained an explicit copy option), and
counts render only when meaningful. Dogfooding also exposed and fixed an
accessible-mode defect: the editor loop reused the program-start snapshot, so
Profiles published mid-session were invisible to the copy and reference
forms until restart.

## Problem

`review-party config` feels broken and boring because of how it is
architected, not because the terminal lacks capability:

1. **The Hub and its editors are two different terminal worlds.** The Hub menu
   runs as a Bubble Tea program with `AltScreen = true` (`model.go:235-257`).
   Selecting any action sets `model.action` and runs `tea.Quit`
   (`model.go:193-204`). The program exits, the alt screen collapses, and
   `editor.form()` (`editors.go:55-69`) runs each Huh form with **no width,
   height, theme, or program options** — so every submenu renders as a small
   box sized to its content on the raw scrollback. After the form,
   `nextAction()` (`model.go:431-451`) launches a brand-new Bubble Tea
   program. Every action therefore tears down and re-enters the alt screen:
   flicker, lost scroll position, and a dashboard that is full-bleed while
   every submenu is tiny.
2. **The shell has no design system.** The only style in the package is
   `lipgloss.NewStyle().Bold(true)` for two titles (`model.go:241,262`).
   Inventory renders as `[repository] name — detail` plain text. No color
   roles, containers, gutters, badges, or action bar.
3. **Flow is action-first and context-losing.** Opening *Profiles*
   immediately launches the create flow; Esc deep inside a multi-step profile
   form propagates `huh.ErrUserAborted` through `runHubStep`
   (`model.go:395-418`) and can exit the whole Hub; the repository selection
   editor asks for zero-based indexes in text inputs while the inventory it
   indexes sits one screen away.

## Research basis

- **no-mistakes** (`kunchenguid/no-mistakes`, cloned read-only under
  `scratch/no-mistakes/`) ships a written TUI design system
  (`scratch/no-mistakes/internal/tui/DESIGN.md`): ANSI 1-8 semantic color
  roles that follow the user's terminal theme, rounded-border boxed sections
  with titles embedded in the top border and hints in the bottom border, an
  action bar separating bold keys from labels, a fixed-width gutter so
  selection changes never shift content, responsive two-column layout at
  >= 100 columns with a stacked fallback, and exactly one `theme.go` /
  `box.go` / `layout.go` shared by every screen. Its `internal/wizard`
  package hand-rolls forms from `bubbles/textinput` on the Bubble Tea v1
  stack and contains no Huh — the transferable idea is "one long-lived
  program hosts custom step forms", not its code.
- **Lip Gloss v2** (`charm.land/lipgloss/v2`, already in `go.mod` at
  v2.0.6) supplies `JoinHorizontal`/`JoinVertical` for composed layouts,
  `Style.Width/Height` block padding so box content never rewraps mid-frame,
  `Place` for centering, and (with Bubble Tea v2) `tea.View{AltScreen}` for
  true fullscreen rendering.
- **Huh v2** (v2.0.3, already in `go.mod`) forms are *not* `tea.Model`
  implementations: `Form.View() string` versus Bubble Tea v2's
  `View() tea.View`, and `Form.Update(tea.Msg) (huh.Model, tea.Cmd)` versus
  `(tea.Model, tea.Cmd)` (verified in the module cache; the v1-era claim
  that "`huh.Form` is just a `tea.Model`" does not hold for the v2 line).
  Embedding is done with a small adapter, not direct composition.
  `WithWidth`, `WithHeight`, `WithTheme`, `WithShowHelp`, and
  `WithProgramOptions` exist (`form.go:244,269,302,316,351`);
  `Form.State()` reports `StateNormal/StateCompleted/StateAborted`
  (`form.go:47-51,567,583`).
- One `theme.go`/`box.go`/`layout.go` shared by every screen (R2), a glyph
  vocabulary aligned to status, live-count action bars, one-line outcome
  banners on success/failure, and layout math measured from rendered blocks
  via `lipgloss.Width`/`Height` instead of estimated header constants.
- **Huh v2 ships structured inputs the current editors never use**:
  `NewMultiSelect[T]` (`field_multiselect.go:56`), `NewNote`, and per-field
  `.Validate(func(T) error)` (verified in the module cache). bubbles v2
  v2.2.0 (already an indirect dependency) provides `help`, `list`, `table`,
  `spinner`, `textinput`, `textarea`.
- **Current editors confirmed unstructured beyond the wizard spam**: party
  membership is a free-text textarea ("one scoped reference per line",
  `party_editor.go:17`), Profile copy is free-text name lookup with a raw
  manager error on typo (`copy_editor.go:10-18`), and the create-profile
  wizard runs one single-question form per field (`profile_editor.go:73-83`
  loops `profileFieldSpecs`).
- Contextual action bars with live counts and a `?` help overlay from the
  bubbles `help` component (R3/R5).
- Two-press confirmation pattern for future destructive actions, recorded
  in R3 for deletion work when Slice 9 unblocks it.

## Locked decisions

- **One program, one screen.** The interactive Hub is a single Bubble Tea
  program for its whole lifetime. The quit-on-action loop, the standalone
  Huh program boundary, and the raw-`fmt.Fprintf` interleaves are removed
  from the interactive path. `view.AltScreen = true` is set from the first
  frame, so there is exactly one alt-screen entry and exit per session.
- **The Model still owns no filesystem handle.** The documented invariant at
  `model.go:111-112` stands: navigation and searching never touch the
  Manager. Plan construction and `Publish` execute as Bubble Tea commands
  (async, cancellable via `tea.WithContext`) outside the Model, and results
  return as messages. Draft state moves from the `editor` struct
  (`editors.go:15-53`) into the Model.
- **Capability contract unchanged.** The Hub offers exactly what the
  Configuration Manager can plan today: Profile creation, Profile copy,
  Party creation, and Repository Reviews add/remove/move/concurrency.
  Profile and Party **deletion and editing stay out of scope** — the
  existing plan defers deletion until backup/export recovery exists
  (Slice 9), and no update intent exists in
  `internal/configuration/intent.go`. This rebuild changes no domain
  operation, no command, and no JSON contract.
- **ANSI 1-8 palette only.** Semantic roles adopted from no-mistakes:
  blue = focus/interactive, cyan = section titles/accent, green = published
  /success, yellow = warnings/drafts, red = errors/danger, bright black =
  borders/meta/hints. No hard-coded hex or 256-color values; styling follows
  the user's terminal theme.
- **Accessible mode is a first-class surface, not a leftover.** It keeps its
  own plain non-redrawing renderer over the same draft state machine, per
  the existing locked decisions ("Accessible mode uses the same draft and
  intent state machine through ordinary prompts").
- **Scoped `$EDITOR` handoff stays out-of-program.** Instruction editing
  suspends the Hub with `tea.ExecProcess`
  (`charm.land/bubbletea/v2` `exec.go:50`, verified present), which
  restores the terminal around the child and writes the alt screen back
  afterward. The existing process-group cleanup in
  `instruction_editor.go` remains the owner of child lifecycle.

## Non-goals

- No Profile or Party editing, renaming, or deletion.
- No new Configuration Manager operations, intents, or commands.
- No Profile/Party `Detail` or snapshot enrichment beyond wrapping what
  `buildSnapshot` already produces (`snapshot.go`).
- No Model Discovery area, dragging, or mouse support.
- No themes beyond the ANSI role palette; no user-facing theme selection.
- No compatibility shims for the removed quit-on-action loop.

## Architecture

```text
                one tea.Program (alt screen, whole session)
                                     │
        ┌────────────────┬───────────┴──────────┬───────────────────┐
        ▼                ▼                      ▼                   ▼
   viewMenu         viewInventory        viewForm (modal)      viewPlanPreview
   nav + overview   list + detail pane   embedded huh.Form     rendered plan +
                                         via formAdapter       warnings + confirm
        │                │                      │                   │
        └────────────────┴──────────┬───────────┴───────────────────┘
                                    ▼
                    commands outside the Model:
              planSubmission / publishProgress / publishResult
                                    │
                                    ▼
                       Configuration Manager (unchanged)
```

New files in `internal/configurationhub/`:

| File | Owns |
| --- | --- |
| `theme.go` | ANSI role constants and the lipgloss style set (single source of styling) |
| `box.go` | `renderBox(title, content, width, footer)` with embedded title/hint borders |
| `layout.go` | Header composition, breadcrumb, responsive two-column split, height budget |
| `form_adapter.go` | Bridges Huh `Form` into the Bubble Tea program (Init/Update/View, width/height/theme forwarding, State() reading) |
| `views.go` | Per-view renderers (menu, inventory + detail pane, form modal, plan preview) |
| `commands.go` | `planSubmission`, `publishProgress`, `publishResult` tea.Cmd/Msg types |

Modified files: `model.go` (state machine, view switching, command wiring),
`editors.go` (editor functions become plan builders + messages; `form()`
retained only for the accessible path), the per-editor files (return plan
requests instead of running forms inline), `command_line.go` (unchanged),
`snapshot.go` (unchanged).

## Slices

```text
R1  Program unification and embedded forms        (fixes "broken")
R2  Theme and box primitives                      (fixes "boring")
R3  Hub shell, browsers, and detail pane
R4  In-TUI plan preview and editor conversion
R5  Responsive layout and polish
```

Each slice lands independently: R1 alone removes the alt-screen churn;
R2-R5 are progressive visual and flow upgrades on top.

## Slice R1: program unification and embedded forms

### Goal

Every interactive step renders inside one fullscreen program. Selecting an
area, completing or aborting a form, and returning to the menu never exit the
program; the terminal flashes exactly once at launch and once at exit.

### Work

- Introduce `viewState` on the Model: `viewMenu`, `viewForm`, and later
  `viewPlanPreview`. `updateNavigation`'s enter case switches views instead
  of returning `tea.Quit` (`model.go:202-204`).
- Add `form_adapter.go` wrapping `*huh.Form`:
  - `newFormAdapter(fields []huh.Field, width, height int, theme huh.Theme) formAdapter`
    builds the form with `WithWidth/WithHeight/WithTheme/WithShowHelp(false)`.
  - `Update` forwards `tea.KeyPressMsg` and `tea.WindowSizeMsg` (huh v2
    accepts these directly) and maps the returned `huh.Model` back onto the
    adapter's form pointer.
  - `View() string` renders `form.View()` inside a themed box sized to the
    content width and viewport height.
  - State reads via `form.State()`: `StateCompleted` advances the flow,
    `StateAborted` returns to `viewMenu` keeping drafts, `StateNormal`
    stays.
- Move draft state (`draftSet`, `editors.go:21-53`) onto the Model. The
  editors stop being the state owner; they become functions the command
  layer calls with the Model's drafts.
- Keep `editor.form()` for `--accessible` and reuse the existing
  `runAccessibleForm` cancellation join (`editors.go:71-82`). Accessible
  behavior is otherwise unchanged.
- Route instruction editing through `tea.ExecProcess` with the existing
  `editInstructions` child command so the alt screen suspends and restores
  cleanly.
- Menu-level `q` and `ctrl+c` quit; inside a form, `esc` aborts the form
  (draft retained) and only `ctrl+c` quits the program.
- Consolidate the single-question profile wizard into one grouped Huh form
  per editing session (name/reviewer/model/effort/deadline fields plus a
  `NewNote` context header in one `NewGroup`), replacing the
  one-form-per-field loop in `editProfileFields`
  (`profile_editor.go:73-83`). Interdependent resets (reviewer change
  clears model/effort/deadline, `profileFieldSpecs.set`) stay in the same
  setters; validation errors keep the user inside the form instead of
  printing and looping through the menu (`runHubStep`'s
  print-and-return error path, `model.go:407-411`, becomes an in-program
  error banner; full conversion lands in R4 with plan preview).

### Acceptance

- Entering and leaving any editor never exits the program: one alt-screen
  entry per Hub session, verified by driving `Model.Update` with key
  sequences and asserting `viewState` transitions without `tea.Quit`.
- A form's `esc` returns to the menu with drafts intact; the existing
  discard-or-return exit flow (`confirmExit`, `editors.go:123-135`) still
  triggers only when drafts exist.
- Resize during a form re-renders the form at the new size.
- All existing `model_test.go` scenarios pass after adaptation; new tests
  cover view switching, adapter state mapping, and draft retention.
- The Hub cancellation ledger row ("Cancel each editor and exit from Review
  Changes... byte-for-byte absent or unchanged") remains satisfied.

## Slice R2: theme and box primitives

### Goal

One design system file pair that every view composes, ported from
no-mistakes' DESIGN.md and constrained to the locked ANSI palette.

### Work

- `theme.go`: ANSI role constants (`roleRed="1"`, `roleGreen="2"`,
  `roleYellow="3"`, `roleBlue="4"`, `roleCyan="6"`, `roleBrightBlack="8"`)
  and one style set: section title (bold cyan), meta (bright black), focus
  (blue), warning (yellow), danger (red), success (green), border (bright
  black). No other file may construct a lipgloss style with a raw color.
- `box.go`: `renderBox(styledTitle, content string, width int, footer
  string) string` — rounded border, title embedded in the top border
  (`╭─ Title ──╮`), optional hint embedded in the bottom border
  (`╰──── hint ────╯`), 1-column horizontal padding, content padded to
  `width` with `Style.Width` so it never rewraps. Port the arithmetic from
  `scratch/no-mistakes/internal/tui/box.go` (92 lines) with minimum-width
  guards.
- Restyle the menu header: title row with repository scope badge, menu rows
  in a fixed gutter (`› ` cursor column, counts right-aligned, draft
  indicator `⏸` in yellow next to *Review Changes* when drafts exist),
  action bar with bold keys (`↑/↓ navigate  ⏎ open  / search  esc clear  q
  quit`).
- Restyle inventory rows: colored scope badges (`[global]` cyan dim,
  `[repository]` blue), name, detail in meta style.
- Warnings render yellow with `▲`; errors red.
- `Render()` (accessible mode) gains the same information in plain text with
  no ANSI output.
- Deliberately **not adopted** from the researched sources: lipgloss v2
  `LightDark`/`Complete` adaptive hex colors (the locked ANSI 1-8 decision
  already tracks the user's theme without hex triples), gradient/blend
  border effects, and no-mistakes' bubbles v1 `textinput` wizard code (v1
  stack; patterns only, no code port).

### Acceptance

- `theme.go` and `box.go` are the only new styling surface; grep shows no
  raw color literals outside `theme.go`.
- Snapshot tests (golden strings) for box rendering at widths 20, 40, 80:
  title and footer embedded correctly, content never clipped silently,
  minimum-width guards hold.
- Accessible `Render()` output contains no ANSI escape sequences (assert in
  a test).
- The recent header-duplication-on-scroll fix (commit `95388b4`) is covered
  by a regression test in the new layout.

## Slice R3: Hub shell, browsers, and detail pane

### Goal

Browse-first navigation: opening an area shows its inventory plus a live
detail pane; creation becomes an action (`n`), not the area's only outcome.

### Work

- Menu view: the six areas rendered in a bordered box with descriptions;
  enter opens the area's browser view.
- Inventory views for Profiles, Parties, and Repository Reviews: item list
  in the left/main region, cursor with fixed gutter, filtered live by the
  active `/` query (search keeps its current substring semantics over
  `matches()`, `model.go:359-362`).
- Detail pane (wide layout) or stacked section (narrow): for the
  highlighted item, render its existing `Item` fields (scope badge, name,
  detail) plus, for profiles/parties, the resolved definition the snapshot
  already carries in `Overview` lines. No new Manager reads from the Model:
  the command layer enriches the snapshot once per action cycle exactly as
  `buildSnapshot` does today (`model.go:432`).
- Contextual action bar per view: `n` new (Profiles/Parties), `a` add /
  `r` remove / `m` move / `c` concurrency (Repository Reviews), `p` copy to
  Global (Profiles), `/` filter, `esc` back to menu, `q` quit.
- Remove-item and move-item flows become row-driven: `r` on a highlighted
  review row stages removal by the row's identity; `m` then `j/k/⏎` picks
  the destination — no typed zero-based index. The typed-index path remains
  only in the CLI commands.
- The Reviews browser renders both scopes as labeled sections
  (`Global selection`, `Repository selection`) matching the authored
  selection document.
- Row-driven structured inputs replace free-text where the domain data is
  already enumerable:
  - Profile copy (`copy_editor.go`) presents a select over the
    repository's profiles from the snapshot inventory instead of a
    free-text name; a typo becomes an impossible state instead of a raw
    `PlanProfileCopy` error.
  - Party membership (`party_editor.go:17`) presents a `huh.NewMultiSelect`
    of scoped profile references (grouped Global/Repository) instead of
    the free-text "one scoped reference per line" textarea. Expansion
    order follows selection order; the existing
    `configuration.ParseScopedReference` path remains for the CLI.
  - Reviews concurrency input gains `Validate` (integer, > 0) so errors
    surface inline instead of `strconv.Atoi` failure after submit.

### Acceptance

- Every action reachable from the menu today is still reachable, with the
  same Manager calls and the same plan/confirm publication contract.
- Opening Profiles no longer launches a creation form; `n` does.
- Repository Reviews remove/move operate on the highlighted row and produce
  the same `configuration.SetReviewSelection` values the typed-index forms
  produced (assert by comparing plans in tests).
- Draft indicators appear in the action bar whenever `drafts` is non-empty;
  exit with drafts still offers discard-or-return.

## Slice R4: in-TUI plan preview and editor conversion

### Goal

The publication moment — plan review, warnings, confirm — happens inside
the program as a styled preview view instead of printed scrollback plus a
standalone confirm form.

### Work

- Add `viewPlanPreview`: renders `configuration.RenderPlanHuman` output into
  a box with warnings above and an action bar (`p` publish  e revise  esc
  cancel). Confirm/decline are keys, not a Huh form.
- Convert `reviewAndPublish` / `reviewAndPublishWithPreview`
  (`editors.go:166-194`) into a flow that returns a `planReady` message; the
  Model switches to `viewPlanPreview`; `publishRequested` dispatches the
  `publishResult` command that calls `manager.Publish` outside the Model
  and reports success/failure as a message. On success the drafts for that
  editor clear (matching `createProfile`/`createParty` today) and a green
  confirmation line shows in the action bar until the next keypress.
- `planSubmission` command builds the plan (including
  `ModelChoiceCheck` warning attachment, `profile_editor.go:85-98`) outside
  the Model; validation failures return as an error message that reopens
  the form view with the draft intact and the error in red above the form —
  replacing the `reviseProfileAfterError` print-and-reloop.
- JSON output is untouched: this is the interactive path only; CLI commands
  keep their existing contracts (Slice 6 acceptance stands).

### Acceptance

- No `fmt.Fprintf(e.Output, ...)` remains on the interactive path between
  program frames (assert: `editor.Output` writes occur only in accessible
  mode and error paths that abort the program).
- Cancel at plan preview writes nothing (ledger row: cancellation leaves
  configuration byte-for-byte unchanged) — covered by an isolated-XDG test
  under `scratch/`.
- Publish failure surfaces in-program with the draft retained; retry
  revises instead of restarting the flow.
- Publication success clears only the published editor's draft (profile
  publish does not discard a pending party draft).

## Slice R5: responsive layout and polish

### Goal

Wide terminals use the two-column shell; narrow terminals degrade to a
single column with no truncation. The whole Hub shares one height budget.

### Work

- `layout.go`: responsive split adapted from
  `scratch/no-mistakes/internal/tui/layout.go` — threshold 100 columns,
  left pane (menu/context) 38-48 columns, right pane (browser/detail) the
  remainder with a 2-column gap, stacked below threshold. Implemented with
  `lipgloss.JoinHorizontal` over `Style.Width`-padded blocks.
- One height budget computed per frame: header + action bar + footer
  heights are subtracted once; the viewport receives exactly the remainder
  (replacing the ad-hoc arithmetic at `model.go:139-151`, whose two
  clashing clamps are the current source of layout drift).
- Scroll indicator embedded in the viewport box's bottom border
  (`╰──── ↓ 23 more lines (j/k) ────╯`) per no-mistakes' Diff View
  pattern.
- Terminal title set/cleared around the program (no-mistakes' practice;
  cheap and aids window switching).
- Polish pass: consistent 1-blank-line spacing between sections, no
  trailing blank lines inside boxes, breadcrumb (`Hub › Profiles`) in the
  header on non-menu views.
- Outcome banners: publication success/failure renders a one-line banner
  (`✓ Published profile "bugs"` green / `✗ Publish failed: …` red) that
  persists in the action bar until the next keypress, naming the affected
  item.
- Help overlay: `?` toggles a bubbles-`help`-based overlay listing every
  binding for the focused view; the action bar keeps the short form.

### Acceptance

- Golden-frame tests at 80x24, 100x30, and 160x50: no clipped lines, no
  wrapping mid-frame, cursor always visible.
- Height-budget unit test: viewport height equals terminal height minus
  exactly header + action bar + footer at every size from 20 to 60 rows.
- Manual dogfood under `scratch/` with isolated XDG dirs per the existing
  slice protocol: create profile from template, create party, edit
  selection, copy profile, abort mid-form, resize mid-form, publish, quit.
  Terminal state clean after exit (`git diff --check`-equivalent: no
  residual alt-screen artifacts).

## Profile setup follow-up before Slice 9

Status: scoped, not implemented. Discovery-backed selection is separate from
R5 form sizing and navigation repairs. Linear remains the issue tracker; this
section defines the implementation scope, not a second backlog.

### Goal

Create a Profile by choosing a Reviewer, model, and supported reasoning effort
without having to know their identifiers in advance. Keep exact manual model
entry available when discovery is incomplete.

### Existing implementation

- `internal/discovery/choices.go` exposes immediate cached, configured, and
  packaged choices through `ChoiceSnapshot`. `Service.Open` adds a bounded
  asynchronous refresh, and `ChoiceSession.Close` cancels it.
- `ModelChoice` carries source provenance and model reasoning-effort metadata.
- `cmd/review-party/config_hub.go` currently passes only `ModelChoiceCheck` to
  the Hub. The profile fields in `interactive_forms.go` remain text inputs.

### Bounded work

- Offer known Reviewer identifiers as choices. Show availability and
  authentication diagnostics without starting login or substituting an agent.
- Offer searchable model choices for the selected Reviewer, with provenance
  and an explicit manual-entry option. Render immediate choices before refresh
  completes; retain the current selection when results arrive.
- Offer the selected model's reported reasoning efforts. When metadata is
  unavailable, label that limitation and allow explicit manual entry rather
  than guessing supported values or choosing a default silently.
- Keep Profile name, scope, deadline, and instruction selection in the existing
  creation flow. Changes to Reviewer or model invalidate dependent selections
  using the existing draft rules.
- Own discovery lifecycle outside rendering. Cancel abandoned requests and
  ignore late results for a previous Reviewer or closed editor.
- Give the accessible editor the same choices, manual-entry route, warnings,
  and reviewed publication contract.

### Acceptance

- A terminal drive creates and reads back a Profile by selecting a Reviewer,
  model, and reported effort without typing their identifiers.
- Empty, expired, failed, and slow discovery leave manual entry usable. Cached
  choices never imply verified current access.
- Switching Reviewers during refresh cannot populate the new selection with
  the old Reviewer's results. Refresh never overwrites typed input.
- Unknown model publication requires the existing warning and confirmation.
  No authentication or executable review starts during configuration.
- Normal and accessible flows preserve exact selected values through publish
  and `explain`. Cancellation leaves configuration unchanged.
- Choice lists and manual fields fit at 80x24 and 120x30, keep focus visible
  through resize, and retain visible navigation guidance.

Out of scope: editing saved Profile execution settings, new Reviewer adapters,
credential management, model benchmarking, and Slice 9 recovery work.

## Relationship to the existing plan

- `docs/configuration-hub-implementation-plan.md` Slices 1-8 remain
  complete and authoritative for the domain, commands, and publication
  contract. This rebuild consumes those seams unchanged.
- That plan's Slice 9 is partially absorbed: its Hub presentation work
  (Template drift visibility) will be delivered on top of R2/R3 views, and
  its deletion work remains deferred exactly as written. Its non-Hub work
  (ledger backup, `doctor`, README, dogfooding) stays owned by Slice 9.
- The existing plan's locked decisions, explicit non-goals, and test-intent
  ledger carry forward. New ledger rows added by this plan:

| Behavior | Plausible harmful defect | Boundary | Required observation |
| --- | --- | --- | --- |
| One program per Hub session | Alt-screen churn loses scrollback and drafts mid-flow | Interactive Hub program lifecycle | Exactly one program; view switches assert no `tea.Quit` |
| Drafts survive form abort | Esc in a form discards user input or exits the Hub | Model draft state | Abort returns to menu with drafts; discard prompt only at exit |
| Plan/publish outside the Model | Navigation triggers filesystem writes | Model/Manager boundary | No Manager call reachable from `Update` navigation paths |
| Cancellation writes nothing | Failed publish leaves partial configuration | Real Manager filesystem publication | Isolated-XDG test asserts byte-for-byte unchanged files on cancel/failure |
| Palette follows terminal theme | Hard-coded hex clashes on light themes | `theme.go` | No raw color literals outside `theme.go`; ANSI 1-8 only |
| Accessible output stays plain | ANSI escapes corrupt screen-reader output | Accessible renderer | No escape sequences in `Render()` output |
| Row actions match typed intents | Row-driven remove produces a different selection than the CLI | Review selection plans | Plans compare equal to the typed-index forms' output |
| Party membership stays declarative | MultiSelect order or scope labels diverge from authored references | Party plan expansion | Published party references equal the CLI path's output for the same choices |
| Inline validation prevents stale submits | Invalid concurrency reaches plan time as before | Form field Validate | Invalid values are rejected inside the form; plan receives only valid drafts |
| Responsive panes fit the terminal | A pane clips or wraps differently after resize | Layout rendering | Golden frames at 80x24, 100x30, and 160x50 keep every line within the terminal and keep the cursor visible |
| One height budget owns the frame | Independent clamps hide the action bar or viewport content | Frame sizing | Viewport height equals terminal height minus the measured header, action bar, and footer at rows 20 through 60 |

## Execution protocol

Per slice, follow the existing plan's slice protocol: mark in progress,
record the test-intent ledger refinement, implement through the owning
module's interface, run `gofmt`, focused tests, targeted `go vet`, targeted
build, the CodeScene flow from `AGENTS.md` for each touched source file, and
the named CLI scenario in isolated XDG configuration and state directories
under `scratch/`. Update this document and the configuration-hub plan's
status lines as slices land.
