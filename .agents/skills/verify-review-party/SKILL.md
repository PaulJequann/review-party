---
name: verify-review-party
description: Drive Review Party through its public CLI to verify binary identity, isolated initialization and history, or explicit Profile configuration without launching a Reviewer.
---

# Verify Review Party

Run every command from the repository root. This skill verifies local CLI behavior through an installed binary. It does not prove live Reviewer authentication, model availability, or review execution.

Read [features/README.md](features/README.md) before choosing a recipe. Read the linked feature file for the behavior under test.

## Launch

Create a fresh run and capture the printed run directory:

```sh
run_dir=$(.agents/skills/verify-review-party/scripts/verify.sh launch)
printf '%s\n' "$run_dir"
```

`launch` creates a unique identity under `scratch/verify-review-party/`, installs the current checkout into the run, creates an isolated Git repository, and initializes Review Party with run-owned XDG configuration and state. The CLI is ready when launch's final doctor check prints `doctor: ready`.

Review Party is a short-lived CLI. It owns no server, listening port, session, container, device, or long-running PID. The run manifest records the source revision, installed binary and digest, XDG roots, target repository, managed state directory, and evidence directory. Local recipes need no authentication.

## Doctor

Run the read-only diagnostic before every drive:

```sh
.agents/skills/verify-review-party/scripts/verify.sh doctor "$run_dir"
```

Doctor succeeds only when the run identity is valid, the checkout remains at the recorded Git revision, the installed binary digest matches, the binary identifies itself as Review Party, and the owned repository, XDG roots, state database, and ownership marker exist. An absent Global Configuration file is valid until a command publishes one. Doctor names the first mismatched fact on stderr.

## Drive

Use the literal commands in the selected feature file. Run CLI commands through `capture` so each entry point receives the owned XDG roots and produces one transcript:

```sh
.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" FEATURE/ENTRY.txt -- review-party ARGS...
```

The wrapper resolves `review-party` to the run-owned binary. It does not replace or bypass a product interface.

## Evidence

Evidence lives at `$run_dir/evidence/` and remains after cleanup. Each capture records the argument vector, stdout, stderr, and exit code. Retain the negative control beside the positive transcript. For durable changes, capture a second read-only CLI view as required by the feature recipe.

An exit code alone proves only process completion. Inspect structured output for the claimed fields. A live Reviewer substitution is not allowed. If a recipe later needs a Reviewer, record the unavailable saved Reviewer, model, or authentication as unproved.

## Cleanup

Clean up after successful and failed drives:

```sh
.agents/skills/verify-review-party/scripts/verify.sh cleanup "$run_dir"
```

Cleanup removes only the run's recorded `runtime` directory and is safe to repeat. It preserves the manifest and evidence. A successful cleanup prints `cleanup: complete`; doctor must then fail because the owned runtime is gone.

## Helper

`scripts/verify.sh` requires a POSIX shell, Git, Go, and either `sha256sum` or `shasum`. Its commands are `launch`, `doctor RUN_DIR`, `capture RUN_DIR EVIDENCE -- COMMAND [ARGS...]`, and `cleanup RUN_DIR`. The helper must remain executable.
