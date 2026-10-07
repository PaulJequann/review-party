---
name: dogfood-review-party
description: Run Review Party against the current checkout as a bounded local dogfood review, including isolated setup, concurrent Profiles, result inspection, correction limits, and parallel Paseo worktree safety.
---

# Dogfood Review Party

Run from the repository root. Use complete saved Profiles configured with the
Codex agent, model `gpt-5.6-luna`, maximum supported reasoning (`high`), and an
eight-minute Attempt deadline. Ordinary Review commands execute those settings
exactly. Treat a missing Profile as unavailable. A different agent, model, or
weaker capability requires explicit caller authorization.

## Prepare one owned binary

Create isolated state and configuration under `scratch/`. Choose one binary
branch and keep `dogfood_binary` unchanged for initialization, every Profile,
and inspection.

For a single checkout with no concurrent Review Party build, install through
the normal local path:

```sh
mkdir -p scratch
./scripts/install-local.sh
dogfood_binary=$(command -v review-party)
```

For a Paseo worktree or any checkout that may build concurrently, avoid the
shared `$HOME/.local/bin/review-party`. Install into the worktree:

```sh
mkdir -p scratch
./scripts/install-local.sh "$PWD/scratch/dogfood-bin"
dogfood_binary="$PWD/scratch/dogfood-bin/review-party"
```

Publish the tracked Templates as complete Global Profiles, smoke the chosen
binary, and initialize isolated state:

```sh
REVIEW_PARTY_DOGFOOD_REVIEWER=codex \
REVIEW_PARTY_DOGFOOD_MODEL=gpt-5.6-luna \
REVIEW_PARTY_DOGFOOD_EFFORT=high \
REVIEW_PARTY_DOGFOOD_DEADLINE=8m \
  ./scripts/sync-local-profiles.sh "$PWD/scratch/dogfood-config/review-party/profiles"
./scripts/smoke-installed.sh "$dogfood_binary"
XDG_STATE_HOME="$PWD/scratch/dogfood-state" \
XDG_CONFIG_HOME="$PWD/scratch/dogfood-config" \
  "$dogfood_binary" init --repo .
```

The sync replaces `bugs`, `code-quality`, `documentation`, and `test-audit`.
`init --repo .` without `--baseline` prepares record state and creates no
Profiles.

## Run one cycle

Dogfooding is a small multi-Profile review exercise, not a formal Party or
Review Bundle. Run `bugs` and `code-quality` against the same unchanged Subject.
Add `documentation` when the change touches documentation. Each Profile creates
its own ordinary Review Record. Compare the records instead of merging raw
Reviewer text.

Launch every selected Profile before waiting:

```sh
XDG_STATE_HOME="$PWD/scratch/dogfood-state" \
XDG_CONFIG_HOME="$PWD/scratch/dogfood-config" \
  "$dogfood_binary" run --profile bugs --repo . --format json \
  > scratch/dogfood-bugs.json &
bugs_pid=$!
XDG_STATE_HOME="$PWD/scratch/dogfood-state" \
XDG_CONFIG_HOME="$PWD/scratch/dogfood-config" \
  "$dogfood_binary" run --profile code-quality --repo . --format json \
  > scratch/dogfood-code-quality.json &
code_quality_pid=$!
# If documentation changed, launch it before the wait and add its PID.
# XDG_STATE_HOME="$PWD/scratch/dogfood-state" \
# XDG_CONFIG_HOME="$PWD/scratch/dogfood-config" \
#   "$dogfood_binary" run --profile documentation --repo . --format json \
#   > scratch/dogfood-documentation.json &
# documentation_pid=$!
wait "$bugs_pid" "$code_quality_pid" ${documentation_pid:+"$documentation_pid"}
```

A cycle ends when every selected Profile has a terminal Review Record. Each
output file holds one entry in its `reviews` array. Read the Review ID from
`.reviews[0].id`, then inspect each persisted record with the same
`dogfood_binary`, `XDG_STATE_HOME`, and `XDG_CONFIG_HOME`, using
`--format json`. Verify the recorded Reviewer (`.reviews[0].reviewer.reviewer_id`),
model (`.reviews[0].reviewer.model`), reasoning effort
(`.reviews[0].reviewer.effort`), and result contract revision
(`.reviews[0].profile.result_contract_revision`). Read the findings from
`.reviews[0].findings` and the lifecycle from `.reviews[0].lifecycle`.

Treat an unavailable, malformed, or timed-out Codex run as Incomplete. Retry
only an incomplete Profile, at most once, against the exact unchanged Subject.
Do not rerun a Profile that completed in the cycle. If the retry is incomplete,
report the verification gap and continue to handoff.

## Bound corrections

Use this complete dogfood budget:

1. Run one initial cycle against one unchanged Subject.
2. Inspect every record before editing. Classify each finding as confirmed and
   material, disproved or stale, deferred or out of scope, or incomplete. A
   finding blocks publication only when repository governance makes the
   confirmed finding a blocker.
3. Make at most one correction batch for confirmed, material, in-scope
   findings. Batch related changes and run focused local verification once.
4. If the Subject changed, run one follow-up cycle with the same Profiles. Stop
   after that cycle. Fix only a confirmed release-blocking defect after it, run
   focused local verification, and report that the final Subject was not
   dogfood-reviewed.
5. Start a third cycle only with explicit caller authorization. Context
   compaction, another skill invocation, a finding-bearing result, or a changed
   Subject does not reset the two-cycle budget.

Stop launching new dogfood processes 20 minutes after the initial cycle starts.
Report any cycle or retry that the time limit prevented.

## Clean owned runtime files

Keep dogfood state, output, and temporary builds under `scratch/`. They are
session evidence, not durable repository records. After inspection, remove only
the run-owned binary, state, and configuration when cleanup is appropriate:

```sh
rm -rf "$PWD/scratch/dogfood-bin" \
  "$PWD/scratch/dogfood-state" \
  "$PWD/scratch/dogfood-config"
```

Preserve the `scratch/dogfood-*.json` evidence through handoff.
