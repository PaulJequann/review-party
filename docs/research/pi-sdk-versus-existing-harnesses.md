# Pi SDK versus established coding-agent harnesses

Status: research, not an accepted architecture decision
Date: 2026-08-08

## Question

Should Review Party:

1. orchestrate established coding-agent harnesses through direct CLI and ACPX
   adapters;
2. embed Pi with the Pi SDK and thereby own a Review Party-specific harness; or
3. defer harness ownership until a concrete requirement cannot be met through
   adapters?

## Conclusion

Use option 3 as the governing decision and option 1 as the initial
implementation: make Review Party a review orchestrator, not an agent harness.
Keep direct-CLI adapters where a harness exposes useful native controls, use
ACPX for ACP agents, and treat Pi as another established harness that can be
reached through `pi --mode rpc`, Pi's JSON event mode, or ACPX's `pi` adapter.

Do not embed the Pi SDK yet. The SDK is capable and would save Review Party from
implementing an agent loop from primitives, but embedding it would still make
Review Party responsible for configuring and operating Pi's model runtime,
credentials, tools, context loading, retries, compaction, sessions, events, and
Node.js lifecycle. Those responsibilities are not needed to compile review
profiles, dispatch bounded review runs, and normalize findings.

This recommendation is an architectural inference from the evidence below, not
a claim that Pi is unsuitable. Pi is a strong candidate if a later requirement
specifically calls for one deeply controlled embedded harness.

## Terms and ownership boundary

- A **review orchestrator** resolves a review subject, compiles a profile into
  lanes, selects adapters, constrains execution, and validates a common Review
  Party result.
- An **agent harness** owns the model/tool loop: model and credential access,
  context assembly, tool definitions and execution, turn/retry behavior,
  compaction, and session state.
- A **transport/client** starts or connects to a harness and carries prompts,
  events, permission decisions, cancellation, and session operations.

The distinction matters because Pi SDK and ACPX live on opposite sides of this
boundary. Pi SDK embeds an agent harness. ACPX is an ACP client and operational
transport around harnesses that already exist.

## Evidence: what the Pi SDK actually provides

The following are sourced facts from Pi's official documentation and source
repository.

### It embeds Pi's complete agent capabilities

Pi describes its SDK as programmatic access to Pi's agent capabilities for
embedding Pi, custom interfaces, automated workflows, sub-agent tools, and
programmatic tests. `createAgentSession()` creates an `AgentSession`; that
session owns agent lifecycle, message history, model state, compaction, event
streaming, abort, and cleanup. `AgentSessionRuntime` additionally owns replacing
the active session for new, resume, fork, clone, and import flows.

Sources:

- [Pi SDK: overview and core concepts](https://pi.dev/docs/latest/sdk#core-concepts)
- [Pi SDK: session management](https://pi.dev/docs/latest/sdk#session-management)

### It owns model/provider and credential integration

`ModelRuntime` discovers built-in and custom models, lists authenticated
models, switches models and thinking levels, and resolves credentials from
runtime overrides, Pi's auth store, environment variables, and custom-provider
fallbacks. Pi supports built-in provider catalogs plus API-key and several
subscription/OAuth flows. SDK applications must also set deadline policy for
remote provider-catalog refreshes; without an abort signal those operations are
unbounded.

Sources:

- [Pi SDK: model selection and authentication](https://pi.dev/docs/latest/sdk#model)
- [Pi provider and credential documentation](https://pi.dev/docs/latest/providers)

### It owns prompt and context assembly

The default resource loader discovers project/global extensions, skills,
prompt templates, context files such as `AGENTS.md`, settings, custom models,
credentials, and sessions. A custom `ResourceLoader` can replace discovery and
override the system prompt. Prompt calls also expand Pi prompt templates and
support queued steering and follow-up behavior.

Sources:

- [Pi SDK: directories and resource discovery](https://pi.dev/docs/latest/sdk#directories)
- [Pi SDK: system prompt](https://pi.dev/docs/latest/sdk#system-prompt)
- [Pi SDK: prompting and message queueing](https://pi.dev/docs/latest/sdk#prompting-and-message-queueing)

### It owns tool availability and execution

The SDK exposes Pi's built-in `read`, `bash`, `edit`, `write`, `grep`, `find`,
and `ls` tools, supports an allowlist and exclusions, and allows custom typed
tools. A read-only Pi session can expose only `read`, `grep`, `find`, and `ls`.
This is true tool-availability control: excluded tools need not be visible to
the model.

Sources:

- [Pi SDK: tools](https://pi.dev/docs/latest/sdk#tools)
- [Pi SDK: custom tools](https://pi.dev/docs/latest/sdk#custom-tools)

### It owns event, retry, compaction, and session mechanics

`AgentSession` streams message, thinking, tool-execution, turn, queue,
compaction, and retry events. Its session manager supports in-memory and
persistent sessions, continuation, branching, tree navigation, and context
reconstruction. Settings include retry and compaction controls.

Sources:

- [Pi SDK: events](https://pi.dev/docs/latest/sdk#events)
- [Pi SDK: session management](https://pi.dev/docs/latest/sdk#session-management)
- [Pi session format](https://pi.dev/docs/latest/session-format)

### It does not provide a security boundary

Pi explicitly states that project trust controls resource loading, not tool
execution. Pi has no built-in sandbox; built-in tools and extensions run with
the Pi process's user permissions. Its documentation recommends an OS,
container, VM, micro-VM, or policy-controlled sandbox for real isolation.
Allowlisting only read-oriented Pi tools reduces the model's available action
surface, but it does not turn the Node.js process or loaded extensions into a
security boundary.

Sources:

- [Pi security: no built-in sandbox](https://pi.dev/docs/latest/security#no-built-in-sandbox)
- [Pi containerization patterns](https://pi.dev/docs/latest/containerization)

### The SDK is not required to automate Pi

Pi provides `--mode json` for a JSONL event stream and `--mode rpc` for a
bidirectional headless JSON protocol. Pi recommends the SDK for same-process
TypeScript type safety, direct agent state, and programmatic tool/extension
customization; it recommends RPC for another language, process isolation, or a
language-agnostic client.

Sources:

- [Pi JSON event stream mode](https://pi.dev/docs/latest/json)
- [Pi RPC mode](https://pi.dev/docs/latest/rpc)
- [Pi SDK: RPC alternative and selection guidance](https://pi.dev/docs/latest/sdk#rpc-mode-alternative)

**Inference:** Review Party is planned as Go software, so direct SDK embedding
would add a Node.js/TypeScript process or distribution component. Pi RPC is the
better first Pi adapter if Review Party needs Pi-specific behavior that ACP does
not expose. It retains Pi's harness while preserving a Go process boundary.

## Evidence: what established harnesses already provide

Review Party can inherit mature harness behavior without pretending the
harnesses have identical capabilities.

### GitHub Copilot CLI

Copilot's programmatic mode supports a one-shot prompt, pinned model, custom
agent, no-user-question mode, session transcript export, allowed directories,
URL controls, and both allow/deny and availability filtering for tools. Its
tool controls distinguish read, write, shell, URL, memory, and MCP tools and can
filter particular shell commands or MCP operations.

Sources:

- [GitHub Copilot CLI programmatic reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-programmatic-reference)
- [GitHub Copilot CLI tool availability and permissions](https://docs.github.com/en/copilot/how-tos/copilot-cli/use-copilot-cli/allowing-tools)

### OpenCode

OpenCode's non-interactive `run` command supports named agents, provider/model,
session continuation/forking, raw JSON events, a working directory, and a
server attachment path that avoids repeated server startup. OpenCode agents can
bind custom prompts, models, step limits, and granular tool permissions; its
documented `explore` subagent is read-only. OpenCode also provides a headless
OpenAPI server and a generated TypeScript SDK. The SDK can request validated
JSON-schema output with retries.

Sources:

- [OpenCode CLI: `run` and `serve`](https://opencode.ai/docs/cli/#run)
- [OpenCode agents and permissions](https://opencode.ai/docs/agents/)
- [OpenCode server](https://opencode.ai/docs/server/)
- [OpenCode SDK structured output](https://opencode.ai/docs/sdk/#structured-output)

**Inference:** these native surfaces are not implementation noise to erase.
They are capabilities Review Party should selectively exploit behind adapters.
For example, direct Copilot can apply its native tool-availability filters, and
an OpenCode adapter can use native JSON-schema output when it maps cleanly to
Review Party's result contract. A common adapter interface should describe
capabilities and effective constraints instead of forcing every harness through
the lowest common denominator.

## Evidence: what ACP and ACPX provide

### ACP is a client-agent protocol, not an agent loop

ACP v1 is JSON-RPC. A client and agent negotiate protocol versions and
capabilities, create or load sessions, send prompt turns, stream session
updates, handle permission and filesystem/terminal requests, and cancel work.
The agent processes prompts, interacts with the model, executes its agent-side
tools, and reports tool calls. The client manages user interaction and resource
access.

Sources:

- [ACP v1 overview and message flow](https://agentclientprotocol.com/protocol/v1/overview)
- [ACP v1 initialization and capability negotiation](https://agentclientprotocol.com/protocol/v1/initialization)
- [ACP v1 session setup](https://agentclientprotocol.com/protocol/v1/session-setup)
- [ACP v1 prompt turns](https://agentclientprotocol.com/protocol/v1/prompt-turn)
- [ACP v1 tool calls](https://agentclientprotocol.com/protocol/v1/tool-calls)

### ACPX supplies operational client behavior around ACP

ACPX describes itself as a headless ACP client. It supplies built-in launch
profiles for many ACP adapters, process startup, protocol initialization,
one-shot and persistent sessions, local session records, prompt serialization
and queue ownership, cancellation and timeouts, permission policy, stable exit
codes, and text/quiet/raw-ACP-NDJSON output. Its documented data path is:
`CLI -> AcpClient -> NDJSON/stdio -> ACP adapter -> coding agent`.

Sources:

- [ACPX README](https://github.com/openclaw/acpx/blob/main/README.md)
- [ACPX architecture](https://github.com/openclaw/acpx/blob/main/docs/2026-02-17-architecture.md)
- [ACPX permissions](https://github.com/openclaw/acpx/blob/main/docs/permissions.md)
- [ACPX output formats](https://github.com/openclaw/acpx/blob/main/docs/output-formats.md)
- [ACPX built-in agent registry](https://github.com/openclaw/acpx/blob/main/docs/agents.md)
- [ACPX exit codes](https://github.com/openclaw/acpx/blob/main/docs/exit-codes.md)

ACPX explicitly says it is pre-1.0 and its CLI/runtime interfaces are evolving.
ACP documentation currently labels v1 latest and v2 draft.

Sources:

- [ACPX README stability note](https://github.com/openclaw/acpx/blob/main/README.md#install)
- [ACP protocol versions](https://agentclientprotocol.com/protocol/v1/overview)

### ACPX permissions are mediation, not a universal sandbox

ACPX can approve, deny, or escalate ACP permission requests, decline terminal
capability advertisement, and constrain the ACP client filesystem/terminal
methods it implements to `--cwd`. ACP itself says agents execute requested tool
calls and may use client permission or filesystem capabilities. Therefore an
ACPX policy constrains behavior represented through ACP; it does not establish
that every child-agent code path, extension, inherited credential, or
agent-native tool is OS-isolated.

Sources:

- [ACPX permissions and `--cwd`](https://github.com/openclaw/acpx/blob/main/docs/permissions.md)
- [ACP v1 tool-call ownership](https://agentclientprotocol.com/protocol/v1/tool-calls)

**Inference:** Review Party should record three distinct properties for every
lane:

1. tools visible to the model;
2. tool requests permitted by the harness/client; and
3. the actual OS/filesystem/network isolation boundary.

Calling a lane `read-only` should require an adapter-specific availability or
permission contract plus verification of the filesystem boundary appropriate
to the threat model. ACPX `--deny-all` alone is not evidence of OS isolation.

## Comparison for Review Party

| Concern | Existing harness + direct adapter | Existing harness + ACPX | Embedded Pi SDK |
| --- | --- | --- | --- |
| Review profiles | Review Party chooses harness/model/prompt and maps native controls | Same, constrained to advertised ACP and ACPX controls | Review Party can configure Pi directly but becomes coupled to one harness |
| Read-only tool isolation | Best access to harness-native tool visibility and permissions; still needs OS isolation for hostile inputs | Cross-agent permission mediation and optional no-terminal; adapter compliance and agent-side tools vary | Exact Pi tool allowlist; Pi explicitly has no sandbox |
| Model/provider access | Reuses each harness's supported models, auth, subscriptions, and routing | Reuses upstream harness auth/model behavior when exposed by adapter | Review Party operates Pi's model catalog, credentials, OAuth/API-key behavior, and refresh policy |
| Telemetry | Parse native structured events where available; shapes vary | Raw ACP event stream, permission statistics, stable process exit categories | Rich typed Pi lifecycle, tool, retry, compaction, and message events |
| Structured review result | Review Party validates its own result contract; exploit native schema mode when available | ACP structures activity, not a Review Party finding schema; Review Party still validates results | Review Party still defines and validates the finding schema unless it adds a custom output tool |
| Sessions | Use native stateless/session commands only when a profile needs them | ACPX supplies one-shot plus persisted/queued ACP sessions | Review Party configures Pi in-memory/persistent/tree sessions and compaction |
| Prompt/context control | Native custom-agent and instruction controls vary | ACP baseline plus adapter-specific extensions; least uniform | Deep system prompt and `ResourceLoader` control, including disabling/replacing discovery |
| Deployment | Go binary plus explicitly installed external harnesses | Adds Node-based ACPX and ACP adapters to external harnesses | Adds and versions Node.js plus Pi packages/runtime alongside the Go CLI |
| Protocol churn | Each direct adapter tracks a deliberately selected native CLI surface | ACPX absorbs much ACP/adapter churn, but ACPX itself is pre-1.0 | No ACP churn for Pi lane; Review Party tracks Pi SDK changes and still needs other adapters for other harnesses |

## Recommended initial architecture

1. Keep review profiles declarative and harness-neutral: intent, subject,
   prompt fragments, lane budget, required capabilities, result schema, and
   declared fallback policy.
2. Compile profiles into lanes whose adapter selection is explicit. Never
   silently replace an unavailable agent, model, transport, or isolation
   contract.
3. Give adapters a capability report, including:
   - model selection and reasoning controls;
   - tool-availability filtering;
   - permission mediation;
   - working-directory enforcement;
   - OS isolation level;
   - native structured-output support;
   - event/usage telemetry;
   - stateless and session support.
4. Start with direct adapters for harnesses whose native surface provides a
   meaningful advantage and an ACPX adapter for ACP-only or sufficiently
   compatible lanes.
5. Add Pi first as a harness adapter, not an embedded runtime. Prefer Pi RPC
   from Go when Review Party needs Pi-specific events or configuration; prefer
   ACPX when the ACP capability set is sufficient.
6. Normalize all runs into a Review Party-owned execution record and validated
   finding package. Preserve the raw native/ACP event artifact for diagnosis.
7. Enforce hard wall-clock cancellation and child-process cleanup in Review
   Party even when the underlying adapter also supports cooperative abort.

## Concrete triggers for owning a harness later

Revisit Pi SDK embedding or a Review Party-owned harness only when at least one
of these is demonstrated by a real review profile and cannot be provided
reliably through a direct CLI, Pi RPC, or ACPX adapter:

1. **Review-specific tools must participate inside the agent loop.** Examples:
   a typed finding-emission tool, a semantic-diff query tool, or evidence
   citation validation that must constrain each turn rather than validate the
   final response.
2. **Exact context construction is a product requirement.** Automatic harness
   discovery of instructions, skills, extensions, or repository context causes
   measured review inconsistency that cannot be disabled through the native
   adapter.
3. **Per-turn scheduling is required.** A profile needs deterministic steering,
   branching, compaction, retry policy, or dynamic model switching within one
   lane rather than independent one-shot reviewers.
4. **Native telemetry is insufficient.** Cost, token, tool, retry, or evidence
   events needed for budgets and auditability are unavailable or lossy through
   all practical transports.
5. **Cold-start or transport overhead is material.** Repeated, controlled
   benchmarks show adapter/ACPX startup dominates useful review time or cost,
   and a long-lived embedded Pi runtime removes that bottleneck.
6. **A stable provider/model combination is unavailable in established
   harnesses.** Review Party needs provider routing or model features that Pi's
   model runtime exposes and other selected harnesses cannot provide.
7. **Operational deployment favors one embedded runtime.** The supported
   environment already carries Node.js, the team accepts Pi as a core runtime
   dependency, and managing several authenticated external CLIs has become a
   measured reliability burden.

Even when a trigger fires, prefer embedding Pi over writing a model/tool loop
from scratch unless the requirement is specifically incompatible with Pi's
runtime. Pi SDK already owns the difficult harness mechanics.

## Experiments before reconsidering

Capture comparable runs for direct OpenCode, ACPX-to-OpenCode, Pi RPC, and
ACPX-to-Pi using the same immutable review subject, profile prompt, model where
possible, tool/isolation contract, and result schema. Record:

- launch-to-first-event and total wall time;
- model/provider/reasoning setting actually selected;
- tool names visible and tool calls attempted/allowed/denied;
- final stop reason and incomplete-run signals;
- input/output/cache tokens and cost when the harness reports them;
- raw event bytes and whether evidence locations survive normalization;
- schema-validation attempts and failures;
- child-process state after timeout or cancellation.

The experiment should answer a concrete adapter question. It should not become
a benchmark used to choose one universal harness: profiles may legitimately
select different harnesses for bug, security, test, or architecture reviews.

## Decision summary

**Evidence:** Pi SDK provides a complete, deeply customizable Pi harness;
ACPX provides a reusable ACP client/transport layer; established harnesses
already provide differentiated model, tool, auth, session, and automation
capabilities.

**Inference:** Review Party's differentiator is profile compilation,
multi-harness routing, bounded execution, and validated findings. Owning a
harness now would expand the product boundary without resolving a demonstrated
gap. Defer it. Preserve the option by keeping transports behind capability-aware
adapters and by treating Pi RPC as the low-cost bridge to Pi-specific behavior.
