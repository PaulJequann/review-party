# Review Party

Review Party coordinates trustworthy external code reviews while leaving delivery authority with its caller and the project being reviewed.

## Language

**Review Party**:
The conductor of a bounded, capability-aware code review whose outcome the caller can safely interpret. It may provide evidence and recommendations, but it does not decide whether changes may ship.
_Avoid_: Agent runner, delivery gate, coding-agent harness

**Review**:
A bounded evaluation of one exact state of code or a plan for one particular purpose. Multiple coding agents may contribute to the same review; evaluating changed code is a separate, related review.
_Avoid_: Agent invocation, review thread, evolving review

**Review Subject**:
The exact change, code, plan, or repository state whose evaluation a review describes. It may range from one file to an entire repository, but it remains fixed for that review.
_Avoid_: Target, mutable branch, current changes

**Review Profile**:
A named, versioned definition of a repeatable kind of review. It captures the purpose and review recipe, but not the Review Trigger, Review Subject, or project delivery policy.
_Avoid_: Prompt template, untracked configuration bundle

**Review Pass**:
One intentionally scoped, independently meaningful reviewer contribution within a review. Retrying the same contribution does not create another independent pass.
_Avoid_: Lane, process launch, review

**Attempt**:
One effort to complete a Review Pass. A failed or interrupted Attempt does not count as independent review coverage.
_Avoid_: Review Pass, retry result

**Attempt Limit**:
The maximum number of Attempts allowed for one Review Pass, including its initial Attempt. Exhausting the limit leaves the Pass incomplete rather than creating an unbounded recovery loop.
_Avoid_: Retry count, Party deadline

**Retry Policy**:
The finite rules that determine whether a failed Attempt is eligible for another Attempt using the same selected Reviewer. The Attempt Limit is a ceiling, not an instruction to repeat deterministic failures.
_Avoid_: Fallback policy, retry until clean

**Execution Deadline**:
The finite time boundary on an Attempt or Party. Reaching it stops the affected work and preserves an honest incomplete outcome.
_Avoid_: Attempt Limit, harness timeout

**Attempt Outcome**:
The normalized result of one observable Agent Harness execution, paired with its raw diagnostic. Review Party distinguishes completion, transient failure, unavailable Reviewer, invalid result, cancellation, and unknown failure without exposing provider-specific errors as domain states.
_Avoid_: Provider response, Review Result, raw exit code

**Recovery Policy**:
The recorded rules that bound retries, fallback, deadlines, and stopping behavior for a Review Pass. Profile and Party Revisions provide ordinary defaults while explicit Caller overrides produce a distinct effective revision.
_Avoid_: Invocation flags, retry forever, hidden defaults

**Concurrency Limit**:
The maximum number of independent ready work items a Party may execute simultaneously. Review Dependencies impose ordering, while Attempts for the same Pass remain sequential.
_Avoid_: Pass count, fallback race

**Fallback Chain**:
The ordered Reviewer candidates a Profile Revision declares acceptable for one Review Pass. Advancing within the chain is observable recovery, while using an undeclared candidate is a Substitution.
_Avoid_: Retry Policy, hidden global order, peer reviewers

**Availability Check**:
A pre-execution determination that a Reviewer candidate is reachable and satisfies required capabilities. A rejected candidate is recorded but consumes no Attempt because its Agent Harness never executed.
_Avoid_: Attempt, Review Result, guaranteed authentication

**Fallback**:
The outcome-driven advance from one Reviewer candidate to the next candidate in a declared Fallback Chain. It is neither a retry of the same Reviewer nor an independent peer contribution.
_Avoid_: Retry, Substitution, Review Pass

**Reviewer Preference**:
A Caller choice that reorders the candidates already permitted by a Profile Revision while preserving its declared Fallback Chain.
_Avoid_: Reviewer Requirement, Substitution

**Reviewer Requirement**:
A Caller constraint that narrows which Reviewer candidates are acceptable. Review Party cannot fall back outside that constraint without obtaining authority for a Substitution.
_Avoid_: Reviewer Preference, preferred Reviewer

**Review Result**:
The caller-facing outcome of a review, including its scoped conclusion, findings, actual contributions, and completeness. Diagnostic logs may support it, but are not the result itself.
_Avoid_: Review Package, transcript, universal verdict

**Review Trigger**:
The event that asks Review Party to begin a review, such as a direct command, project event, or schedule. It is independent of the Review Subject and Review Profile.
_Avoid_: Mode, Review Subject, Review Profile

**Profile Revision**:
One exact version of a Review Profile and its effective review recipe. Every effective change, including an experimental override, produces a distinct revision retained by the review that used it.
_Avoid_: Profile name, mutable configuration

**Review Record**:
The retrievable history of one review, containing its Review Result, provenance, Passes, Attempts, and available diagnostics. It supports inspection without making diagnostic detail the primary result.
_Avoid_: Review Result, console output, transcript

**Incomplete Review**:
A review that ended without fulfilling its Profile Revision. Evidence from completed Passes remains available, but the review cannot claim that no actionable findings were found.
_Avoid_: Clean review, partial success

**Substitution**:
The use of an agent, model, transport, profile, or capability contract outside the choices declared by the effective Profile Revision. A Substitution requires caller authorization; selecting among declared alternatives does not.
_Avoid_: Declared alternative, automatic fallback

**Review Lifecycle**:
A review is Pending once its Subject and Profile Revision are fixed, Running while its promised work remains, and then Completed or Incomplete. Adapter phases and termination reasons do not create additional Review states.
_Avoid_: Adapter lifecycle, success or failure

**Finding**:
An evidence-bearing claim contributed by a reviewer about the Review Subject. Its presence guarantees provenance and valid review structure, not factual acceptance, unless the Profile Revision explicitly requires independent verification.
_Avoid_: Fact, universal defect, raw reviewer prose

**Verification Review**:
A new Review over a changed Subject, scoped by an earlier Review Result and the relevant change to determine whether earlier Findings remain. It is related to, but never a continuation of, the earlier Review.
_Avoid_: Remediation Review, continuing Review, remediation pass

**Caller**:
The person, agent, or automation that asks Review Party to conduct a review and consumes its Review Record. The Caller identifies what it wants reviewed and supplies required authority, while retaining responsibility for remediation and delivery decisions.
_Avoid_: User, reviewer, delivery gate

**Reviewer**:
The configured coding agent and model responsible for contributing a Review Pass. Its harness and transport are recorded separately because they can materially affect its behavior.
_Avoid_: Model, transport, adapter

**Agent Harness**:
The runtime that owns a Reviewer's model and tool loop, including context assembly and tool execution. Review Party coordinates it but does not become it.
_Avoid_: Reviewer, transport, Review Party

**Transport**:
The means by which Review Party communicates with an Agent Harness. It affects execution provenance without defining the review purpose or Reviewer.
_Avoid_: Reviewer, Review Profile

**Review Pipeline**:
An ordered or conditional composition of immutable Reviews. Passes may provide independent contributions within one Review, while different Profiles or changed Subjects remain related Reviews in the Pipeline.
_Avoid_: Evolving Review, merged Review Record

**Assessment**:
An evidence-backed, non-binding classification or recommendation included when required by a Profile Revision. The Caller and project policy determine what action follows it.
_Avoid_: Delivery decision, universal verdict

**Project Rule**:
A constraint explicitly declared applicable to a Review by its Caller or execution environment. Its authority comes from that declaration, not from the type, name, or location of the document containing it.
_Avoid_: Project Context, repository documentation

**Project Context**:
Attributable project material that may inform a Review without being presumed current or correct. A material conflict with an ADR, plan, document, or implementation is evidence to surface, not an automatically authoritative verdict.
_Avoid_: Project Rule, source of truth

**Review Guarantee**:
A promise Review Party can establish about orchestration, such as the exact Subject and Profile Revision, actual Reviewer provenance, bounded execution, valid result structure, and honest completeness. It does not promise exhaustive or factually infallible reviewer judgment.
_Avoid_: Correct verdict, exhaustive review, deterministic Finding

**Reviewer Judgment**:
A Reviewer's non-deterministic discovery and interpretation of the Review Subject, Project Rules, and Project Context. A Profile guides this judgment but cannot guarantee that every relevant concern will be identified.
_Avoid_: Review Guarantee, deterministic rule evaluation

**Context Discovery**:
The lean search for project material relevant to a Review. Reviewers begin with applicable `AGENTS.md` instructions and domain context files, consult repository orientation when useful, and pursue other documents only as the Subject or emerging evidence warrants.
_Avoid_: Documentation audit, reading every document

**Documentation Review**:
A Review whose Profile evaluates documentation accuracy, omissions, consistency, and relevant project-language concerns. It may evaluate a documentation-only Subject, a code change, or an entire repository state.
_Avoid_: Documentation mode, Review Pipeline

**Review Bundle**:
The aggregate output of a Review Pipeline, preserving each constituent Review Record, its provenance, and its completeness. Its completion is independent of the Findings contributed by completed Reviews.
_Avoid_: Review Result, merged Findings, log archive

**Party**:
A Caller-selected composition of Review Profiles applied to one Review Subject. A Party may be reusable or ad hoc, and its effective composition is fixed before it becomes a Review Pipeline.
_Avoid_: Review, Review Pipeline, mandatory multi-review workflow

**Party Revision**:
One exact version of a reusable Party, including its selected Profile Revisions and declared dependencies. A Party name may evolve while each Review Bundle retains the effective composition it used.
_Avoid_: Party name, mutable profile list

**Review Dependency**:
An explicit relationship that supplies selected upstream Review Results as attributable context to a downstream Review. Reviews remain independent by default, and every consumed result is retained in the downstream Review Record.
_Avoid_: Shared hidden context, automatic result forwarding

**Incomplete Bundle**:
A Review Bundle in which at least one required Review did not fulfill its Profile Revision. Completed Results and Findings remain available and never disguise the missing coverage.
_Avoid_: Failed bundle, clean bundle, discarded partial evidence

**Synthesis Review**:
An explicitly requested downstream Review that evaluates the shared Subject using selected upstream Review Results as context. It contributes reconciliation or an Assessment without replacing or rewriting the original Results.
_Avoid_: Finding merge, pipeline summary, last reviewer wins
