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
