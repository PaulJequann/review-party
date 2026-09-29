# Review Party verification features

## Baseline

Run `scripts/verify.sh launch` from the repository root. Launch builds the current checkout and creates one isolated Git repository, HOME, XDG configuration root, XDG state root, explicit managed state directory, and evidence directory. The owned HOME keeps the operator's own skills and user-level executables out of every recipe. A Reviewer installed only under the operator's HOME is therefore reported as unavailable; recipes save Reviewer and model as configuration data and never prove availability. It initializes Review Party once. No authentication or seed Review Records are required.

Every recipe starts from this baseline unless it says otherwise. Use a fresh run for each recipe so Profile and history state cannot leak between features.

## Driving conventions

Run doctor before a recipe and after restoring a negative control. Use `scripts/verify.sh capture` for every public CLI command. Pass `review-party` as the command name so the helper selects the owned binary. Commands below spell run-owned paths under `$run_dir/runtime/` to match the layout `launch` creates; `scripts/verify.sh path RUN_DIR KEY` is the authoritative source for each one.

Source citations name functions rather than line numbers so they survive edits. Choose menu and prompt options by their visible label; any number shown beside a choice is a hint for the current order, not the handle.

Expected negative commands exit nonzero by design. Run them with exit-on-error
disabled, record the status, inspect the named rejection point, and run doctor
before the positive path.

## Terminal drives

The Configuration Hub requires a terminal. Use one fresh `tmux` session for a
terminal recipe, with the run-owned HOME, XDG roots, binary, and session name from
the manifest:

```sh
verify=.agents/skills/verify-review-party/scripts/verify.sh
binary=$("$verify" path "$run_dir" binary)
config_root=$("$verify" path "$run_dir" config_root)
state_root=$("$verify" path "$run_dir" state_root)
home=$("$verify" path "$run_dir" home)
config_file=$("$verify" path "$run_dir" config_file)
target_repository=$("$verify" path "$run_dir" target_repository)
evidence=$("$verify" path "$run_dir" evidence_directory)
session=$("$verify" path "$run_dir" terminal_session)
: "${session:?terminal session is required}"
tmux new-session -d -x 120 -y 30 -s "$session" -- \
  env TERM=xterm-256color EDITOR=true HOME="$home" XDG_CONFIG_HOME="$config_root" XDG_STATE_HOME="$state_root" \
  "$binary" config --repo "$target_repository" --config "$config_file"
```

The session name is unique per run, so parallel runs never drive each other's
terminal. Never run a `tmux` command with an empty `$session`: tmux then
targets the attached session and sends keys to whatever terminal the agent
itself is running in. Keep the `:?` guard in every shell that drives the Hub. Capture with
`tmux capture-pane -p -t "$session" -S -200 > "$evidence/FEATURE/NAME.txt"`.
Send one key or logical line at a time, waiting for the next stable screen
handle before continuing. Use `C-[` for Escape when driving `tmux`. Stop the
session with `tmux kill-session -t "=$session"` (exact-match target) before
`scripts/verify.sh cleanup`. If no PTY driver is available, record the Hub
route as `verified-unreachable` with the exact missing prerequisite; do not
count it as verified.

## Evidence rules

Evidence stays under `$run_dir/evidence/`, which is ignored through the repository's `scratch/` policy. A capture includes the action, stdout, stderr, and exit code. Inspect JSON before claiming a field or state transition. Keep negative and positive captures together. Cleanup preserves evidence but removes all other run-owned resources.

## Feature index

- [Binary identity](binary-identity.md) verifies the installed CLI identity and revision-bound doctor check. This is the tracer feature.
- [Managed initialization and history](managed-initialization.md) verifies idempotent initialization and read-only history against isolated state.
- [Profile configuration](profile-configuration.md) verifies explicit non-interactive Profile publication and a read-only second view.
- [Configuration Hub](configuration-hub.md) verifies the terminal shell, responsive navigation, plan-preview publication, and accessible prompt publication.
- [Skill Templates](skill-templates.md) verifies that a skill in the caller's HOME becomes a Review Profile Template, reports drift and an unavailable source, and appears in the Hub Template picker.
