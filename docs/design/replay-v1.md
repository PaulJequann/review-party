# Replay v1

Status: implemented locally on 2026-08-11.

## Interface

`Conductor.Replay` accepts one source Review ID and refuses Reviewer, model,
and effort overrides. This small Interface hides ledger lookup,
reconstructibility checks, stored Profile reconstruction, relationship
publication, and ordinary Review execution. The CLI does not see rows,
migrations, or compiler internals.

Replay creates a new ordinary Review. The source remains immutable and the new
record owns its ID, lifecycle, Attempts, Result, timings, and current build
provenance. `replays_review_id` records the immediate source relationship in the
public Review Record and relational projection; inspect and history JSON expose
the same fact.

## Frozen inputs

Only a committed-range Subject is replayable. Before creating a new record,
Review Party resolves the recorded full base/head objects from the recorded
local repository, rebuilds the binary-capable diff and identity, and requires
the identity to equal the stored Subject. Missing objects, a moved repository,
or identity disagreement fail before Reviewer availability or launch.

Default replay uses the stored Profile Revision and Profile Snapshot directly.
It does not discover or compile the current Profile file. Prompt assembly uses
the stored instructions and source provenance. The recorded result-contract,
single-Pass/single-Attempt contract, capability requirements, and execution
deadline must remain supported.

The original Reviewer ID, model, effort, harness, and transport must still be
available exactly. Current configuration may disable or disallow a choice, but
Review Party never falls back. A Replay selection that names a Reviewer, model,
or effort is refused before any record is created; a different choice is an
ordinary new Review, not a Replay.

## Semantics and exclusions

Replay reproduces frozen experiment inputs. Model output
is non-deterministic: Results and Findings are never expected or asserted to
equal the source. Replay does not overwrite a record, replay application events,
fetch missing Git objects, recover another machine's repository, or upgrade to
a latest model implicitly. Working-changes reconstruction remains unsupported
until a separate design can reproduce its exact repository view safely.
