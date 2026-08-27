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
A named, versioned, executable definition of a repeatable kind of review. It packages the review purpose and judgment instructions with the selected Reviewer, model, reasoning effort, and Execution Deadline. It does not contain the Review Trigger, Review Subject, Party membership, or project delivery policy.
_Avoid_: Review Profile Template, prompt alone, temporary model override, untracked configuration bundle

**Review Profile Template**:
A packaged, non-executable starting point for Review Profile judgment instructions. A Caller creates a Review Profile by copying or authoring instructions and selecting the complete execution configuration. Template updates never mutate saved Profiles automatically.
_Avoid_: Review Profile, executable built-in, inherited prompt fragment

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
The recorded rules that bound retries, fallback, deadlines, and stopping behavior for a Review Pass. Ordinary saved Profiles fix their Execution Deadline; Eval experiments may test different finite recovery choices without mutating those Profiles.
_Avoid_: Invocation flags, retry forever, hidden defaults

**Concurrency Limit**:
The maximum number of independent ready work items a Party or Eval Suite Run may execute simultaneously. A value of one means sequential execution; higher values allow that many work items to run in parallel. Review Dependencies impose ordering, while Attempts for the same Pass remain sequential. The Caller may configure the limit, and the default is sequential execution.
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

**Eval Case**:
One versioned, known Review problem used to evaluate Reviewer Judgment. It fixes the code state supplied as a Review Subject and declares human-confirmed expected Findings or known-clean evidence, but it does not select a Reviewer, model, or effort.
_Avoid_: Model configuration, Review, generated reviewer answer

**Eval Suite**:
A versioned collection of Eval Cases that share one evaluation purpose. A packaged global suite provides a general baseline, while Caller-owned global or project suites may extend it without changing packaged content.
_Avoid_: Review Profile, Experiment Configuration, hard-coded product policy

**Canary Eval Suite**:
A small Eval Suite of intentionally obvious cases used to prove evaluation plumbing, isolation, result-contract compatibility, adjudication, and scoring. Its results are not evidence of broad Reviewer capability.
_Avoid_: General benchmark, model leaderboard, representative recall

**Experiment Configuration**:
The explicit Review Profile, Reviewer, model, effort, Execution Deadline, Retry Policy, Concurrency Limit, and other permitted execution choices tested against an Eval Suite. It may be named in version-controlled material or supplied by the Caller, and its effective choices are frozen into the resulting records.
The effective Retry Policy and Concurrency Limit are persisted with the other Experiment choices. Retries remain Attempts within one ordinary Review and one Eval Run; bounded concurrency never changes manifest identity.
_Avoid_: Eval Case, expected Finding, implicit default Reviewer

**Eval Run**:
The durable execution record for one Eval Case under one Experiment Configuration. It is created before execution, retains Pending, Running, and terminal execution state, and freezes the Eval Case Revision. It links the ordinary Review after Review creation; Pending, Running, and no-Review Incomplete runs have no link, remain not ready for adjudication, and render `review not started` in human inspection. Execution facts remain separate from later adjudication or scoring.
_Avoid_: Eval Case, Eval Suite Run, EvalReview

**Eval Suite Run**:
The durable parent record for one execution of an Eval Suite under one Experiment Configuration. It retains the Suite revision, ordered Eval Runs, explicit Pending, Running, Completed, or Incomplete lifecycle, optional hard-stop termination, completion categories, and timing without overwriting earlier runs or claiming a quality score. Completed means the full manifest was attempted and may include Incomplete cases; Incomplete means cancellation or a hard stop left planned work unattempted. The effective numeric Concurrency Limit bounds active Reviewer execution while preserving manifest order.
_Avoid_: Review Bundle, score, comparison

**Adjudication Revision**:
One immutable, human-published mapping between the expected Findings and reported Findings of an Eval Suite Run. It preserves matched, missed, novel-valid, false-positive, uncertain, and unscored-incomplete decisions with their evidence; a correction creates another revision.
_Avoid_: Review Result rewrite, automated judge output, mutable score

**Eval Score**:
The deterministic metrics calculated from one Adjudication Revision. It reports defect recall, Finding precision, clean-case behavior, completion, and termination facts with explicit numerators and denominators; uncertain and Incomplete executions remain visible rather than being guessed.
_Avoid_: Reviewer verdict, delivery decision, terminal-output summary

**Eval Comparison**:
A deterministic side-by-side report over two immutable Adjudication Revisions. It compares exact shared Eval Case revisions, lists omitted and mismatched cases, preserves each experiment's Profile, Reviewer/model/effort, transport, harness, and build provenance, and reports quality, completion, termination, and runtime deltas without declaring a winner.
_Avoid_: Universal score, leaderboard, promotion gate, delivery decision

**Synthetic Review Subject**:
The isolated Review Subject materialized for an Eval Run. It contains the code state and bounded change needed by the Reviewer, but excludes source Git history, public commit identities, fixing commits, and expected Findings.
_Avoid_: Mutable fixture checkout, corpus authority view, generated repository

**Review Record**:
The retrievable history of one review, containing its Review Result, provenance, Passes, Attempts, and available diagnostics. It supports inspection without making diagnostic detail the primary result.
_Avoid_: Review Result, console output, transcript

**Review Record State Preparation**:
The deterministic local operation that makes a Review Record ledger usable by applying known migrations and establishing its managed state. It is distinct from observing an already prepared Review Record and does not preserve retired record formats.
_Avoid_: Legacy import, exposed records-directory setup, implicit inspect setup

**Review Party Initialization**:
The first-use operation that prepares Review Party for one repository, including its managed Review Record state. It does not create a custom Review Profile.
_Avoid_: Profile initialization, first Review

**Configuration Hub**:
The interactive control center for inspecting and changing Review Party configuration over time. It serves recurring customization as Reviewers, models, Profiles, and preferences change; first-use guidance is one journey through the Hub rather than its defining purpose.
_Avoid_: Setup wizard, onboarding screen, web UI

**Global Configuration**:
Caller-owned configuration available to every repository, including reusable Review Profiles and Parties, evaluation defaults, and managed-state choices. Global availability does not cause a Profile or Party to run; each Repository Configuration selects its own defaults.
_Avoid_: Automatically enabled baseline, organization configuration, repository configuration

**Repository Configuration**:
Review Party configuration associated with one repository. It defines repository-owned Profiles and Parties and selects that repository's ordered default Reviews from Global and Repository Configuration.
_Avoid_: Global Configuration, project delivery policy, Party inheritance

**Configuration Scope**:
The ownership context of a saved value or definition: Global Configuration or Repository Configuration. Effective values retain their scope so the Caller can distinguish reusable choices from repository-owned choices.
_Avoid_: Config directory, hidden precedence, environment

**Model Discovery**:
The best-effort collection of model choices reported by an available Agent Harness for one Reviewer. Discovered choices make selection searchable and provider-correct, but do not replace manual model entry when discovery is unavailable or incomplete.
_Avoid_: Packaged model catalog, model allowlist, guaranteed availability

**Effective Configuration**:
The complete configuration Review Party will use for one operation after resolving the selected Global and Repository definitions and any explicit Profile or Party choice. It preserves the authored selection, expanded ordered Reviews, deduplication, execution settings, and provenance. Templates are not Effective Configuration because they cannot run.
_Avoid_: Configuration file, merged JSON, implicit defaults

Agents inspect Effective Configuration with `review-party config show` and
inspect one authored document with `review-party config file show`. They use
the remaining `review-party config` commands to submit typed changes that the
Configuration Manager validates and publishes only after confirmation.

**Profile Creation**:
The operation that saves one complete executable Review Profile from a Review Profile Template or blank instructions plus a Reviewer, model, reasoning effort, and Execution Deadline. Agents invoke it with `review-party config profile create`. It is separate from Review Party Initialization.
_Avoid_: Profile init, template selection alone, unnamed profile material

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
A named, reusable, ordered group of Review Profile references with one Concurrency Limit. A Party does not contain other Parties or alter a Profile's Reviewer, model, reasoning effort, deadline, or instructions.
_Avoid_: Review, Review Pipeline, Profile variant, inherited Party

**Party Revision**:
One exact version of a Party, including its Configuration Scope, Concurrency Limit, ordered scoped Profile references, and selected Profile Revisions. Each Review Bundle retains the Party Revision it used even when a referenced Global Profile later changes.
_Avoid_: Party name, mutable profile list, nested Party graph

**Review Dependency**:
An explicit relationship that supplies selected upstream Review Results as attributable context to a downstream Review. Reviews remain independent by default, and every consumed result is retained in the downstream Review Record.
_Avoid_: Shared hidden context, automatic result forwarding

**Incomplete Bundle**:
A Review Bundle in which at least one required Review did not fulfill its Profile Revision. Completed Results and Findings remain available and never disguise the missing coverage.
_Avoid_: Failed bundle, clean bundle, discarded partial evidence

**Synthesis Review**:
An explicitly requested downstream Review that evaluates the shared Subject using selected upstream Review Results as context. It contributes reconciliation or an Assessment without replacing or rewriting the original Results.
_Avoid_: Finding merge, pipeline summary, last reviewer wins
