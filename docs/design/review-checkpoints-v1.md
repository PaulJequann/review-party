# Review Checkpoints v1

Status: accepted on 2026-10-01; not implemented. Ownership is recorded in
[ADR 0001](../adr/0001-project-declared-review-checkpoints.md). Hook facts per
Caller Agent are in
[Caller Agent hooks](../research/caller-agent-hooks-2026-10-01.md).

## Problem

A repository can select Reviews, but nothing makes a Caller Agent run them.
Teams want the practice "every push is reviewed" to hold without relying on
memory, and they differ on how hard it should hold: some want an instruction,
some want a refusal.

## Model

A **Review Checkpoint** is a point in the workflow, before commit or before
push, at which the repository expects its Review selection to have covered the
change. A Checkpoint checks for Reviews; it never starts one.

**Coverage** holds when, for every Profile in the expanded Review selection, a
completed Review examined exactly the Checkpoint's content changes.

- A content change is a set of entries `(path, content before, content after)`,
  with contents identified by Git blob ID. Untracked files are hashed with
  `git hash-object`.
- A Review covers a change when its recorded content changes equal the
  Checkpoint's. Commit IDs, repository path, and patch formatting do not take
  part. Rewording a commit, pushing from a worktree, or rebasing over upstream
  commits that do not touch the changed files keeps Coverage. Partial staging
  and a rebase that changes a file's starting content lose it.
- A push range is covered when one Review covers the whole range, or when each
  commit in the range is covered by its own Review.
- Profiles match by scoped name, at any Profile Revision.
- An Incomplete Review never covers.

Each Checkpoint requires either `reviewed` (Coverage) or `judged` (Coverage
and a verdict on every Finding). `judged` depends on
[finding feedback](finding-feedback-v1.md).

A **Checkpoint Exemption** treats a change as needing no Coverage:

- `exempt_paths` removes matching paths from the requirement. A change whose
  paths are all exempt passes.
- `small_change_lines` passes a change whose total added and deleted lines do
  not exceed the limit. Size is measured over the whole Checkpoint change, so
  splitting a push into small commits does not pass each commit separately.

A **Checkpoint Waiver** records that one exact content change passed without
Coverage, with a required reason. It is keyed like Coverage, so it never
carries over to another change. `waivers` declares who may waive: `anyone`,
`human` (terminal confirmation), or `none`. The default is `human`.

## Declaration

The team declares Checkpoints in the committed Repository Configuration:

```json
{
	"schema_version": 1,
	"reviews": {"repository": [{"party": "baseline"}]},
	"checkpoints": {
		"pre-push": {
			"requirement": "reviewed",
			"exempt_paths": ["*.md", "docs/**"],
			"small_change_lines": 0,
			"waivers": "human",
			"integrations": ["git", "claude-code", "codex", "agents-md"]
		}
	}
}
```

`integrations` is the team floor. Each Caller may install more Integrations
in files that are not committed. Nothing prevents a determined bypass, so a
missing team-floor Integration is something `init` and `doctor` report, not
something Review Party blocks.

## Checkpoint Integrations

Every Integration is a single line that calls Review Party. Each Caller
Agent's input format and refusal format stay inside Review Party, so
installed files do not change when the logic changes.

| Integration | Installed into | Command |
|---|---|---|
| `git` | The repository's hook manager (see below) | `review-party checkpoint hook git pre-push` |
| `claude-code` | `.claude/settings.json` (team) or `.claude/settings.local.json` (personal), `PreToolUse` with `if: "Bash(git push*)"` | `review-party checkpoint hook claude-code` |
| `codex` | `.codex/hooks.json` (team) or `~/.codex/hooks.json` (personal), `PreToolUse` | `review-party checkpoint hook codex` |
| `agents-md` | A marked block in `AGENTS.md`, or `CLAUDE.md` when only that exists | None; advisory text |

`review-party checkpoint check pre-push` gives the same answer without a hook.

Git hooks are added through the manager the repository already uses, and
existing hook lines are never overwritten or reordered:

- **lefthook, husky, pre-commit:** a step is added to the committed config,
  as team floor, through a reviewed Plan.
- **`core.hooksPath` or plain `.git/hooks`:** a marked block is inserted into
  `pre-push`, or the file is created. This is per clone, so each Caller's
  `init` installs it. Linked worktrees share the common hooks directory.

### Refusal

When Coverage is missing, the hook refuses with one line that names the
Checkpoint and the next command:

- No Review: `review-party run --base <base> --head <head>` with refs filled in.
- A Review still running: `review-party wait <id>`.
- Unjudged Findings, under `judged`: `review-party finding record <id>`.

The refusal never calls the change bad. It mentions waivers only when
`waivers` is `anyone`.

### Missing binary

If `review-party` is not on `PATH`, every Integration warns on one line and
allows the action. A missing binary is a setup gap. Blocking teammates who
have not installed Review Party would push teams to remove the hooks. Claude
Code already treats exit 127 as non-blocking; Git hook managers do not, so the
shim checks `command -v review-party` first.

### Known limits

- Codex matches hooks on tool name only, so the Codex hook runs on every shell
  command. `checkpoint hook codex` must classify the command and exit before
  loading configuration or the ledger. `init` warns about this cost. Measure
  it once integrated, before calling slice 4 done.
- Codex activates a project hook only after the Caller approves it through
  Codex `/hooks`. `init` reports that step. `doctor` does not read Codex trust
  state.
- Git hooks are skipped by `--no-verify`. Caller Agent hooks are not.

### AGENTS.md block

The block is generated from the declared Checkpoints and stays about three
lines, for example: "Before pushing, review the change with `review-party run
--base <upstream> --head HEAD` and record a verdict for each Finding.
`review-party checkpoint --help` has details." Its job is to make the agent
review before the hook would refuse.

## Initialization and doctor

[Guided initialization](guided-init-v1.md) gains two steps after the
selection: Checkpoints (show the declared ones, or offer pre-push by default,
pre-commit, or none, with requirement, exemptions, and waiver policy) and
Integrations (install the per-clone parts of the team floor, and offer
personal additions preselected from detected `claude` and `codex`). When the
selection contains a documentation Profile, offering a `*.md` exemption warns
that it would disable that Profile for those files. The final report lists
remaining manual steps, such as Codex `/hooks` approval.

`doctor` reports each of the following with the command that fixes it:

- Unresolved Global names in the selection.
- Missing team-floor Integrations.
- Edited hook blocks.
- A stale AGENTS.md block.
- Exemptions that conflict with the selection.
- Waivers recorded in the last 30 days.

## Delivery

Each slice works without the ones after it.

1. Guided initialization.
2. Coverage. Reviews record their content changes, which needs a ledger
   migration. `checkpoint check` reports Coverage.
3. Checkpoint declaration, Git Integrations, Exemptions, and Waivers.
4. Claude Code and Codex Integrations, with the Codex cost measured.
5. The AGENTS.md block and `doctor` reporting.
6. The `judged` requirement, after finding feedback ships.

## Open questions

- **Checkpoint base.** A pre-push to a new branch has a zero remote object.
  Recommendation: use the merge base with the remote default branch.
- **Commit forms.** `git commit -a` and `git commit <paths>` commit content
  that is not staged when the Caller Agent hook runs. The hook must derive the
  commit's change from the command, or treat a form it cannot parse as
  uncovered.
- **Personal Checkpoints.** A Caller who wants a Checkpoint in a shared
  repository whose team declares none has nowhere to declare it. Global
  Configuration applies to every repository. Recommendation: defer until
  asked for; a solo repository declares Checkpoints in its own Repository
  Configuration.

## Out of scope

- Hooks that run Reviews.
- Per-Profile Exemptions.
- An environment-variable bypass.
- opencode, Cursor, and Gemini Integrations. opencode follows once its plugin
  path is verified end to end.
