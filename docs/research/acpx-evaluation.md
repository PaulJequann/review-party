# ACPX evaluation for Review Party

Status: research and prototype evidence, not an accepted architecture decision

Date: 2026-08-08

## Question

Should Review Party restore ACPX as a transport, replace it with direct agent
adapters, or reproduce selected ACPX features itself?

## Recommendation

Keep ACPX as an **optional one-shot ACP transport**, but do not make it the
default path for Grok or OpenCode today and do not make it a required runtime
dependency for Review Party V1.

- Keep direct Grok. Its native CLI exposes the review-specific controls Review
  Party already uses: explicit tool visibility, no web search, no subagents, no
  memory, no plan mode, reasoning effort, structured streaming output, and a
  turn limit. Grok also exposes `grok agent stdio` as an ACP agent, so ACPX is a
  viable second transport rather than the only route. See the official
  [Grok Build overview](https://docs.x.ai/build/overview) and
  [CLI reference](https://docs.x.ai/build/cli/reference).
- Keep direct OpenCode while its native permission configuration and JSON event
  stream satisfy the profile. OpenCode has a real ACP server, but ACPX's current
  built-in OpenCode launch is `npx -y opencode-ai acp`; Review Party should use
  a pinned/installed command override if it experiments with this path rather
  than permit an implicit install. See OpenCode's
  [ACP documentation](https://opencode.ai/docs/acp/),
  [permission documentation](https://opencode.ai/docs/permissions/), and
  [ACPX agent registry](https://github.com/openclaw/acpx/blob/main/src/agent-registry.ts).
- Add ACPX when a named profile needs an ACP-native agent or ACP behavior that a
  direct CLI cannot provide cleanly. Gemini is a reasonable first comparison,
  not a foregone conclusion: Gemini also has native headless, policy, sandbox,
  structured-output, and ACP modes.
- Do not implement a native Go ACP client yet. If ACPX becomes a demonstrated
  operational problem, use the community
  [`coder/acp-go-sdk`](https://github.com/coder/acp-go-sdk) rather than writing
  JSON-RPC or generated protocol types by hand. The SDK is listed by the ACP
  project as a [community library](https://agentclientprotocol.com/libraries/community),
  not an official Go SDK.

This preserves the original reason for ACPX without coupling Review Party's
domain model to it: ACPX is replaceable transport and compatibility plumbing,
not the review engine.

## Evidence

### ACP is more than launching a subprocess

ACP v1 is bidirectional JSON-RPC. A client initializes and negotiates protocol
versions and capabilities, optionally authenticates, creates or loads a
session, prompts it, consumes streamed updates, answers client-side filesystem,
terminal, and permission requests, cancels work, and interprets the terminal
stop reason. The client owns environment management, user interaction, and
resource access control. See the official
[ACP overview](https://agentclientprotocol.com/protocol/v1/overview) and
[initialization contract](https://agentclientprotocol.com/protocol/v1/initialization).

Capabilities are deliberately optional. Omitted capabilities must be treated
as unsupported, and clients and agents must negotiate an agreed protocol
version before session creation. The baseline agent surface is small
(`session/new`, `session/prompt`, `session/cancel`, and `session/update`), while
session loading, models/configuration, filesystem callbacks, terminals, and
richer prompts vary by agent. This makes an ACP host a compatibility component,
not merely a JSON decoder.

### What ACPX one-shot provides

ACPX describes itself as a headless ACP client and explicitly labels its CLI
and runtime interfaces pre-1.0. Its `exec` command performs a stateless run. In
that narrow mode it provides:

| Concern | Evidence | Value to Review Party |
|---|---|---|
| Agent launch registry and raw-command escape hatch | [Registry source](https://github.com/openclaw/acpx/blob/main/src/agent-registry.ts), [custom-agent and built-in overview](https://github.com/openclaw/acpx#choose-an-agent) | High for experiments and ACP-only agents; launch commands still need pinning and validation. |
| ACP startup, version/capability negotiation, auth selection, session creation, model configuration, and compatibility quirks | [Client source](https://github.com/openclaw/acpx/blob/main/src/acp/client.ts) | High; this is the main maintenance ACPX saves. |
| Permission brokering | [Permission guide](https://github.com/openclaw/acpx/blob/main/docs/permissions.md), [permission source](https://github.com/openclaw/acpx/blob/main/src/permissions.ts) | Useful for uniform approve/deny/escalate decisions when an agent actually represents the action through ACP. |
| No-terminal capability advertisement | [Permission guide](https://github.com/openclaw/acpx/blob/main/docs/permissions.md#--no-terminal) | Useful fail-closed signal to conforming agents. It is not an OS sandbox. |
| Raw machine-readable ACP stream | [Output formats](https://github.com/openclaw/acpx/blob/main/docs/output-formats.md) | Useful diagnostic input, but Review Party must still normalize agent extensions and validate its own result contract. |
| Classified process outcomes | [Exit codes](https://github.com/openclaw/acpx/blob/main/docs/exit-codes.md) | Useful input to Review Party's incomplete-outcome taxonomy. |
| Cooperative cancellation and child cleanup | [Sessions guide](https://github.com/openclaw/acpx/blob/main/docs/sessions.md#cancelling), [client source](https://github.com/openclaw/acpx/blob/main/src/acp/client.ts) | Useful, but Review Party must retain its own hard wall-clock watchdog and process-tree cleanup. |

ACPX therefore does absorb protocol negotiation, permission brokerage, process
lifecycle, authentication edge cases, model/capability variance, and several
agent launch quirks. That part of the earlier rationale remains valid.

ACPX does **not** give Review Party one canonical cross-agent event schema. Its
JSON mode deliberately emits raw ACP JSON-RPC with no ACPX envelope and no key
renaming. Agent-specific `_meta` events and payloads remain visible. Review
Party still owns event accumulation, requested-versus-resolved provenance,
diagnostic redaction, completion interpretation, and validation of Review
Results.

Permission mediation also has an important boundary: it governs permission
requests that reach the ACP client. It is not proof that a native tool was
removed from the model's view, and it is not filesystem or network isolation.
Tool availability may still depend on an agent-specific launch flag or `_meta`
extension. Review Party should claim only the capability it has verified for
that exact agent/transport/version tuple.

### Features that are mostly irrelevant to the current product

ACPX also owns broader orchestration:

- persisted and named sessions, resume/load, history, export/import, and crash
  recovery ([sessions guide](https://github.com/openclaw/acpx/blob/main/docs/sessions.md));
- queue ownership and ordered follow-up prompts over local IPC;
- `compare`, which runs one prompt across multiple agents
  ([compare guide](https://github.com/openclaw/acpx/blob/main/docs/compare.md)); and
- durable TypeScript flow graphs with actions, decisions, checkpoints, retries,
  persistence, and replay ([flows guide](https://github.com/openclaw/acpx/blob/main/docs/flows.md)).

Those features are valuable in ACPX, but they should not define Review Party's
architecture:

- A static review Attempt is intentionally one-shot. Persisting an agent chat
  would blur the accepted relationship between Review, Attempt, Pass, and
  later fix verification.
- A Party is a Review Party concept with named Profiles, independent Passes,
  shared immutable Subject identity, and Review Party outcome rules. Delegating
  it to `acpx compare` would lose those semantics.
- Retry budgets, fallback authorization, Review records, and future review
  pipelines are product behavior. Delegating them to ACPX flows would make a
  generic TypeScript workflow engine an accidental second Conductor.

Persistent ACP sessions may become useful later for an explicitly designed
remediation-continuity profile. That is a separate product decision, not a
reason to use persisted sessions for ordinary reviews.

## Local prototype

Environment observed on 2026-08-08:

- ACPX installed: `0.12.0`; npm latest: `0.13.0`.
- Grok installed: `0.2.111`; its native ACP command is `grok agent stdio`.
- OpenCode installed: `1.18.15`; its native ACP command is `opencode acp`.
- Gemini installed: `0.43.0`; its native ACP flag is `--acp`.

The installed-to-latest ACPX mismatch and ACPX's own pre-1.0 warning support a
version preflight and fixture compatibility test before launch. They do not by
themselves justify replacing ACPX.

### Grok direct versus ACPX-to-Grok

I ran one deliberately small read-only probe against the same repository,
prompt, and requested `grok-4.5` model:

```text
Read go.mod. Reply with exactly the module path and nothing else.
```

| Path | Wall time | Reported total tokens | Reported cost | Outcome |
|---|---:|---:|---:|---|
| direct Grok CLI | 4.64 s | 33,244 | $0.0376256 | exact answer |
| ACPX one-shot to `grok agent stdio` | 4.61 s | 40,262 | $0.0451656 | exact answer |

This is a functional probe, not a statistically meaningful performance
benchmark. It shows that ACPX startup/handshake overhead was not material in
this sample. It does **not** show inference parity: the ACP run exposed a
different session/context surface and consumed about 21% more reported tokens.
Repeated fixed-snapshot measurements would be required before attributing that
difference to transport.

A second ACPX probe forced a repository search. ACPX/Grok emitted structured
`tool_call` and `tool_call_update` messages for a read-only `grep`, returned the
exact value, reported the selected model and high effort, and completed with
`stopReason: end_turn`. The initialization also showed `terminal: false` and
the requested allowed-tool list passed through agent-specific session metadata.

A fuller review-shaped pair used the same clean Go fixture, prompt, CWD,
`grok-4.5` model, high effort, and requested `view,grep,glob` tool set:

| Path | Wall time | Reported usage | Reported cost | Outcome |
|---|---:|---:|---:|---|
| direct Grok CLI | 11.75 s | 17,189 input; 33,792 cached input; 541 output | $0.0477616 | `NO_FINDINGS` |
| ACPX one-shot to `grok agent stdio` | 12.06 s | 61,568 input; 37,120 cached input; 563 output | $0.06341 | `NO_FINDINGS` |

The 0.31-second difference is too small and the sample too narrow to establish
a latency tax. The capability and context difference is more actionable. The
direct command exposed only its requested read/search tools and disabled web,
subagents, memory, and plan mode. The ACP run's raw initialization and custom
metadata advertised a large ambient catalog of native tools, skills, MCP
servers, and commands despite ACPX receiving `--allowed-tools view,grep,glob`
and `--no-terminal`. The allowed-tool list was transmitted in agent-specific
session metadata, but the emitted catalog shows that transmission is not proof
that the model-visible surface was actually reduced. A Profile must verify the
resolved capability, not merely record the requested flag.

The raw ACP stream was much noisier than Review Party's direct Grok stream. It
included initialization, authentication, settings, commands, skills, MCP, tool,
usage, and custom xAI notifications. It also included authentication/account
metadata. `--suppress-reads` suppresses read payloads, not all potentially
sensitive metadata. Review Party must therefore retain bounded capture and an
explicit redaction/retention policy if it stores raw ACP artifacts.

### OpenCode comparison exposed a capability mismatch, then hit a live failure

The controlled direct OpenCode probe requested
`zai-coding-plan/glm-5.2` twice and reached the provider path, but both attempts
returned an `UnknownError` service failure before inference. The installed
OpenCode ACP server initialized successfully through ACPX, but advertised its
model configuration as `unknown/unknown` with an empty option list. ACPX then
failed closed when asked to pin `zai-coding-plan/glm-5.2`: it could not prove
that the ACP agent supported the requested model. Omitting the model allowed
session creation, after which the same OpenCode service failure occurred.

This is not a performance comparison. It is direct evidence that the tested ACP
path cannot currently reproduce the Profile's requested model contract, while
ACPX correctly reports the unsupported pin instead of silently substituting a
default. The ACP session also emitted the machine's broad ambient command/skill
catalog even with terminal capability disabled. Direct OpenCode remains the
better candidate when its provider is healthy because its native config can
express the deny-by-default tool contract; both paths still need availability
preflight and a completed-run compatibility test.

## What Review Party should own

These are product semantics and should be implemented once above every
adapter:

- Subject resolution, immutability, identity, and materiality limits.
- Profile compilation into one or more bounded Pass plans.
- Requested runtime and capability contract, plus resolved agent/model/effort
  provenance.
- Hard wall-clock deadline, process-tree cleanup, output limits, and incomplete
  outcomes even when a transport has cooperative cancellation.
- A transport-neutral event/Attempt record and validated Review Result.
- Retry accounting, fallback authorization, Party concurrency, and pipeline
  state.
- Bounded raw-artifact retention with secret, account-metadata, and repository
  payload redaction.
- Version/capability preflight and recorded compatibility evidence for each
  adapter tuple.

Review Party should adopt the useful *semantics* of ACPX's permission and error
models without copying its implementation: distinguish unavailable capability,
authentication, transient failure, timeout, permission denial/escalation,
protocol incompatibility, malformed result, and cancellation.

## What Review Party should not recreate now

- JSON-RPC framing and generated ACP types.
- General ACP initialization/auth/session/config-option machinery.
- A broad registry of third-party agent launch quirks.
- Persistent conversation storage, prompt queues, session export/import, and
  crash recovery.
- Generic compare or flow engines.

If Review Party later owns native ACP, the minimum host should use
`coder/acp-go-sdk` and implement only a one-session review path: typed client
callbacks, fail-closed read/search permission policy, no terminal capability,
event accumulation, cancellation, subprocess supervision, and per-agent
compatibility tests. The SDK examples demonstrate
`NewClientSideConnection`, `Initialize`, `NewSession`, and `Prompt`; Review
Party would still own launch, policy, lifecycle, and normalization.

## Decision triggers

### Add or retain an ACPX adapter when

1. A desired reviewer has a maintained ACP surface but lacks an equally
   controllable native batch interface.
2. ACP capability/model negotiation or permission events provide required
   evidence absent from the direct interface.
3. An ACPX-to-agent compatibility test proves the Profile's exact read/search,
   no-terminal, bounded-turn, model, and completion contract.
4. The operational environment accepts pinned Node 22+ and pinned ACPX/agent
   versions.

### Prefer a direct adapter when

1. Native flags can remove disallowed tools or context sources from the model's
   view more precisely than ACP permissions.
2. Native structured output exposes richer or cleaner stop, usage, schema, or
   provenance data.
3. ACP omits required controls or depends on undocumented agent `_meta`
   extensions.
4. ACPX would implicitly download an adapter/harness or materially complicate
   deployment.

### Replace ACPX with native Go ACP only when measured evidence shows

1. ACPX compatibility churn repeatedly breaks supported Review Party profiles.
2. Node/ACPX packaging is unacceptable for a supported deployment target.
3. Raw ACP access through ACPX is too lossy or too difficult to redact and
   normalize.
4. Repeated controlled measurements show meaningful latency/resource overhead
   in the actual review workload.
5. Review Party needs a small, stable ACP capability that ACPX cannot expose
   without importing irrelevant session/flow behavior.

## Proposed next experiment

Do not replace the working direct adapters first. Add a prototype-only ACPX
transport behind the existing Attempt executor boundary and run a compatibility
matrix, one agent at a time:

1. Grok direct versus ACPX-to-Grok with the same immutable Subject, full bugs
   Profile prompt, model/effort, tool set, and three repetitions.
2. Repair or re-authorize the OpenCode model/provider configuration, then compare
   direct `opencode run --format json` with ACPX invoking the **installed**
   `opencode acp`, not the registry's unpinned `npx -y` command.
3. Compare direct Gemini headless mode with ACPX-to-Gemini only if Gemini is
   selected for a named Profile.

Record launch-to-first-event, wall time, actual model/effort, prompt/context
tokens, cost, visible and attempted tools, permission requests/denials, stop
reason, output bytes, raw-metadata redaction, timeout cleanup, and canonical
Review Result equivalence. Promote ACPX only for the agent/Profile combinations
where it produces a concrete advantage.

## Final judgment

The original ACPX rationale was valid, but it supports a narrower conclusion
than “use ACPX for every agent.” ACPX is useful maintained infrastructure for
speaking ACP and handling agent variance. Review Party should keep that option
behind a transport adapter. Direct Grok and direct OpenCode remain preferable
where their native automation and capability controls are stronger and easier
to audit. Recreate Review Party's own execution, evidence, and review semantics;
do not recreate ACPX's protocol client, session manager, queue, compare command,
or flow engine until a measured product requirement demands it.
