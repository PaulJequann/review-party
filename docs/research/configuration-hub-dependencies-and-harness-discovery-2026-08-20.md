# Configuration Hub dependencies and harness discovery

Date: 2026-08-20
Status: Slice 1 implementation evidence

## Question

Which compatible terminal-library versions should Review Party adopt, and what
observational model-discovery and explicit authentication interfaces do its four
current Agent Harnesses expose?

## Terminal stack

The selected versions build together under the repository's Go 1.26 toolchain:

| Module | Version | Role |
| --- | --- | --- |
| `github.com/spf13/cobra` | `v1.10.2` | command tree, flags, help, completion |
| `charm.land/bubbletea/v2` | `v2.0.9` | persistent Hub state and event loop |
| `charm.land/huh/v2` | `v2.0.3` | focused forms |
| `charm.land/bubbles/v2` | `v2.2.0` | reusable controls |
| `charm.land/lipgloss/v2` | `v2.0.6` | terminal styling |

Huh v2 declares `charm.land/huh/v2`; the former GitHub import path is invalid
for this release. Huh's `Form` retains a compatibility model interface over
Bubble Tea v2, so a parent Bubble Tea model can forward messages into a form and
render its view without starting a nested program. The production Hub should
own the only Bubble Tea program and embed forms this way.

A throwaway spike under `scratch/config-hub-spike/` combined all five modules.
It built successfully and was exercised through a pseudo-terminal at 48 columns:
Hub-to-form transition, Huh selection/input, resize rendering, Ctrl-C
cancellation, alternate-screen restoration, and a normal completed form all
returned control to the invoking terminal. Scratch evidence is session-local
and is not a durable product implementation.

Primary sources:

- [Cobra package documentation](https://pkg.go.dev/github.com/spf13/cobra)
- [Bubble Tea repository and v2 tutorial](https://github.com/charmbracelet/bubbletea)
- [Huh v2 package documentation](https://pkg.go.dev/charm.land/huh/v2)

## Harness discovery matrix

Local observations used installed executables and did not launch login flows or
modify harness configuration.

| Reviewer | Observed version | Model Discovery | Authentication status | Explicit sign-in |
| --- | --- | --- | --- | --- |
| Grok | `1.0.4` | `grok models`; human text | `grok models` distinguishes logged-out failure from success, but no dedicated structured status was found | `grok login`; supports `--device-auth` and `--oauth` |
| OpenCode | `1.18.21` | `opencode models [provider]`; one canonical `provider/model` ID per line; optional `--verbose` and explicit mutating/network refresh via `--refresh` | `opencode providers list`; human text | `opencode providers login [url]` |
| Copilot | `1.0.80` | No supported machine model-list command found. Current local help no longer includes model choices. Interactive `/model` is not an observational integration interface | No dedicated read-only status command found. Environment-token presence is not proof of validity | `copilot login`; supports `--device-code` and `--web-flow` |
| Codex | `0.148.0` | `codex app-server` JSONL protocol: initialize, then paginated `model/list`; structured IDs, display names, effort options, modalities, and default facts | `codex login status`, or structured app-server `account/read` | `codex login`; app-server also exposes explicit account login requests |

### Grok

The official CLI reference documents both `grok models` and `grok login`.
Locally, `grok models` reported successful Grok authentication, default
`grok-4.6`, and available IDs `grok-4.6` and `grok-4.5`. The command has no JSON
flag in version 1.0.4, so its adapter must use bounded parsing and preserve an
`unsupported` result if future output cannot be recognized. Custom models mean
manual entry remains necessary even when discovery succeeds.

Sources:

- [xAI Grok Build CLI reference](https://docs.x.ai/build/cli/reference)
- [xAI Grok Build overview and custom models](https://docs.x.ai/build/overview)

### OpenCode

The official CLI documentation defines `opencode models` as the list of models
from configured providers and specifies canonical `provider/model` output.
`--refresh` refreshes a Models.dev cache, so passive Hub discovery must omit it;
refresh should be an explicit user action. `opencode providers list` reports
configured credential providers but currently emits decorated human output.
The alias shown in current local help is `providers`, while official prose also
uses `auth`; Review Party should invoke the executable's current documented
canonical command rather than assume the alias forever.

Source:

- [OpenCode CLI: models and authentication](https://opencode.ai/docs/cli/)

### GitHub Copilot

GitHub documents `--model` and says model strings are available in `copilot
help`, but local 1.0.80 help exposes only an unconstrained `<model>` value.
GitHub's open CLI issue #700 confirms the lack of a stable programmatic
model-list interface and distinguishes models supported by the client from
models enabled for an account. Interactive `/model` would take terminal control
and can persist a selection, so Review Party must not scrape or drive it during
observational discovery.

Initial status: model discovery is `unsupported`. The Hub should offer configured
and packaged choices plus manual entry. If a later Copilot release adds a
structured command, the adapter can become supported without changing the Hub
interface. `copilot login` is a documented explicit action; Review Party must
not launch it during discovery.

Sources:

- [GitHub Copilot CLI programmatic reference](https://docs.github.com/en/copilot/reference/copilot-cli-reference/cli-programmatic-reference)
- [GitHub Copilot CLI authentication](https://docs.github.com/en/copilot/how-tos/copilot-cli/set-up-copilot-cli/authenticate-copilot-cli)
- [GitHub Copilot CLI issue #700](https://github.com/github/copilot-cli/issues/700)

### Codex

Codex has no simple `codex models` command, but its first-party app-server is the
supported rich-client interface. After protocol initialization, `model/list`
returns paginated structured records with canonical model ID, display name,
default state, supported reasoning efforts, input modalities, and other
capabilities. `account/read` reports authentication state. The simple CLI also
provides observational `codex login status` and explicit `codex login` browser,
device-code, or stdin credential flows.

The discovery adapter should use one short-lived app-server process, complete
the required initialization handshake, page through `model/list`, then stop and
reap the process. It must not call account-login methods during discovery.

Sources:

- [OpenAI Codex app-server documentation](https://developers.openai.com/codex/app-server)
- [OpenAI Codex app-server source reference](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)
- [OpenAI Codex repository authentication overview](https://github.com/openai/codex)

## Accepted adapter contract for Slice 5

Discovery returns one of:

- `supported`: bounded model results are available;
- `authentication_required`: the harness explicitly reports missing or invalid
  authentication;
- `unavailable`: the executable cannot be found or started;
- `unsupported`: the installed harness has no safe observational interface or
  its version/output is not recognized.

Every execution uses argv, inherited allowlisted authentication environment,
context cancellation, a finite deadline, bounded stdout/stderr, and process-tree
cleanup. Results carry harness version and observation time. One Reviewer's
failure cannot block another Reviewer or the Hub.

Recommended initial limits for the production design are a 10-second passive
discovery deadline, a bounded 4 MiB combined capture, and no automatic network
refresh flag. These are architectural starting points, not harness guarantees;
Slice 5 should refine them with focused fixtures and measured output sizes.

## Decision

Proceed with the selected v2 Charm stack and Cobra versions. Implement direct
observational Model Discovery for Grok and OpenCode, structured app-server
discovery for Codex, and an explicit `unsupported` Copilot adapter until GitHub
ships a stable machine interface. Manual model entry remains available for all
Reviewers. Authentication is always a separate user-confirmed action.
