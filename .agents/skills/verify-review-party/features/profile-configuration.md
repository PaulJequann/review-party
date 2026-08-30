# Profile configuration

Review Party creates a complete saved Profile from a packaged Template and exposes the published definition through read-only configuration commands.

## Sub-features

- `CONFIG-PROFILE-CREATE`
- `CONFIG-PROFILE-READBACK`

## Source evidence

- `cmd/review-party/standard_commands.go:148-166` registers the explicit
  `config` command family and Profile mutation subcommands.
- `cmd/review-party/config_profile_mutations.go:12-41` builds a typed Profile
  draft, checks the model choice, plans the mutation, and routes publication
  through the confirmation boundary.
- `cmd/review-party/config_show.go:30-48` renders read-only configuration
  results through the command output path.

Drift: none. The command remains the non-interactive Profile path; the
terminal-backed Hub is covered separately by
[Configuration Hub](configuration-hub.md).

## How to get to it (user POV)

- Run `review-party config profile create NAME` with Template and Reviewer settings.
- Run `review-party explain NAME --format json` to inspect the compiled Profile.
- Run `review-party config validate --scope global --format json` to validate the authored file.

## Driving it with the CLI

Preconditions: Start from a fresh launched baseline. This recipe uses the saved Reviewer identity only as configuration and never launches it.

- **Check readiness.** Run `.agents/skills/verify-review-party/scripts/verify.sh doctor "$run_dir"` and require `doctor: ready`.
- **Create the Profile.** Run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" profile-configuration/create.json.txt -- review-party config profile create verify-bugs --template bugs --reviewer codex --model gpt-5.6-luna --effort high --deadline 8m --config "$run_dir/runtime/config/review-party/config.json" --yes --format json`. The command exits `0` and its Plan reports the created Global Profile.
- **Confirm through a second view.** Run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" profile-configuration/show.json.txt -- review-party explain verify-bugs --repo "$run_dir/runtime/repository" --config "$run_dir/runtime/config/review-party/config.json" --format json`. With no same-named Repository Profile, the unqualified name resolves to the created Global Profile. The JSON names `verify-bugs`, `codex`, `gpt-5.6-luna`, `high`, and `8m`.
- **Validate authored configuration.** Run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" profile-configuration/validate.json.txt -- review-party config validate --scope global --config "$run_dir/runtime/config/review-party/config.json" --format json`. The Global scope is valid and the command exits `0`.

Negative control: Before creating the Profile, run the same `config profile create` command without `--yes`, captured at `profile-configuration/negative-confirmation.txt`, with exit-on-error disabled. Record the nonzero status from the capture and require its transcript to contain `JSON mutations require --yes`. Then capture `review-party explain verify-bugs` with the same explicit repository and configuration paths at `profile-configuration/negative-readback.txt`; it must exit nonzero and report `unknown review profile "verify-bugs"`. Confirm that the run-owned Profile path is still absent, run doctor, and let the positive create command establish the intended published baseline.

## Gotchas

- A saved Reviewer and model do not prove the harness is installed, authenticated, or currently accepts that model. This recipe proves configuration only.
- JSON and non-terminal mutations require `--yes`. Do not use a PTY to bypass this boundary in an automation recipe.
