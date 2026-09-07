# Configuration Hub

Review Party opens a recurring terminal Configuration Hub for scoped
configuration. The normal path is a Bubble Tea shell with browse-first areas
and in-program plan preview; `--accessible` uses non-redrawing prompts for the
same configuration operations.

## Sub-features

- `HUB-SHELL-NAVIGATION`
- `HUB-PLAN-PREVIEW-PUBLISH`
- `HUB-ACCESSIBLE-PUBLISH`

## How to get to it (user POV)

- Run `review-party config --repo PATH --config PATH` in a terminal.
- Run `review-party config --accessible --repo PATH --config PATH` for
  non-redrawing prompts in a terminal.
- Use the Profiles area to create a Profile, review its plan, and publish it.

## Source evidence

- `cmd/review-party/standard_commands.go:148-166` exposes `config`, the
  `--accessible` flag, and the explicit command family.
- `cmd/review-party/config_hub.go:26-53` requires terminal input/output,
  resolves the repository, and routes the selected mode through the Hub.
- `internal/configurationhub/model.go:79-87` defines the current Overview,
  Profiles, Parties, Repository Reviews, and Review Changes areas;
  `internal/configurationhub/model.go:388-423` owns menu navigation and area
  opening; `internal/configurationhub/model.go:447-460` owns the alt-screen
  title and terminal view.
- `internal/configurationhub/view_chrome.go:10-35` renders the plan preview
  and publish controls; `internal/configurationhub/model.go:580-616` handles
  plan receipt, publication, refreshed snapshots, and the success outcome.
- `internal/configurationhub/editors.go:75-88` and `:160-217` implement the
  accessible form and plan/publication flow.

Drift: this feature was absent from the authored map. The current source has a
separate terminal boundary and a separate accessible adapter, so both entry
points need live coverage.

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
procedure in `features/README.md`. Wait for `Menu`, then perform this
sequence, capturing each named state:

1. Send `Down`, then `Enter` to open Profiles, and send `n` for a new Profile.
2. Wait for `Configuration scope`. Send `Down` to choose Repository scope,
   then Tab through the fields entering, in order: `ui-bugs`, `codex`,
   `gpt-5.6-luna`, `high`, and `8m`.
3. Accept the default Template instruction source, accept the first packaged
   template, and accept the default `Edit instructions with $EDITOR?` answer.
4. Capture `configuration-hub/menu.txt`, `profiles-browser.txt`, and
   `profile-form.txt` as those states appear. Wait for `Plan preview` and
   capture `configuration-hub/plan-preview.txt`. It must show `ui-bugs`,
   `codex`, `gpt-5.6-luna`, and the Repository scope, with `p` available to
   publish.
5. Send `p`, wait for `Published profile "ui-bugs"`, capture
   `configuration-hub/published.txt`, send `esc` to return to the menu, then
   send `Enter` to reopen Profiles. Send `p` (copy)
   and wait for `Repository Profile to copy to Global Configuration`; capture
   `configuration-hub/copy-form.txt`. It must show the published `ui-bugs`
   among the selectable Repository Profiles. Send `C-[` twice to return to
   the menu without publishing the copy.
6. Send `q`, and require the session to
   exit. The run-owned repository Profile at
   `$run_dir/runtime/repository/.reviewparty/profiles/ui-bugs/profile.json`
   must exist, and no `ui-bugs` Profile may exist in the Global configuration
   root.

The expected evidence is a wide menu with `Menu` and `Overview` in separate
panes, the Profiles browser and Profile form handles, a copy form handle, a
plan preview, and a published outcome. Run doctor, then use `capture` for a
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
next logical line; Huh selects use numeric choices and confirmations use `y`
or `n`. The representative Profile flow is:

```text
2        # Configuration Hub menu: Profiles
2        # Configuration scope: Repository
1        # Instruction source: Template
1        # First packaged template
n        # Keep template instructions
ui-accessible
codex
gpt-5.6-luna
high
8m
y        # Publish this complete plan?
6        # Copy a Repository Profile to Global Configuration
1        # ui-accessible (repository)
n        # Decline publishing the copy
8        # Configuration Hub menu: Exit
y        # Discard unfinished drafts and exit
```

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
