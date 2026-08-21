# Evaluation execution v1

Review Party evaluations are Caller-controlled experiments over ordinary
Reviews. The product supplies a framework and immutable packaged suites;
Caller-owned global and project suites can describe different concerns without
changing product policy.

## Domain relationships

An Eval Case fixes one known problem and its human-confirmed expected Findings
or known-clean evidence. It never selects a Reviewer, model, or effort. The
Experiment Configuration selects the Review Profile, Reviewer, model, effort,
Execution Deadline, Retry Policy, and Concurrency Limit tested against a suite.
All effective choices are frozen in the Suite Run.

```text
Eval Suite x Experiment Configuration = Eval Suite Run
                                           |
                                           +-- Eval Run -- ordinary Review Record
```

Every suite rerun creates new records. Full-suite preflight materializes every
Eval Run in manifest order before execution: each begins `pending`, becomes
`running` immediately before its ordinary Review, and finishes as
`completed_clean`, `completed_findings`, or `incomplete`. A terminal Eval Run
becomes `awaiting_adjudication` only when it links an ordinary Review; planned,
active, and no-Review Incomplete runs remain `not_ready`.

The Eval Suite Run has its own explicit lifecycle. It is `pending` once its
ordered children are durably created, `running` while bounded Reviewer
executions are active, `completed` after every case reaches a terminal execution state,
and `incomplete` when cancellation or a hard execution or persistence error
stops remaining work. A completed suite can contain Incomplete Eval Runs; this
means the suite attempted its full manifest, not that every Review succeeded.
Cancellation or a hard persistence failure before the first successful
Running checkpoint moves a suite directly from Pending to Incomplete.
Each child transition and the corresponding parent progress are committed in
one SQLite transaction. The pre-checkpoint Pending-to-Incomplete stop is the
intentional exception: `TerminateEvalSuiteRun` commits only the parent and
leaves every planned child Pending. Scoring and comparison remain later
concerns.

JSON inspection includes `updated_at` on every Eval Run and `lifecycle` plus an
optional `termination {category,message}` on the Eval Suite Run. Before an
ordinary Review Record exists, `review_id` is empty in JSON and human Eval Run
inspection renders `review not started`, including an Incomplete case that
failed before Review creation. Adjudication export requires every planned Eval
Run to have an ordinary Review and explicitly rejects any Pending, Running, or
no-Review Incomplete run.

## Suite sources

`global:canary-bugs` selects elementary cases for fast plumbing and
result-contract checks. `global:general-bugs` selects the broader bug benchmark
embedded in the installed Review Party version. `global:code-quality` selects
the packaged maintainability benchmark and requires the `code-quality` Profile;
the engine rejects other Profile selections before launching any cases.
`global:seeded-bugs` selects reviewed controlled mutations pinned to a source
commit and exact changed-file allowlist. A
filesystem path selects a Caller-owned suite whose
relative case and fixture paths are anchored at `suite.json`. Unknown fields,
unsupported versions, duplicate case IDs, contradictory classifications,
missing fixtures, Git metadata, and symlinks fail full-suite preflight before
any Agent Harness launch.

A suite manifest lists case files explicitly. Each case uses `change`, `state`,
or `seeded`
mode, identifies `base` and `head` fixture directories as applicable, and
declares either expected Findings or known-clean evidence. Expected Findings
use semantic behavior and impact; paths are supporting evidence rather than
exact-match scoring keys.

A seeded case declares a stable seed ID, full deterministic source commit,
version-controlled patch, and exact expected changed paths. Full preflight
materializes a temporary committed source and detached Git worktree, checks the
commit and patch, rejects path drift, and strips Git metadata before ordinary
Subject resolution. Seed generation is never delegated to a live model.

## Retry and concurrency

The default Retry Policy permits three total Attempts with finite exponential
backoff and jitter; one Attempt disables retry. Temporary transport,
availability, deadline, and malformed-result outcomes are retryable.
Authentication, invalid configuration or capability, cancellation, corpus, and
ledger failures are terminal. Every Attempt remains attached to the same
ordinary Review and Eval Run, and only its final terminal Review Result is
eligible for adjudication.

The default Concurrency Limit is one. A positive value N permits at most N
active Reviewer executions for that Suite Run, without automatic sizing or a
process-wide scheduler. Retry backoff does not occupy Reviewer capacity.
Ordered Eval Run identity remains manifest-based even when Reviews finish in a
different order.

## Reviewer isolation

The corpus authority view may contain expected Findings, verification evidence,
and private source provenance. The Reviewer view is a Synthetic Review Subject:

- a temporary copy of the buggy head tree with no `.git` entry or symlink;
- a normalized captured patch and synthetic content identity;
- no source path, commit ID, fixing commit, or expected Finding;
- the ordinary restricted Reviewer capability contract, including web and shell
  denial.

The ordinary Review Record persists only the synthetic identity, patch, changed
paths, Profile Revision, Attempts, artifacts, and Review Result. The Eval Run
separately freezes the complete Eval Case Revision. Local ledger access is not a
secret-storage boundary, and Review Party cannot prove that a model never saw
public code during training.

## Corpus quality roles

The Canary Eval Suite uses one-file, locally obvious defects and one clean
control. It verifies that the correct code reaches the selected Reviewer, the
Reviewer can satisfy the result contract, and execution/adjudication facts stay
separate. A high canary score is a prerequisite signal, not a capability claim.

The general suite uses multi-file repositories and requires surrounding
contracts, callers, persistence behavior, or lifecycle ownership to validate a
Finding. It includes suspicious-looking known-clean changes so blindly echoing
the Profile's risk taxonomy lowers clean-case performance. General cases may
still be synthetic and anonymous; realistic means the relevant reasoning path
resembles an ordinary repository Review, not that fixture line count alone is
large.

The Review Profile is deliberately not weakened for benchmark use. An
Experiment evaluates the actual Profile, Reviewer, model, and effort
configuration. Prompt-ablation experiments use separate Profile Revisions over
the same suite rather than tailoring cases to hide production instructions.

Semantic quality is calculated only from validated Review Results. A malformed
result remains Incomplete and contributes to completion and termination facts,
even when its raw assistant artifact contains a plausible candidate. V1 does
not salvage malformed prose into a Finding or treat contract compliance as a
semantic miss.

## Packaged and private content

The packaged canary contains elementary authentication, concurrency, database
atomicity, resource-cleanup, and clean-control fixtures. The packaged general
suite contains anonymous multi-file persistence, authorization, tenant
isolation, worker lifecycle, resource-ownership, and transaction-ownership
cases. Private project history may inform defect categories
but is not copied or referenced by packaged fixtures. Private dogfood corpora
belong under ignored scratch state or in a separate private repository.
