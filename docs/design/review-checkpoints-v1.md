# Review Checkpoints v1

Status: accepted on 2026-10-01; all six slices implemented. Ownership is recorded in
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
`human` (terminal confirmation), or `none`. The default is `human`. A
recorded waiver counts only while the current policy would allow recording
it, so tightening the policy retires waivers it no longer permits.

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
| `claude-code` | `.claude/settings.json` (team) or `.claude/settings.local.json` (personal), one `PreToolUse` entry with `if: "Bash(git *)"` | `review-party checkpoint hook claude-code \|\| true` behind a `command -v` guard that warns |
| `codex` | `.codex/hooks.json` (team) or `$CODEX_HOME/hooks.json`, by default `~/.codex/hooks.json` (personal), one `PreToolUse` entry with matcher `Bash` | `review-party checkpoint hook codex \|\| true` behind a `command -v` guard |
| `agents-md` | A marked block in `AGENTS.md`, or `CLAUDE.md` when only that exists | None; advisory text |

`review-party checkpoint check pre-push` gives the same answer without a hook.

Git hooks are added through the manager the repository already uses, and
existing hook lines are never overwritten or reordered:

- **husky:** a marked block is inserted into the committed hook script.
- **lefthook, pre-commit:** a snippet is printed to add to the committed
  config by hand. Slice 3 decisions below give the exact rules.
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

If `review-party` is not on `PATH`, every Integration allows the action. A
missing binary is a setup gap. Blocking teammates who have not installed
Review Party would push teams to remove the hooks. Every shim checks `command
-v review-party` first. The git hook and Claude Code warn on one line. The
Codex entry allows silently, because Codex would print the warning before
every shell command. `init` reports the missing binary instead. Both agent
entries also end in `|| true`, so no exit status from an old or broken
`review-party` blocks a tool call. Slice 4 decisions below give the reasons.

### Known limits

- Codex matches hooks on tool name only, so the Codex hook runs on every shell
  command. `checkpoint hook codex` classifies the command and exits before
  loading configuration or the ledger. The installer prints the measured cost,
  which Slice 4 decisions below record.
- Codex activates a project hook only after the Caller approves it through
  Codex `/hooks`. `init` reports that step. `doctor` does not read Codex trust
  state.
- Git hooks are skipped by `--no-verify`. Caller Agent hooks are not.
- A `review-party` on `PATH` that predates `checkpoint hook` fails on the
  unknown command, and `|| true` lets the tool call run unchecked without a
  warning. Its message is on stderr, which Claude Code sends to its debug log
  when the hook exits 0
  ([Exit code 0](https://code.claude.com/docs/en/hooks#exit-code-0)). The
  Codex documentation does not say what Codex does with that stderr.

### AGENTS.md block

The block is generated from the declared Checkpoints and stays about three
lines, for example: "Before pushing, review the change with `review-party run
--base <upstream> --head HEAD`; the pre-push Checkpoint refuses a push no
completed Review covers. `review-party checkpoint --help` has details." Its
job is to make the agent review before the hook would refuse.

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
- Markdown exemptions that conflict with a selected documentation Profile.
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

## Slice 3 decisions

**Pattern language.** An `exempt_paths` pattern is relative to the repository
root and separated by `/`. Each segment is a Go `path.Match` pattern, and a
`**` segment matches zero or more directories. A pattern without `/` matches
the base name at any depth, so `*.md` exempts `docs/guide.md`. Validation
rejects an empty pattern, a leading `/`, an empty, `.`, or `..` segment, and a
malformed segment. Exemptions filter both the Checkpoint change and each
candidate Review's recorded change before they are compared, so a Review of
the code alone covers a push that also edits exempt files.

**Hook error policy.** The git hook refuses, with exit 1, only when it reached
a decision and the change does not pass. When `review-party` is missing, the
configuration does not load, the ref lines do not parse, git fails, or no base
can be found, the hook prints one warning line and exits 0. An undeclared
Checkpoint exits 0 silently. A ref deletion is skipped. When this clone has
the remote object, the base is its merge base with the pushed object, so a
forced push does not count the commits it drops. A new branch, whose remote
object is zero or not present locally, uses the merge base with
`refs/remotes/<remote>/HEAD`, then `refs/remotes/origin/HEAD`. Every
installed form stops git only on exit 1. Any other status, such as a usage
error from a `review-party` that predates `checkpoint hook`, warns with the
status and allows.

**Hook tools.** The installer edits only hook scripts. Plain hooks,
`core.hooksPath`, and husky get a marked block after the shebang, or a new
`#!/bin/sh` file with mode 0755. A block already present is left alone, and a
block that differs from the generated one is reported as edited. Installing
into an existing hook script also makes it executable, since git skips one
that is not. A compiled hook, or one whose shebang names an interpreter other
than a POSIX shell, gets a printed command to call by hand instead. The
pre-push block captures git's ref lines and feeds them back as standard input,
so a hook that reads them still receives them byte for byte. A symlinked hook
is written at its target, even a target that does not exist yet, so the link
survives.

lefthook and the pre-commit framework get a printed snippet, because editing
YAML without a parser cannot be made safe. A Checkpoint counts as installed
there when a line that is not a comment calls `review-party checkpoint hook git
<checkpoint>`. lefthook pastes the hook's arguments into the shell text
unquoted, so the lefthook pre-push snippet reads the remote from a quoted
heredoc. The pre-commit framework passes pre-push facts as `PRE_COMMIT_*`
variables rather than git's ref lines, so its snippet rebuilds one ref line
from them. Every form passes the hook's arguments after `--`, so a remote named
like an option cannot turn the call into a usage error that allows the push.

lefthook, husky, and the pre-commit framework run only after each clone
installs them. The installer and init print `lefthook install`, `npx husky`, or
`pre-commit install --hook-type <checkpoint>` when git's hook in this clone is
not the one that tool generates.

Installed hooks never carry `--config`. Each Caller's hook loads that Caller's
default Global Configuration, and install warns when it was given another one.
Hook edits go straight to disk after confirmation, not through a configuration
Plan, because hook files are not Review Party configuration.

## Slice 4 decisions

**Agent protocol.** Claude Code and Codex send the same `PreToolUse` fields
the hook reads, `tool_name`, `tool_input.command`, and `cwd`. Both read the
same JSON deny on stdout with exit 0 and pass its reason to the model
([Claude Code PreToolUse decision control](https://code.claude.com/docs/en/hooks#pretooluse-decision-control),
[Codex hooks](https://learn.chatgpt.com/docs/hooks)). Each agent is therefore
one table entry, and the decision is the git hook's. A refusal prints
`{"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision":
"deny", "permissionDecisionReason": "<the git hook's line>"}}` and exits 0.
An undecidable case prints `{"systemMessage": "warning: ..."}` and exits 0,
which both agents show without blocking. Only a failed write exits 1. Both
agents also block on exit 2, but the hook never uses it, because an old
`review-party`, a usage error, and a Go panic exit 2 too, and under Codex
that would block every shell command. A tool other than `Bash` is not
relevant.

**Classification.** The hook returns before it reads configuration, the
ledger, or git unless the command contains `git` and either `push` or
`commit`. It then splits the command on `&&`, `||`, `;`, `|`, `&`, and
newlines. It removes quoting, drops redirections and here-document bodies,
and keeps a command substitution as one opaque word. It expands nothing. A
segment is relevant when, after `VAR=value` words and wrappers such as `env`,
`command`, and `time`, its first word is `git` and its first word after git's
global options is `push` or `commit`. `-C` moves the repository the hook
decides. `--git-dir`, `--work-tree`, a `GIT_DIR`, `GIT_WORK_TREE`, or
`GIT_INDEX_FILE` assignment, a `-c` or `--config-env` key under `push.`,
`remote.`, or `branch.`, options to a wrapper such as `env -u NAME`, an
abbreviation of a long option the hook reads, such as `--mir`, or a global
option the hook does not know leaves the Checkpoint undecided, with a
warning. A command whose last `--dry-run` or `--no-dry-run` is `--dry-run`
sends nothing, so it is not relevant.

**Push.** The hook rebuilds the ref lines git would pass to pre-push and
decides them as the git hook does. `git push` without a refspec pushes the
current branch to the remote the command names, else to
`branch.<name>.pushRemote`, `remote.pushDefault`, the upstream remote, or
`origin`. The branch lands on its upstream when the upstream is on that remote
and `push.default` is unset, `simple`, or `upstream`. Otherwise it lands on the
same name. Another `push.default`, or push refspecs configured for the remote,
leave the Checkpoint undecided. A refspec's remote object is the clone's tracking ref for the destination, or
zero, which takes the new-branch base. `--all`, `--branches`, `--mirror`,
`--tags`, `--delete`, `--prune`, a pattern refspec, an unresolvable revision,
and a detached HEAD without a refspec leave the Checkpoint undecided.

**Chained commands.** A relevant segment with any segment before it is
refused when its Checkpoint is declared, as in `git add -A && git commit -m x`.
The content at hook time is not what git will see, and the hook does not run
the earlier commands to find out. The refusal asks the Caller to run that `git
push` or `git commit` as its own command. A `cd` to one literal word is not a
preceding command: it moves the directory the later segments are decided in,
resolved against `cwd` and any earlier `cd`, so `cd app && git push` and `cd
app; git commit -m x` are decided in `app`. Agents emit that form constantly.
The `cd` counts only when it runs unconditionally in the hook's shell: outside
a subshell, ending in `&&`, `;`, or a newline, and after a segment that does
too. A `cd` to a word with a variable, a command substitution, `~`, or a glob,
`cd -`, a `cd` with options, and a `cd` in a pipeline, a subshell, or after
`||` still count as preceding commands. Segments after the relevant one, such
as `| tail`, do not matter.

**Commit forms.** Plain `git commit` decides the staged content, as
`checkpoint check pre-commit` does. `-a` and `--all` decide the tracked
working-tree changes against HEAD, which is what they commit. Pathspecs,
`--only`, `--include`, `-p`, `--patch`, `--interactive`, and
`--pathspec-from-file` commit content the hook cannot see, so they are refused
with one line telling the Caller to stage the change and run `git commit`
without paths. This closes the commit-forms question that slice 3 left open.

**Missing binary.** Both entries fail open. The Claude Code entry is

```sh
if command -v review-party >/dev/null 2>&1; then review-party checkpoint hook claude-code || true; else echo '{"systemMessage":"warning: review-party is not on PATH, so Review Checkpoints were not checked"}'; fi
```

so a missing binary prints the one-line warning as a JSON `systemMessage` and
the git command runs. The Codex entry is

```sh
if command -v review-party >/dev/null 2>&1; then review-party checkpoint hook codex || true; fi
```

and allows silently, because Codex would print a warning before every shell
command. `|| true` makes the exit status 0 whatever `review-party` returns, so
an old binary without `checkpoint hook`, a usage error after a flag change,
or a panic cannot block a tool call. The refusal still reaches the agent,
because it is JSON on stdout rather than an exit status. The installer warns
when `review-party` is not on `PATH`.

**Entries.** Each agent gets one entry that every declared Checkpoint shares,
since the hook decides which Checkpoint a command meets. The Claude Code entry
has matcher `Bash` and the `if` rule `Bash(git *)`, so the hook starts only for
git commands. Claude Code checks an `if` rule against each subcommand after
stripping `VAR=value` words, so `Bash(git *)` also catches `git -C dir push`,
`FOO=bar git push`, and `npm test && git push`, where `Bash(git push*)` would
miss `git -C`
([common fields](https://code.claude.com/docs/en/hooks#common-fields)). Codex
filters by tool name only. The installer appends the entry after the last
element of `hooks.PreToolUse`, creating the keys or the file when absent, in
the file's own indentation. Existing bytes are kept. `$CODEX_HOME` moves the
personal Codex file
([advanced configuration](https://learn.chatgpt.com/codex/config-file/config-advanced)).

**Cost.** Measured on 2026-10-01 on an AMD Ryzen 7 7800X3D, 16 CPUs, Linux
7.2.2, zsh as `$SHELL`, and bash as `/bin/sh`, with the built binary and an
agent `PreToolUse` payload on stdin. Each case started a fresh process, timed
from spawn to exit:

| Case | Runs | p50 | p95 |
|---|---|---|---|
| Codex, `ls -la`, binary run directly | 300 | 17.8 ms | 18.5 ms |
| Codex, `ls -la`, guarded entry under `zsh -lc` | 300 | 68.2 ms | 70.7 ms |
| Codex, `true` under `zsh -lc`, for comparison | 300 | 49.7 ms | 51.8 ms |
| Codex, refused `git push`, binary run directly | 30 | 39.6 ms | 40.8 ms |
| Codex, refused `git push`, guarded entry under `zsh -lc` | 30 | 90.4 ms | 94.2 ms |
| Claude Code, `git status`, guarded entry under `sh -c` | 200 | 18.8 ms | 19.5 ms |
| `true` under `sh -c`, for comparison | 200 | 0.9 ms | 1.0 ms |

Codex runs a hook command with `$SHELL -lc`
([command_runner.rs](https://github.com/openai/codex/blob/main/codex-rs/hooks/src/engine/command_runner.rs)),
and starts that login shell only because the hook exists. The Caller's added
cost per Codex shell command is therefore the whole guarded-entry time, about
70 ms. Across three runs on this machine its p50 was 68 to 73 ms and its p95
71 to 89 ms. Most of it is the login shell, 50 to 55 ms here, before the
hook's 18 ms. Claude Code runs hook commands with `sh -c`
([command hook fields](https://code.claude.com/docs/en/hooks#command-hook-fields)),
and the `Bash(git *)` rule starts the hook for every git command, so `git
status` and `git log` each wait about 19 ms more. Other shell commands do not
start it.
Of the hook's time, 15 to 16 ms is package initialization in
`github.com/mattn/go-runewidth` v0.0.27, which builds its width table eagerly.
It is linked in through the Configuration Hub's terminal libraries. v0.0.30
fills only the first 0x300 entries at init and builds the rest on first use. Moving to it is a dependency change and is left
for its own decision.

## Slice 5 decisions

**Markers.** The block sits between two lines that hold only
`<!-- review-party checkpoints: begin -->` and
`<!-- review-party checkpoints: end -->`. HTML comments keep the markers out of
rendered Markdown, and a line that holds only the marker, after trimming
whitespace, is the only thing the installer treats as one. A marker quoted
inside other text is prose.

**File choice.** The block goes in `AGENTS.md` at the repository root. A
repository with `CLAUDE.md` and no `AGENTS.md` gets it in `CLAUDE.md`, since
that is the file its agents already read. A repository with both uses
`AGENTS.md`, and one with neither gets a new `AGENTS.md` that holds only the
block. The installer never writes the block into both files.

**Stale rule.** The body is generated from the declared Checkpoints, one line
per Checkpoint in name order and one line pointing at `review-party checkpoint
--help`. A Waiver command is added to a Checkpoint's line only under
`anyone`, as the refusal does. The block is installed when the lines between
the markers equal that body byte for byte. Any other content is stale,
including a hand edit, because the block is generated. Replacing it rewrites
only the lines between the markers, after a terminal confirms or under
`--yes`, and a rerun without either exits 2 and writes nothing. A missing
block is appended after exactly one blank line. Repeated markers, a lone
marker, or an end marker before the begin marker are malformed. The installer
refuses them, names the counts, and leaves the file for the Caller to repair,
because guessing which marker is real could delete the Caller's text. With
`--config`, the installer warns that the block's commands do not carry it.

**Initialization.** `agents-md` is preselected in the team-floor multi-select
when `init` offers a Checkpoint, and `init` writes the block after its own
confirmation. It is never offered as a personal Integration, because the file
is shared. Without a terminal, `init` prints a missing or stale block with the
install command, and malformed markers with the lines to fix by hand. `init`
still treats an edited hook as in place. `doctor` reports it.

**Doctor findings.** Each finding is one line ending in `; fix: <command>`, and
`--format json` carries the same facts in `unresolved_names`,
`integration_gaps`, `exemption_conflicts`, and `recent_waivers`. Every fix
repeats `--config` when the doctor run had it. `init` and `doctor` read
Integration gaps from one planner, which plans each Integration's install and
maps its outcome to a gap, so the two commands cannot disagree about what is
missing. Unresolved names are read even from an invalid configuration,
because they are often why it is invalid. The other findings need a
configuration that validates.

**Exit codes.** Doctor exits 1 only when the configuration is invalid, and
findings exit 0. This follows the AGENTS.md rule "Keep project governance
outside the review engine." The CLI reports facts, and the repository's
instructions and the Caller decide whether a missing hook or a recent Waiver blocks delivery. A
nonzero exit for findings would make that decision for every caller that
checks the status.

**Documentation-Profile signal.** A Profile is a documentation Review when it
was created from the packaged `documentation` Template. An exemption is a
Markdown exemption when its pattern matches `README.md` or `docs/README.md`,
the files every documentation layout has. Doctor reports a Checkpoint whose
Markdown exemptions would let changes a selected documentation Profile
reviews pass without that Review. The fix is the full `config checkpoint set`
command with those patterns removed and every other setting repeated, since a
set replaces the declaration.

**Recent Waivers.** Doctor lists the Waivers recorded in this repository in
the last 30 days, newest first. They are records, not gaps, so their lines
carry no fix. Ledger schema 15 adds a `repository` column to
`checkpoint_waivers`, keyed by the repository root the Waiver was recorded
from, with an index on the repository and the creation time. Doctor never
creates the state directory or the ledger. A missing ledger lists no Waivers,
and a ledger that needs preparation is reported with `review-party init` as
the fix.

**Deviations.**

- The AGENTS.md example above dropped "record a verdict for each Finding".
  Recording verdicts belongs to the `judged` requirement in slice 6.
- "Exemptions that conflict with the selection" is narrowed to Markdown
  exemptions against a documentation Profile. No other exemption has a
  Profile it is known to defeat.
- The documentation-Profile signal is the Template ID. The earlier
  initialization warning also matched any Profile whose name or Template
  contained "doc", which a Profile named `docker` would have met. Both now use
  the Template ID.
- A linked worktree resolves to its own root, so its Waivers are listed there
  and not in the main checkout.

## Slice 6 decisions

**Which Review counts.** For each Profile that Coverage holds for, Coverage is
decided again over only the completed Reviews whose every Finding has a
current verdict. The whole-set and per-commit rules are unchanged. When those
Reviews cover the change, they are the Profile's Reviews and it passes. So any
fully judged covering Review passes, even when a newer covering Review has no
verdicts yet, and a Caller passes by judging the Review they just ran.
Counting only the newest covering Review would make a Caller who reran a
Review judge again content an older, judged Review already examined. When no
judged Reviews cover the change, the Profile keeps the Reviews that supplied
Coverage, and the report lists each of them that has an unjudged Finding.

**Current verdict.** A verdict counts when finding feedback does not mark it
stale. Accepted, rejected, and deferred all count, because the requirement is
that a Caller judged each Finding, not that the Caller agreed with it. Whether
a deferred Finding should block delivery is the repository's decision, under
the AGENTS.md rule "Keep project governance outside the review engine." A
stale verdict judged text the Finding no longer holds, so it does not count.

**Exempt-path Findings.** A Finding's location is free text that the result
contract writes as `path:line`. A location is read as a path only when the
text after its first `:` is a line, made of digits, `-`, and `:`. A Finding
whose path the Checkpoint's exemptions match needs no verdict, since the
Checkpoint never asks for a Review of that file. This holds for a path only
the covering Review changed, which Coverage already leaves out of its
comparison. A location without that shape, one that names more than one file,
or a path the exemptions do not match still needs a verdict. The match errs
toward asking, because a misread location must not let an unjudged Finding
pass. A small change passes before Coverage is checked, so it is never judged.

**Refusal wording.** The refusal reads `pre-push Checkpoint needs a verdict on
Findings 1, 2 of <id> for <base>..<head>; judge: review-party finding record
<id>`. With more than one unjudged Review it adds `; 2 more Reviews need
verdicts`, and under `anyone` it ends with the waive command as before. It
names one Review, the first in selection order, because each Review takes its
own `finding record` command, and two or more full commands would not fit a
short line. The count says more remain, and `checkpoint check` lists every
one. The ordinals are named because `finding record` reads them on standard
input. The git, Claude Code, and Codex hooks share this line, as they do for
`reviewed`. It says what the change still needs, never that it is bad.

**`checkpoint check`.** The Checkpoint state is `unjudged`, and `covered`
stays true, since Coverage holds. Each Profile's state stays `covered`, and
its `unjudged` list names each Review ID with its unjudged ordinals. The text
form appends `; no verdict on Finding 1 of <id>` to the Profile line and
labels the next command `judge:`. The check exits 1, as for any reached
decision that does not pass. A missing Review comes first, then a running one,
then verdicts, because verdicts are read only once Coverage holds.

**Cost.** Verdicts are read only under `judged`, and only for Profiles that
Coverage holds for. Each completed candidate Coverage already returned costs
one ledger record load and one verdict query, at most once per check. The
Codex hook still classifies the command and exits before it loads
configuration or the ledger.

**Declaration.** `config checkpoint set --requirement judged` declares it, and
validation accepts `reviewed` or `judged`. `init` asks for the requirement in
the same form that asks for exemptions and the waiver policy, with `reviewed`
as the default. The doctor fix for a Markdown exemption repeats
`--requirement judged`, so running it keeps the requirement.

**AGENTS.md block.** Under `judged`, a Checkpoint's line reads, for pre-push,
"Before pushing, review the change with `review-party run --base <upstream>
--head HEAD` and record a verdict for each Finding with `review-party finding
record <id>`; the pre-push Checkpoint refuses a push until a completed Review
covers it and every Finding has a verdict." Under `reviewed`, the line is the
slice 5 line byte for byte, so blocks installed before this slice stay
installed. Changing a Checkpoint's requirement makes its block stale.

**Deviations.**

- The Model defines `judged` as Coverage and a verdict on every Finding
  without naming whose Findings. A Profile can pass on an older, judged
  Review than the one that supplies its Coverage, as the which Review rule
  above explains. The Reviews that supply Coverage are the ones named when
  nothing judged covers the change.
- The refusal for unjudged Findings says "needs a verdict on" instead of the
  "has no completed Review of" shape, since a completed Review exists.

## Open questions

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
