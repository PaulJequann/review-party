# Review Party

Review Party is an experimental code-review CLI for compiling named review
profiles into bounded runs across different coding agents and transports.

The accepted domain language lives in [`CONTEXT.md`](CONTEXT.md), and the
confirmed product structure is captured in
[`docs/product-model.md`](docs/product-model.md). Primary-source investigations
live in [`docs/research`](docs/research/).

The accepted first implementation slice is documented in
[`docs/design/conductor-v1.md`](docs/design/conductor-v1.md).

## Current CLI

List and explain the built-in Review Profiles without launching an Agent
Harness or creating a Review Record:

```sh
review-party profiles
review-party explain bugs
review-party explain documentation --reviewer copilot
review-party config path
review-party config show
```

Run a bug or Documentation Review with an explicitly selected direct adapter:

```sh
review-party review bugs --reviewer grok
review-party review documentation --reviewer opencode \
  --model opencode-go/deepseek-v4-flash
```

`bugs` remains the default Profile, and Grok is the default Reviewer. Both
Profiles require the same repository read/search capability contract with
explicit repository-mutation, shell, and web denials, but compile distinct
purposes, materiality thresholds, Passes, prompts, and Profile Revisions.
Review Party rejects an incompatible Reviewer before launch and does not
silently fall back. An unavailable compatible Reviewer produces an inspectable
Incomplete Review. Grok's built-in model is `grok-4.5`. OpenCode requires a
model supplied by user configuration or explicit `--model`; the product does
not compile a personal OpenCode model preference into its catalog. Copilot's
built-in `auto` selection records the model it resolves.

## User configuration

Review Party loads user policy from
`${XDG_CONFIG_HOME:-$HOME/.config}/review-party/config.json`. The versioned JSON
document may set one Default Reviewer and enable, disable, select, or restrict
models for each supported Reviewer:

```json
{
  "version": 1,
  "default_reviewer": "grok",
  "reviewers": {
    "grok": {"enabled": true, "model": "grok-4.5"},
    "opencode": {
      "enabled": true,
      "model": "meta/muse-spark-1.2-contributor",
      "allowed_models": [
        "meta/muse-spark-1.2-contributor",
        "opencode-go/deepseek-v4-flash"
      ]
    },
    "copilot": {"enabled": true, "model": "auto"}
  }
}
```

An explicit `--reviewer` never bypasses `enabled: false`, and an explicit
`--model` must belong to `allowed_models` when that list is configured. Unknown
fields, unsupported versions, unknown Reviewers, disabled defaults, and
disallowed models fail before Review Subject resolution or Agent Harness
launch. A missing file preserves the built-in zero-configuration behavior.

Review Party currently invokes all three harnesses directly. ACPX remains a
future transport option rather than part of the current execution path.
