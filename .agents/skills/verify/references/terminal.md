# Owned terminal drives

Launch a fresh run using the skill, run doctor, then seed only the prerequisites
named by the journey. Run from the repository root. These public commands save
configuration; they never execute the saved Reviewer:

```sh
v=.agents/skills/verify-review-party/scripts/verify.sh
for scope in global repository; do
  "$v" capture "$run_dir" "seed/$scope.txt" -- review-party config profile create "journey-$scope" \
    --scope "$scope" --repo "$run_dir/runtime/repository" --template bugs \
    --reviewer codex --model gpt-5.6-luna --effort high --deadline 8m \
    --config "$run_dir/runtime/config/review-party/config.json" --yes --format json
done
```

Inspect both captures and require exit zero. Start the Hub only after seeding:

```sh
session=$("$v" path "$run_dir" terminal_session)
evidence=$("$v" path "$run_dir" evidence_directory)
mkdir -p "$evidence/$journey"
tmux new-session -d -x 120 -y 30 -s "${session:?}" -- \
  env TERM=xterm-256color EDITOR=true HOME="$run_dir/runtime/home" \
  XDG_CONFIG_HOME="$run_dir/runtime/config" XDG_STATE_HOME="$run_dir/runtime/state" \
  "$run_dir/runtime/bin/review-party" config --repo "$run_dir/runtime/repository" \
  --config "$run_dir/runtime/config/review-party/config.json"
```

Set `journey` to the feature's literal ID before this block. For the accessible
variant append `--accessible` to the product command. For first use replace
`config` with `init` and supply the run's `--state-dir`.

Send a single logical action, inspect the next screen, then continue:

```sh
printf '%s\n' 'Action: open highlighted menu item with Enter' >> "$evidence/$journey/actions.txt"
tmux send-keys -t "${session:?}" Enter
tmux capture-pane -p -t "${session:?}" -S -200 > "$evidence/$journey/01-open.txt"
```

Use `-l` for literal text, `Space` to toggle a multiselect choice, and quoted
`'C-['` for Escape (unquoted brackets fail in zsh). Choose rows by visible label;
Down/Up counts depend on current state. After Tab, confirm the focus bar is on
the intended field before typing. Do not burst an entire form into tmux:
completion can rebuild the form and consume later keys on a different field.
A capture taken before the expected handle appears is not proof; capture again
once the screen settles. Bound waits and report a missing handle rather than
sending speculative keys. Record each action and its following screen.

For 80x24 checks use `tmux resize-window -t "${session:?}" -x 80 -y 24`,
then restore 120x30. Controls and the focused field must remain reachable;
scroll a long plan to its final change before publishing.

After each mutation capture independent readback:

```sh
"$v" capture "$run_dir" "$journey/config.txt" -- review-party config show \
  --repo "$run_dir/runtime/repository" --config "$run_dir/runtime/config/review-party/config.json" --format json
"$v" capture "$run_dir" "$journey/repository-file.txt" -- review-party config file show \
  --scope repository --repo "$run_dir/runtime/repository" --config "$run_dir/runtime/config/review-party/config.json" --format json
"$v" capture "$run_dir" "$journey/global-file.txt" -- review-party config file show \
  --scope global --config "$run_dir/runtime/config/review-party/config.json" --format json
```

Use distinct evidence names for each checkpoint. For absence checks also
capture a sorted file listing and hashes under the owned configuration roots
before and after the action. A nonexistent root is a valid empty baseline.
Exclude managed state from this comparison: initialization creates it.

Exit normally when the journey permits. If still running, stop only this
session with `tmux kill-session -t "=${session:?}"`. Run the skill's cleanup
after failures too. Require runtime absence and retained nonempty screen,
action, and readback artifacts. Never kill tmux globally or remove evidence.
