# Review Party verification features

## Baseline

Run `scripts/verify.sh launch` from the repository root. Launch builds the current checkout and creates one isolated Git repository, XDG configuration root, XDG state root, explicit managed state directory, and evidence directory. It initializes Review Party once. No authentication or seed Review Records are required.

Every recipe starts from this baseline unless it says otherwise. Use a fresh run for each recipe so Profile and history state cannot leak between features.

## Driving conventions

Run doctor before a recipe and after restoring a negative control. Use `scripts/verify.sh capture` for every public CLI command. Pass `review-party` as the command name so the helper selects the owned binary. Paths in commands below are resolved by the helper's run environment.

## Evidence rules

Evidence stays under `$run_dir/evidence/`, which is ignored through the repository's `scratch/` policy. A capture includes the action, stdout, stderr, and exit code. Inspect JSON before claiming a field or state transition. Keep negative and positive captures together. Cleanup preserves evidence but removes all other run-owned resources.

## Feature index

- [Binary identity](binary-identity.md) verifies the installed CLI identity and revision-bound doctor check. This is the tracer feature.
- [Managed initialization and history](managed-initialization.md) verifies idempotent initialization and read-only history against isolated state.
- [Profile configuration](profile-configuration.md) verifies explicit non-interactive Profile publication and a read-only second view.
