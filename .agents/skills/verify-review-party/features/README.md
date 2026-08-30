# Review Party verification features

## Baseline

Run `scripts/verify.sh launch` from the repository root. Launch builds the current checkout and creates one isolated Git repository, XDG configuration root, XDG state root, explicit managed state directory, and evidence directory. It initializes Review Party once. No authentication or seed Review Records are required.

Every recipe starts from this baseline unless it says otherwise. Use a fresh run for each recipe so Profile and history state cannot leak between features.

## Driving conventions

Run doctor before a recipe and after restoring a negative control. Use `scripts/verify.sh capture` for every public CLI command. Pass `review-party` as the command name so the helper selects the owned binary. Paths in commands below are resolved by the helper's run environment.

Expected negative commands exit nonzero by design. Run them with exit-on-error
disabled, record the status, inspect the named rejection point, and run doctor
before the positive path.

## Terminal drives

The Configuration Hub requires a terminal. Use one fresh `tmux` session for a
terminal recipe, with the run-owned XDG roots and binary from `ownership.tsv`:

```sh
binary=$(awk -F '\t' '$1 == "binary" {print $2}' "$run_dir/ownership.tsv")
config_root=$(awk -F '\t' '$1 == "config_root" {print $2}' "$run_dir/ownership.tsv")
state_root=$(awk -F '\t' '$1 == "state_root" {print $2}' "$run_dir/ownership.tsv")
config_file=$(awk -F '\t' '$1 == "config_file" {print $2}' "$run_dir/ownership.tsv")
target_repository=$(awk -F '\t' '$1 == "target_repository" {print $2}' "$run_dir/ownership.tsv")
session=verify-review-party-hub
tmux new-session -d -x 120 -y 30 -s "$session" -- \
  env TERM=xterm-256color XDG_CONFIG_HOME="$config_root" XDG_STATE_HOME="$state_root" \
  "$binary" config --repo "$target_repository" --config "$config_file"
```

Capture with `tmux capture-pane -p -t "$session" -S -200 > "$run_dir/evidence/…"`.
Send one key or logical line at a time, waiting for the next stable screen
handle before continuing. Use `C-[` for Escape when driving `tmux`. Stop the
session before `scripts/verify.sh cleanup`. If no PTY driver is available,
record the Hub route as `verified-unreachable` with the exact missing
prerequisite; do not count it as verified.

## Evidence rules

Evidence stays under `$run_dir/evidence/`, which is ignored through the repository's `scratch/` policy. A capture includes the action, stdout, stderr, and exit code. Inspect JSON before claiming a field or state transition. Keep negative and positive captures together. Cleanup preserves evidence but removes all other run-owned resources.

## Feature index

- [Binary identity](binary-identity.md) verifies the installed CLI identity and revision-bound doctor check. This is the tracer feature.
- [Managed initialization and history](managed-initialization.md) verifies idempotent initialization and read-only history against isolated state.
- [Profile configuration](profile-configuration.md) verifies explicit non-interactive Profile publication and a read-only second view.
- [Configuration Hub](configuration-hub.md) verifies the terminal shell, responsive navigation, plan-preview publication, and accessible prompt publication.
