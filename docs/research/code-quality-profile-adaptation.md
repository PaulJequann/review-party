# Code quality Profile adaptation

Status: implemented locally; evaluation and live dogfood evidence remain
separate acceptance steps

Date: 2026-08-13

## Source

Review Party is adapting the judgment in Cursor's
[`thermo-nuclear-code-quality-review/SKILL.md`](https://github.com/cursor/plugins/blob/main/cursor-team-kit/skills/thermo-nuclear-code-quality-review/SKILL.md).
The source describes a deliberately strict maintainability review: look for
structural simplification, avoid uncontrolled file growth, detect ad-hoc
branching and misplaced ownership, question indirection and weak type
boundaries, and prefer a small number of high-conviction findings over
cosmetic feedback.

## Review Party adaptation

The source is a reviewer-judgment document, so it maps to a packaged Markdown
Profile rather than a new adapter, transport, capability, or result contract.
The Profile should:

- preserve Review Party's evidence rules and report only material,
  behavior-preserving maintainability regressions or concrete missed structural
  simplifications;
- treat the 1,000-line crossing rule as a strong decomposition signal, not an
  automatic delivery decision;
- focus on structural regressions, needless concepts or branches, spaghetti
  growth, boundary and type-contract erosion, wrong-layer logic, duplicate
  helpers, and avoidable orchestration complexity;
- require exact code evidence, the maintenance or change-risk consequence, and
  the smallest structural remedy for every finding;
- reject style-only, naming-only, speculative, unrelated-debt, and
  preference-based comments; and
- leave approval, blocking, and remediation authority to the caller and
  repository governance. The CLI reports validated findings and does not make
  that decision.

The implemented Profile is named `code-quality`. It remains separate from the
`bugs` Profile: a bug review asks whether behavior or delivery risk is wrong,
while this review asks whether the change materially makes the implementation
harder to understand, extend, or safely modify even when the observed behavior
is correct.

## Eval and dogfood implications

The existing Eval framework executes an explicit Profile through the ordinary
Review path and keeps semantic scoring separate from execution completeness.
That supports a dedicated packaged code-quality suite without an eval-only
execution path. The suite should include both defect cases and adversarial
known-clean cases, with multi-file context where ownership or canonical-layer
reasoning matters. Candidate case families are:

- a structural reframing that removes branches or modes;
- ad-hoc special-case growth in an existing flow;
- feature logic leaking into a shared or canonical module;
- a thin wrapper or duplicate helper that adds indirection;
- a weak optional/cast-heavy contract where an explicit model is available;
- a plausible file decomposition or the 1,000-line crossing heuristic; and
- clean cases where sequential or indirect structure is justified by dependency
  or ownership, so the Profile is not rewarded for flagging every abstraction.

Dogfooding should run the new Profile with the configured OpenCode Muse
selection, persist the ordinary Review Record, and inspect its Profile
Revision, Reviewer/model provenance, result-contract revision, lifecycle, and
termination. An unavailable, malformed, or timed-out run remains Incomplete.

## Architectural inference

This adaptation does not require a new execution seam. The existing packaged
Profile/compiler seam already owns Markdown resolution, Profile Revision
identity, prompt framing, capability restrictions, and source provenance; the
existing eval seam already records the selected Profile and runs ordinary
Reviews. Implementation should therefore be limited to the new Profile,
packaged-profile registration/documentation, the dedicated fixtures, and the
dogfood instructions unless focused evaluation exposes a missing product
capability.
