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

Run the built-in bug review with an explicitly selected direct adapter:

```sh
review-party review bugs --reviewer grok
review-party review bugs --reviewer opencode
review-party review bugs --reviewer copilot
```

Grok is the default reviewer. Review Party does not silently fall back between
adapters: an unavailable selected reviewer produces an inspectable incomplete
Review. Grok uses `grok-4.5`; OpenCode uses `zai-coding-plan/glm-5.2`; Copilot keeps
its native `auto` selection and records the model it resolves.

Review Party currently invokes all three harnesses directly. ACPX remains a
future transport option rather than part of the current execution path.

## Review Profiles

Review Party ships a zero-configuration `bugs` Profile and can load ordinary
Markdown Profiles from a repository or a user-wide library. Initialize a
repository library and create another Profile by adding a Markdown file:

```sh
review-party init
$EDITOR .reviewparty/profiles/security.md
review-party profiles
review-party profile explain security
review-party review security
```

Repository and global libraries use the same shape:

```text
.reviewparty/
├── config.json
└── profiles/
    ├── bugs.md
    └── security.md
```

The global library is `~/.reviewparty/`. Set `REVIEW_PARTY_HOME` to relocate
it, or run `review-party init --global` to create it. Configuration is optional
and only selects defaults:

```json
{
  "schema": 1,
  "defaultProfile": "security",
  "defaultReviewer": "grok"
}
```

Selection precedence is explicit caller choice, repository config, global
config, then packaged defaults. A repository Markdown file shadows a global or
packaged file with the same name as one complete definition; Review Party does
not concatenate or inherit prompt text. An invalid higher-precedence file stops
before an Agent Harness launches rather than silently selecting another
Profile.

Profile Markdown controls Reviewer Judgment. Review Party still owns tool and
capability restrictions, Context Discovery, immutable Review Subject framing,
the canonical Review Result contract, deadlines, and incomplete-result
semantics. Profiles cannot configure executables, transports, or shell commands.
