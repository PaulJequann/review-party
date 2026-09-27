# Configuration Hub

Review Party opens a recurring terminal Configuration Hub for scoped
configuration. The normal path is a Bubble Tea shell with browse-first areas
and in-program plan preview; `--accessible` uses non-redrawing prompts for the
same configuration operations.

## Sub-features

- `HUB-SHELL-NAVIGATION`
- `HUB-PLAN-PREVIEW-PUBLISH`
- `HUB-ACCESSIBLE-PUBLISH`

## Source evidence

- `newConfigCommand` in `cmd/review-party/standard_commands.go` exposes
  `config`, the `--accessible` flag, and the explicit command family.
- `executeConfigurationHub` in `cmd/review-party/config_hub.go` requires
  terminal input/output, resolves the repository, and routes the selected mode
  through the Hub.
- `newAreaSpecs` in `internal/configurationhub/model.go` defines the Overview,
  Profiles, Parties, Repository Reviews, and Review Changes areas;
  `updateNavigation` and `openSelectedArea` own menu navigation and area
  opening; `View` owns the alt-screen title and terminal view.
- `renderPlanPreviewFrame` and `renderPlanActionBar` in
  `internal/configurationhub/view_chrome.go` render the plan preview and
  publish controls; `receivePlan` and `receivePublishResult` in
  `internal/configurationhub/interactive_forms.go` handle plan receipt,
  publication, refreshed snapshots, and the success outcome.
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
4. **Profile fields.** Send `Down` to choose Repository scope, then Tab
   through the fields entering, in order: `ui-bugs`, `codex`, `gpt-5.6-luna`,
   `high`, and `8m`. Require each focused input to remain inside the box. On
   the deadline field, send Shift+Tab and confirm focus returns to Reasoning
   effort without changing `high`. Send Tab to return to the deadline, run
   `tmux resize-window -t "$session" -x 120 -y 30`, and capture
   `profile-form-deadline.txt` showing the focused deadline and navigation
   controls. Send Enter to continue to the instruction source; Tab alone does
   not submit the form.
5. **Instructions.** Accept the default Template instruction source, accept
   the first packaged template, and accept the default
   `Edit instructions with $EDITOR?` answer.
6. **Plan preview.** Wait for `Plan preview` and capture `plan-preview.txt`.
   It must show `ui-bugs`, `codex`, `gpt-5.6-luna`, and the Repository scope,
   with `p` available to publish.
7. **Publish.** Send `p`, wait for `Published profile "ui-bugs"`, and capture
   `published.txt`.
8. **Copy form.** Send `esc` to return to the menu, then `Enter` to reopen
   Profiles. Send `p` (copy) and wait for
   `Repository Profile to copy to Global Configuration`. Capture
   `copy-form.txt`; it must list the published `ui-bugs` among the selectable
   Repository Profiles. Send `C-[` twice to return to the menu without
   publishing the copy.
9. **Exit.** Send `q` and require the session to exit. The run-owned
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
| Configuration scope | the `Repository` choice |
| Instruction source | the `Template` choice |
| Template | the first packaged template |
| Edit instructions | `n` |
| Name, Reviewer, model, effort, deadline | `ui-accessible`, `codex`, `gpt-5.6-luna`, `high`, `8m` |
| Publish this complete plan? | `y` |
| Next action | the choice that copies a Repository Profile to Global Configuration |
| Profile to copy | `ui-accessible` under the `[repository]` label |
| Publish the copy? | `n` |
| Configuration Hub menu | the `Exit` choice |
| Discard unfinished drafts and exit? | `y` |

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

Negative control: the non-TTY command above must fail at the terminal boundary
before either positive route writes a Profile. Restore the baseline by running
doctor after the control and by using a fresh run for each terminal route.

## Gotchas

- `config` without a terminal is expected to refuse with guidance; use the
  explicit `config` subcommands for automation.
- The normal and accessible paths are different adapters. A successful
  accessible prompt run does not prove the Bubble Tea screen layout, and a
  screen-navigation run does not prove the line-oriented adapter.
- The saved Reviewer and model are configuration data only. This recipe never
  launches a Reviewer or proves authentication.
