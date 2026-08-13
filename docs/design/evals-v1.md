# Evaluation execution v1

Review Party evaluations are Caller-controlled experiments over ordinary
Reviews. The product supplies a framework and immutable packaged suites;
Caller-owned global and project suites can describe different concerns without
changing product policy.

## Domain relationships

An Eval Case fixes one known problem and its human-confirmed expected Findings
or known-clean evidence. It never selects a Reviewer, model, or effort. An
Experiment Configuration selects the Review Profile, Reviewer, model, effort,
and Execution Deadline tested against a suite.

```text
Eval Suite x Experiment Configuration = Eval Suite Run
                                           |
                                           +-- Eval Run -- ordinary Review Record
```

Every suite rerun creates new records. Execution categories (`completed_clean`,
`completed_findings`, and `incomplete`) describe what happened; all initial Eval
Runs remain `awaiting_adjudication`. Scoring and comparison are later concerns.

## Suite sources

`global:canary-bugs` selects elementary cases for fast plumbing and
result-contract checks. `global:general-bugs` selects the broader bug benchmark
embedded in the installed Review Party version. `global:code-quality` selects
the packaged maintainability benchmark and is intended to be evaluated with the
`code-quality` Profile; other Profile selections are not scored against its
expected Findings. A filesystem path selects a Caller-owned suite whose
relative case and fixture paths are anchored at `suite.json`. Unknown fields,
unsupported versions, duplicate case IDs, contradictory classifications,
missing fixtures, Git metadata, and symlinks fail full-suite preflight before
any Agent Harness launch.

A suite manifest lists case files explicitly. Each case uses `change` or `state`
mode, identifies `base` and `head` fixture directories as applicable, and
declares either expected Findings or known-clean evidence. Expected Findings
use semantic behavior and impact; paths are supporting evidence rather than
exact-match scoring keys.

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
