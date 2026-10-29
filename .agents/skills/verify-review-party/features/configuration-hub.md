# Configuration Hub

Review Party opens a recurring terminal Configuration Hub for scoped
configuration. The normal path is a Bubble Tea shell with browse-first areas
and in-program plan preview; `--accessible` uses non-redrawing prompts for the
same configuration operations.

## Sub-features

- `HUB-SHELL-NAVIGATION`
- `HUB-PLAN-PREVIEW-PUBLISH`
- `HUB-ACCESSIBLE-PUBLISH`
- `HUB-GLOBAL-TARGETS`
- `HUB-GLOBAL-ONLY-LAUNCH`

## Source evidence

- `newConfigCommand` in `cmd/review-party/standard_commands.go` exposes
  `config`, the `--accessible` flag, and the explicit command family.
- `executeConfigurationHub` in `cmd/review-party/config_hub.go` requires
  terminal input/output, resolves the repository, and routes the selected mode
  through the Hub. `resolveConfigurationHubRepository` opens the Hub with no
  repository only when `--repo` was not given and git reports
  `subject.ErrNotRepository`; an explicit `--repo` must name a repository.
- `hubTargets` in `internal/configurationhub/snapshot.go` sets
  `Snapshot.Targets` to Global and Repository, or to Global alone without a
  repository. `Snapshot.writes` in `internal/configurationhub/model.go` gates
  every Repository-only surface: the `[global only]` title badge and header
  note, the Repository sections of Reviews and Checkpoints, Profile copy
  (`copiesProfiles`), the scope question (`scopeFields` in `editors.go`), and
  the resolved run preview, which becomes `runPreviewAbsence`.
- `newAreaSpecs` in `internal/configurationhub/model.go` defines the Overview,
  Profiles, Parties, Reviews, Checkpoints, and Review Changes areas;
  `updateNavigation` and `openSelectedArea` own menu navigation and area
  opening; `View` owns the alt-screen title and terminal view.
- `renderPlanPreviewFrame` and `renderPlanActionBar` in
  `internal/configurationhub/view_chrome.go` render the plan preview and
  publish controls; `receivePlan` and `receivePublishResult` in
  `internal/configurationhub/interactive_forms.go` handle plan receipt,
  publication, refreshed snapshots, and the success outcome.
- `profileModelOptions` in `internal/configurationhub/profile_choices.go`
  lists Refresh first and marks the current model; `initialProfileModel`
  starts both adapters on the current model, else the first discovered model,
  else Refresh; `completeProfileModelForm`
  reopens discovery on Refresh and `editAccessibleModel` does the same for
  the accessible adapter.
- `runAccessibleForm`, `chooseAction`, `confirmExit`, and
  `reviewAndPublishWithPreview` in `internal/configurationhub/editors.go`
  implement the accessible form and plan/publication flow.

Drift: none. The normal and accessible entry points are separate adapters, so
both need live coverage.

## How to get to it (user POV)

- Run `review-party config --repo PATH --config PATH` in a terminal.
- Run `review-party config --accessible --repo PATH --config PATH` for
  non-redrawing prompts in a terminal.
- Use the Profiles area to create a Profile, review its plan, and publish it.
- Run `review-party config --config PATH` from a directory outside every git
  repository to edit Global Configuration only.

## Driving it with the CLI

Preconditions: start from a fresh launched baseline and verify that a PTY
driver is available. The commands below use `tmux`, a 120x30 terminal, and the
run-owned paths extracted from `ownership.tsv`. Capture screen state after
each stable handle. Use `C-[` for Escape and send one action at a time.

### Terminal boundary and normal Hub

Run the negative control first through `capture`:

```sh
set +e
.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" configuration-hub/negative-non-tty.txt -- review-party config --repo "$run_dir/runtime/repository" --config "$run_dir/runtime/config/review-party/config.json"
negative_status=$?
set -e
```

Require a nonzero `negative_status` and `the Configuration Hub requires a
terminal` in the capture transcript. The helper also records the status in
`negative-non-tty.txt.exit`. Confirm that no Profile path was created, then run
doctor.

For the positive normal-TUI drive, start the session using the terminal
procedure in `features/README.md`. Every capture below goes to
`$evidence/configuration-hub/NAME.txt`.

1. **Menu.** Wait for `Menu`. Capture `menu.txt`; it must show `Menu` and
   `Overview` in separate panes.
2. **Profiles browser.** Send `Down`, then `Enter`. Capture
   `profiles-browser.txt`, then send `n` for a new Profile.
3. **Profile form layout.** Wait for `Configuration scope`. Capture
   `profile-form.txt`: the full 120x30 frame must include the bottom border,
   `tab next`, `shift+tab previous`, `enter continue`, and `? help`. Run
   `tmux resize-window -t "$session" -x 80 -y 24`, capture
   `profile-form-80x24.txt`, and require the same controls to remain visible.
4. **Profile fields.** Run `tmux resize-window -t "$session" -x 120 -y 30`.
   Send `Down` to choose Repository scope, `Tab`, type `ui-bugs`, `Tab`, then
   `Down` to choose `codex` in the Reviewer select. Capture
   `profile-fields.txt` with the name and Reviewer inside the box, then send
   Enter; Tab alone does not submit the form.
5. **Model.** Wait for `Model availability` and capture `profile-model.txt`.
   The Model select must list `↻ Refresh available models` first, then
   `gpt-5.6-luna` with the cursor on it for a new Profile, then
   `Enter a model ID manually`. Send `Up`, then Enter on Refresh, wait for
   the Model select to return, and capture `profile-model-refreshed.txt`; the
   owned HOME reports `codex` unavailable, so the list is unchanged and the
   cursor is back on `gpt-5.6-luna`. Send Enter to choose it.
6. **Effort and deadline.** The unavailable Reviewer reports no effort
   choices, so `Reasoning effort` is a text input. Type `high` and send Enter.
   At `Attempt deadline`, type `8m` and send Enter.
7. **Instructions.** Accept the default Template instruction source, accept
   the first packaged template, and accept the default
   `Edit instructions with $EDITOR?` answer.
8. **Plan preview.** Wait for `Plan preview` and capture `plan-preview.txt`.
   It must show `ui-bugs`, `codex`, `gpt-5.6-luna`, and the Repository scope,
   with `p` available to publish.
9. **Publish.** Send `p`, wait for `Published profile "ui-bugs"`, and capture
   `published.txt`.
10. **Copy form.** Send `esc` to return to the menu, then `Enter` to reopen
   Profiles. Send `p` (copy) and wait for
   `Repository Profile to copy to Global Configuration`. Capture
   `copy-form.txt`; it must list the published `ui-bugs` among the selectable
   Repository Profiles. Send `C-[` twice to return to the menu without
   publishing the copy.
11. **Edit keeps the current model.** Send `Enter` to reopen Profiles, then
   `e` on `ui-bugs`. Send Enter on the Reviewer select, wait for the Model
   select, and capture `edit-model.txt`; the cursor must be on
   `gpt-5.6-luna [current]`. Send `C-[` until the menu returns.
12. **Retain drafts.** Send `q` and wait for `Discard unfinished drafts and
   exit?`. Capture `quit-confirmation.txt`. Accept the default `Keep working`
   choice and require the same menu and unfinished drafts to remain.
13. **Discard and exit.** Send `q` again, choose `Discard and exit`, and require
   the session to exit without publishing either draft. The run-owned
   repository Profile at
   `$run_dir/runtime/repository/.reviewparty/profiles/ui-bugs/profile.json`
   must exist, and no `ui-bugs` Profile may exist in the Global configuration
   root.

Run doctor, then use `capture` for a
read-only second view:

```sh
.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" configuration-hub/readback.json.txt -- review-party explain ui-bugs --repo "$run_dir/runtime/repository" --config "$run_dir/runtime/config/review-party/config.json" --format json
.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" configuration-hub/validate.json.txt -- review-party config validate --scope repository --repo "$run_dir/runtime/repository" --config "$run_dir/runtime/config/review-party/config.json" --format json
```

Require the readback to contain `ui-bugs`, `codex`, `gpt-5.6-luna`, `high`,
and `8m`, and require validation to report `"valid": true`.

### Accessible prompt adapter

Use a fresh launched baseline and run doctor before the drive. Start the same
terminal command with `--accessible`. Wait for each prompt before sending the
next logical line. Huh selects take the number printed beside the choice, so
read the prompt and send the number for the named label; confirmations use
`y` or `n`. The representative Profile flow is:

| Prompt | Answer |
| --- | --- |
| Configuration Hub menu | the `Profiles` choice |
| Profiles | the `Create a new Profile` choice |
| Configuration scope | the `Repository` choice |
| Profile name | `ui-accessible` |
| Reviewer | `codex` |
| Model | `↻ Refresh available models`, then `gpt-5.6-luna` when the prompt returns |
| Reasoning effort, Attempt deadline | `high`, `8m` |
| Instruction source | the `Template` choice |
| Template | the first packaged template |
| Edit instructions with $EDITOR? | `n` |
| Publish this complete plan? | `y` |
| Configuration Hub menu | the choice that copies a Repository Profile to Global Configuration |
| Repository Profile to copy | `ui-accessible` under the `[repository]` label |
| Publish this complete plan? | `n` |
| Configuration Hub menu | the `Exit` choice |
| Discard unfinished drafts and exit? | `y` |

The Refresh answer must print the Reviewer diagnostic and ask for the Model
again with Refresh still listed first.

Capture the plan prompt at `configuration-hub/accessible-plan.txt`. The copy
step must offer `ui-accessible` under a `[repository]` label, and declining it
must leave a draft so that Exit asks to discard before the process exits.
Require the process to exit after discarding, and confirm the run-owned
repository Profile at
`$run_dir/runtime/repository/.reviewparty/profiles/ui-accessible/profile.json`
exists while no `ui-accessible` Profile exists in the Global configuration
root. Use this read-only second view:

```sh
.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" configuration-hub/accessible-readback.json.txt -- review-party explain ui-accessible --repo "$run_dir/runtime/repository" --config "$run_dir/runtime/config/review-party/config.json" --format json
```

Require the readback to contain the same identity and execution fields as the
normal path. Run doctor before cleanup.

### Global targets and the Global-only launch

Use a fresh launched baseline. The run directory sits inside this checkout, so
create a plain directory and stop git's upward search at its parent:

```sh
plain="$run_dir/runtime/plain"
mkdir -p "$plain"
tmux new-session -d -x 120 -y 30 -s "$session" -c "$plain" -- \
  env TERM=xterm-256color EDITOR=true HOME="$home" XDG_CONFIG_HOME="$config_root" XDG_STATE_HOME="$state_root" \
  GIT_CEILING_DIRECTORIES="$run_dir/runtime" "$binary" config --config "$config_file"
```

1. **Badge.** Wait for `[global only]`. Capture `global-only-menu.txt`; the
   title must show `[global only]` and the Global path, the header must say
   `Not in a git repository, so Repository Configuration is not available.`,
   and Overview must show `Global:` counts, `Run preview needs a repository`,
   and no `Repository` line.
2. **Profile without a scope question.** Open Profiles and send `n`. Capture
   `global-only-profile-form.txt`; the form must start at `Profile name` with
   no `Configuration scope` field, and the browser footer and `?` help must
   not offer copy. Complete the Profile as in the normal drive and publish it;
   the plan names `global profiles`.
3. **Global Review selection.** Open Reviews; the only section is
   `Global Configuration`. Send `a`, choose the new Profile, and capture
   `global-only-reviews-plan.txt`; it must show `global reviews` and
   `Run preview needs a repository.` in place of the resolved selection.
   Publish it.
4. **Global Checkpoint.** Open Checkpoints; the only section is
   `Global Configuration`, with no enrollment keys. Set `pre-push`, accept the
   defaults with no Integrations, and publish. No undeclared-Integrations
   notice appears.
5. **Read back.** After each publish run
   `review-party config file show --scope global --format json` through
   `capture`; it must hold the Profile in `reviews.global` and the `pre-push`
   Checkpoint.

Repeat steps 1 to 4 with `config --accessible`: the Profile prompts start at
`Profile name`, Reviews and Checkpoints open on `Global Configuration` with no
file prompt, and the menu has no copy choice.

Negative control: capture `review-party config --repo "$plain" --config
"$config_file"` with `GIT_CEILING_DIRECTORIES="$run_dir/runtime"` in a
terminal. It exits `1` with `not a git repository` and opens no Hub.

Negative control: the non-TTY command above must fail at the terminal boundary
before either positive route writes a Profile. Restore the baseline by running
doctor after the control and by using a fresh run for each terminal route.

## Gotchas

- `config` without a terminal is expected to refuse with guidance; use the
  explicit `config` subcommands for automation.
- A plain directory under the checkout is still inside a git work tree, so
  the Global-only launch needs `GIT_CEILING_DIRECTORIES` at or above the plain
  directory's parent. Without it the Hub opens on this checkout's repository.
- The normal and accessible paths are different adapters. A successful
  accessible prompt run does not prove the Bubble Tea screen layout, and a
  screen-navigation run does not prove the line-oriented adapter.
- The saved Reviewer and model are configuration data only. This recipe never
  launches a Reviewer or proves authentication.
