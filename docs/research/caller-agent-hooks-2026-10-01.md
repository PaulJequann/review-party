# Caller Agent hooks and Git hook managers

Date: 2026-10-01. Question: which Caller Agents can refuse a commit or push
before it runs, and how a Checkpoint Integration can join an existing Git hook
setup without overwriting it. Facts below are sourced; the closing section is
inference.

## Caller Agents

| Agent | Event | Configuration | Refusal | Matches command text |
|---|---|---|---|---|
| Claude Code 2.1.285 | `PreToolUse`, matcher `Bash` | `.claude/settings.json` (committed), `.claude/settings.local.json` (local), `~/.claude/settings.json` | exit 2 with stderr, or `permissionDecision: "deny"` with a reason | Yes, through `if: "Bash(git push*)"`, checked per subcommand |
| Codex CLI 0.159.2 | `PreToolUse` | `.codex/hooks.json` or `.codex/config.toml` (project), `~/.codex/...` (user) | exit 2 with stderr, or `permissionDecision: "deny"` | No; the matcher is a regex on the tool name |
| opencode 1.18.33 | plugin `tool.execute.before` | `.opencode/plugins/` or `~/.config/opencode/plugins/` (JS/TS) | throw an `Error` | Plugin code reads `output.args.command` |
| Cursor | `beforeShellExecution` | `.cursor/hooks.json`, `~/.cursor/hooks.json` | exit 2, or `permission: "deny"` | Not documented; unconfirmed for `cursor-agent` |
| Gemini CLI | `BeforeTool`, matcher `run_shell_command` | `.gemini/settings.json`, `~/.gemini/settings.json` | exit 2, or `decision: "deny"` | Not documented |

Sources: <https://code.claude.com/docs/en/hooks>,
<https://learn.chatgpt.com/docs/hooks>, <https://opencode.ai/docs/plugins/>,
<https://cursor.com/docs/agent/hooks>, <https://geminicli.com/docs/hooks/>.

Codex loads project hooks only when the project `.codex/` layer is trusted, and
new or changed hooks need approval through `/hooks`; trust is recorded as
`trusted_hash` entries in `~/.codex/config.toml`. An installer cannot activate
a Codex project hook by itself. The Codex documentation also says some tool
paths can opt out of hooks and calls hooks "a useful guardrail, not a complete
enforcement boundary".

The opencode error path was read in source (`packages/opencode/src/session/tools.ts`,
`processor.ts`, `message-v2.ts` at sst/opencode `aa481b8`) but not run end to
end. That a thrown error reaches the model as tool error text is unconfirmed.

Caller Agent hooks fire on the tool call, so `git push --no-verify` does not
bypass them. Git's own `pre-commit` and `pre-push` hooks are skipped by
`--no-verify` (githooks(5)).

## Git hook managers

| Manager | Detection | Adding a pre-push step without overwriting |
|---|---|---|
| lefthook | `lefthook.{yml,yaml,toml,json,jsonc}`, dot-prefixed, or under `.config/` ([docs](https://lefthook.dev/configuration/)) | `pre-push.commands.<name>.run` in the main config, or `lefthook-local.*` for a local override |
| husky v9 | `.husky/` and `core.hooksPath` = `.husky/_` | Append to `.husky/pre-push` |
| pre-commit | `.pre-commit-config.yaml` | `repo: local` hook with `stages: [pre-push]`, then `pre-commit install --hook-type pre-push`; refuses while `core.hooksPath` is set |
| `core.hooksPath` | `git config --get core.hooksPath` | Edit `<hooksPath>/pre-push`; Git ignores `.git/hooks` while it is set |
| none | `git rev-parse --git-path hooks` | Join an existing `pre-push` instead of replacing it |

Linked worktrees share the common `.git/hooks` directory: `git rev-parse
--git-path hooks` from a linked worktree returned the main checkout's hooks
directory.

## Inference

Codex and opencode run the hook on every shell call, so the Integration's
command must classify the call and exit before loading configuration or the
ledger for anything that is not a commit or push. Only Claude Code filters in
configuration.
