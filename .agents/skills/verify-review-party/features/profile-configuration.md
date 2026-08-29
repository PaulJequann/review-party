# Profile configuration

Review Party creates a complete saved Profile from a packaged Template and exposes the published definition through read-only configuration commands.

## Sub-features

- `CONFIG-PROFILE-CREATE`
- `CONFIG-PROFILE-READBACK`

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

Negative control: Before creating the Profile, run the same `config profile create` command without `--yes`, captured at `profile-configuration/negative-confirmation.txt`. The non-terminal confirmation boundary must reject the mutation with a nonzero exit code. Then run `review-party explain verify-bugs` and require it to report that the Profile is missing. The positive create command restores the intended baseline.

## Gotchas

- A saved Reviewer and model do not prove the harness is installed, authenticated, or currently accepts that model. This recipe proves configuration only.
- JSON and non-terminal mutations require `--yes`. Do not use a PTY to bypass this boundary in an automation recipe.
