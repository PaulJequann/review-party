# Live adapter audit — 2026-08-09

## Scope

This audit ran the built `review-party` CLI against the non-empty working
changes for Slice 8. Every run explicitly selected the `documentation` Profile,
one Reviewer, a three-minute deadline, JSON output, and an adapter-specific
Record directory under ignored `scratch/live-records/`. No adapter, model,
transport, Profile, or capability contract was substituted.

The command shape was:

```sh
./scratch/review-party review documentation \
  --repo . \
  --reviewer REVIEWER \
  --deadline 3m \
  --records ./scratch/live-records/REVIEWER \
  --format json
```

## Results

| Reviewer | Review ID | Subject identity | Outcome | Evidence |
| --- | --- | --- | --- | --- |
| Grok | `rp_1786248380417_1998766c93721148` | `6c9f736a4be85222d51cb198c76ba522aecd013373168613f7600faafd0d195e` | Incomplete, `transient_failure` | `grok-4.5` reached the three-minute context deadline. |
| OpenCode | `rp_1786248566666_dd66c0f08a222e5a` | `6c9f736a4be85222d51cb198c76ba522aecd013373168613f7600faafd0d195e` | Incomplete, `unknown_failure` | `zai-coding-plan/glm-5.2` returned `Unexpected server error. Check server logs for details.` |
| Copilot, initial | `rp_1786248576422_bdd854d44739139a` | `6c9f736a4be85222d51cb198c76ba522aecd013373168613f7600faafd0d195e` | Incomplete, `invalid_result` | Separate native assistant messages were concatenated without a boundary, hiding the canonical opening marker from the line-oriented parser. |
| Copilot, corrected | `rp_1786248783120_f62d29bdc3c78a76` | `6eba831e22d0251f343bc99c656fe5bb93159d0b3b3339bf1f7294b1c4bf8670` | Completed, findings | `gpt-5-mini` completed in about 52 seconds after message boundaries were preserved. The Subject identity changed because the decoder and its regression test had joined the working changes. |

The ignored Review Records contain the full frozen patches, raw outputs,
provenance, timestamps, and diagnostics for local inspection. This durable note
retains only the bounded evidence needed to assess adapter readiness.

## Finding disposition

The completed Copilot Documentation Review reported three candidate findings:

- Accepted: README capability wording omitted the explicit mutation, shell, and
  web denials. The wording was corrected.
- Accepted: an undated claim that local `main` was synchronized with
  `origin/main` would become stale. It was replaced with a historical delivery
  statement.
- Rejected: `json.Marshal(ProfileRevision)` was described as a runtime panic
  risk. `ProfileRevision` and its nested concrete types contain only supported
  JSON values and no interface, function, channel, complex number, or custom
  marshaler whose runtime value could make this call fail. A synthetic
  injection hook would test an impossible state rather than a production
  regression.

## Initial readiness conclusion

Copilot has one retained completed live Review for this slice. Grok and OpenCode
remain implemented but lack completed live evidence for the first supported
release; their runs are Incomplete, not clean and not successful.

## Slice 8 dogfood follow-up

Later on 2026-08-09, Slice 8 and its newly required User Configuration Module
were reviewed through the preferred Grok and OpenCode paths. The configuration
selected Grok as the Default Reviewer, kept Copilot secondary on `auto`, and
allowed these Caller-owned OpenCode models:

- `meta/muse-spark-1.2-contributor`
- `opencode-go/deepseek-v4-flash`

| Reviewer/model | Review ID | Lifecycle | Result |
| --- | --- | --- | --- |
| OpenCode, obsolete compiled `zai-coding-plan/glm-5.2` | `rp_1786249817479_fa6d908aa84e417a` | Incomplete | Immediate server error exposed the obsolete provider identifier. |
| OpenCode, provider diagnostic `opencode-go/glm-5.2` | `rp_1786250031209_b94a294779c1278c` | Completed | Proved the corrected provider path and found two stale model references; this was integration diagnosis, not an accepted user-model choice. |
| OpenCode, configured Muse Spark | `rp_1786250781266_45afad25b4602581` | Completed | Found three configuration-consistency defects, all remediated. |
| OpenCode, explicit DeepSeek V4 Flash | `rp_1786250909912_b9bf6902007c86f7` | Incomplete | Reached the selected model but hit the five-minute deadline. |
| Grok `grok-4.5/high` with 30-turn bound | `rp_1786251219415_ac2c069b8fe2bf60` | Completed | Found one effective-default validation defect, remediated. |
| OpenCode, Muse remediation review | `rp_1786251807669_9354b8de2c8409b3` | Completed | Found two additional defects; both were assessed and remediated. |
| OpenCode, Muse verification review | `rp_1786252070000_f481bbb6ffce73df` | Completed | Found one valid lazy-storage defect and one rejected event-schema claim. The valid finding was remediated with a regression test. |

The accepted dogfood findings covered stale model provenance, inconsistent
configuration loading, disabled/default Reviewer validation, missing config
path diagnostics, completed OpenCode text-part boundaries, disabled Reviewer
allowlists, and eager Review Record storage. Each accepted finding has focused
regression evidence. The final OpenCode claim that completed text-part events
were token deltas was rejected against OpenCode's own `run.ts`, which emits JSON
`text` events only for completed parts (`part.time?.end`).

### Follow-up readiness conclusion

Grok and OpenCode Muse both have completed bounded Reviews of Review Party's
own non-empty working changes. OpenCode DeepSeek remains supported by user
policy but lacks completed five-minute evidence for this Subject size. Copilot
remains secondary compatibility coverage and is not the primary verifier.
