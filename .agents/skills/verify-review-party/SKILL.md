---
name: verify-review-party
description: Drive Review Party through its public CLI to verify binary identity, isolated initialization and history, explicit Profile configuration, or terminal-backed Configuration Hub flows without launching a Reviewer.
---

# Verify Review Party

Run every command from the repository root. This skill verifies local CLI behavior through an installed binary, including the terminal-backed Configuration Hub. It does not prove live Reviewer authentication, model availability, or review execution.

Read [features/README.md](features/README.md) before choosing a recipe. Read the linked feature file for the behavior under test.

## Launch

Create a fresh run and capture the printed run directory:

```sh
run_dir=$(.agents/skills/verify-review-party/scripts/verify.sh launch)
printf '%s\n' "$run_dir"
```

`launch` creates a unique identity under `scratch/verify-review-party/`, installs the current checkout into the run, creates an isolated Git repository, and initializes Review Party with run-owned XDG configuration and state. The CLI is ready when launch's final doctor check prints `doctor: ready`.

Review Party is a short-lived CLI. It owns no server, listening port, session, container, device, or long-running PID. The run manifest records the source revision, a fingerprint of the working tree the binary was built from (including uncommitted and untracked files), whether that tree was dirty, the installed binary and digest, XDG roots, target repository, managed state directory, evidence directory, and terminal session name. Local recipes need no authentication.

## Doctor

Run the read-only diagnostic before every drive:

```sh
.agents/skills/verify-review-party/scripts/verify.sh doctor "$run_dir"
```

Doctor succeeds only when the run identity is valid, the checkout remains at the recorded Git revision, the working tree still matches the fingerprint taken at build time, the installed binary digest matches, the binary identifies itself as Review Party, and the owned repository, XDG roots, state database, and ownership marker exist. An absent Global Configuration file is valid until a command publishes one. Doctor names the first mismatched fact on stderr. Editing source after launch invalidates the run; launch a fresh one rather than continuing.

## Drive

Use the literal commands in the selected feature file. Run CLI commands through `capture` so each entry point receives the owned XDG roots and produces one transcript:

```sh
.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" FEATURE/ENTRY.txt -- review-party ARGS...
```

The wrapper resolves `review-party` to the run-owned binary. It does not replace or bypass a product interface.

For a terminal-backed entry point, use the PTY procedure in
[features/README.md](features/README.md). `capture` deliberately does not
allocate a terminal, so it is the right driver for the negative non-TTY
control and for read-only follow-up commands, not for the Hub itself. Start
one fresh PTY session per terminal recipe, capture the screen after each
stable state, send one action at a time, and stop the session before cleanup.

## Evidence

Evidence lives at `$run_dir/evidence/` and remains after cleanup. Each capture records the argument vector, stdout, stderr, and exit code. Terminal recipes retain screen captures beside CLI transcripts. Retain the negative control beside the positive transcript. For durable changes, capture a second read-only CLI view as required by the feature recipe. Run expected nonzero controls with exit-on-error disabled, record their status, and run doctor before continuing.

An exit code alone proves only process completion. Inspect structured output for the claimed fields. A live Reviewer substitution is not allowed. If a recipe later needs a Reviewer, record the unavailable saved Reviewer, model, or authentication as unproved.

## Cleanup

Clean up after successful and failed drives:

```sh
.agents/skills/verify-review-party/scripts/verify.sh cleanup "$run_dir"
```

Stop every PTY session before cleanup. Cleanup removes only the run's recorded `runtime` directory and is safe to repeat. It preserves the manifest and evidence. A successful cleanup prints `cleanup: complete`; doctor must then fail because the owned runtime is gone.

## Helper

`scripts/verify.sh` requires a POSIX shell, Git, Go, and either `sha256sum` or `shasum`. Its commands are `launch`, `doctor RUN_DIR`, `path RUN_DIR KEY`, `capture RUN_DIR EVIDENCE -- COMMAND [ARGS...]`, and `cleanup RUN_DIR`. `path` prints one manifest value, such as `binary`, `config_file`, or `terminal_session`; use it instead of spelling run-owned paths by hand. Terminal recipes additionally require `tmux` or an equivalent PTY driver, plus the driver's screen-capture and key-input operations. The helper must remain executable.
