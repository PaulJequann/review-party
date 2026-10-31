# Finish setup after an interruption

A new user needs initialization to produce a complete review selection. If setup
stops midway, repeating initialization should preserve published work and offer
the remaining steps rather than duplicate or overwrite it.

## Sub-features

- `SETUP-PARTIAL`: each confirmed publication survives interruption.
- `SETUP-RESUME`: rerunning init completes the missing setup.
- `SETUP-CHECKPOINT`: declining hook setup leaves Git hooks unchanged.
- `SETUP-READY`: readiness distinguishes saved configuration from Reviewer access.

## How to get to it (user POV)

Run `review-party init --repo PATH` in a terminal. Choose the offered Review Party
baseline, configure its execution settings, and review each publication plan.

## Driving it with tmux

Preconditions: set `journey=first-use-recovery`. Launch a fresh owned runtime, but
**do not seed Profiles**. Follow [terminal setup](../references/terminal.md) with
`init` instead of `config` and `--state-dir "$run_dir/runtime/state/review-party"`.
Launch's initial non-TTY init created state only, leaving setup unfinished.

1. Capture the first-review choice and select **Review Party baseline**. Supply
   `codex`, `gpt-5.6-luna`, `high`, and `8m` when requested. Inspect each Plan:
   scope, names, instructions, execution settings, save locations. Publish the
   first offered Profile plan only. Capture its receipt and independent Global
   configuration/file readback; hash the newly published files.
2. At the next setup step use Ctrl+C to interrupt. Capture the last screen and
   process exit. Read back configuration again: the published Profile remains;
   unconfirmed changes must not have appeared. Do not erase partial state.
3. Rerun the identical init command in a new owned session using the same runtime.
   Capture the resumed offers. Complete the missing Profiles, baseline Party,
   and repository selection, inspecting and confirming each plan. Already
   published Profile files must retain their hashes; names must not duplicate.
4. At Checkpoint setup choose the option that declines a Checkpoint. Decline any
   hook installation offer. Compare `.git/hooks` file names, contents, and modes
   before and after: no new or rewritten hook is allowed for this choice.
5. Capture the final readiness report, then `config show`, `profiles`, and
   `doctor` through the capture helper with the explicit repo/config paths.
   Require the baseline Party to resolve to its four Profiles and the selected
   concurrency to agree with the displayed plan. Saved configuration readiness
   must not be reported as verified Reviewer authentication or execution.
6. Rerun init once more. It must preserve saved definitions and selection rather
   than force baseline recreation. Exit any remaining optional setup safely.
   Repeat the interrupted path with `init --accessible` in a fresh runtime.

## Gotchas

- Exact offers depend on what was already published: select by label and verify
  remaining gaps rather than replaying a fixed numeric answer stream.
- The current owned HOME has no authenticated Reviewer. That is expected for
  configuration proof; a real Review remains unverified.
- If no plan boundary permits interruption as described, capture the actual
  screen and report the mismatch. Do not replace this with noninteractive baseline creation.
- This is a new mapped journey; source-grounded instructions require live proof
  before reporting its interruption and resume behavior as verified.
