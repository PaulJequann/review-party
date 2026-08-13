# Eval comparison v1

Eval comparison is a deterministic, side-by-side report over two stored,
human-adjudicated Eval Suite Runs. It observes the evidence; it does not choose
a winner, promote a model, or make a delivery decision.

## Command

```sh
go run ./cmd/review-party eval compare \
  --baseline ar_... \
  --candidate ar_... \
  --format json
```

Both identifiers refer to immutable Adjudication Revisions. The command loads
their parent suite runs and ordinary Review Records from the managed ledger.
The JSON report preserves the suite and revision digests, effective Profile
Revision, Reviewer/model/effort, transport and harness, Review Party build, and
runtime provenance for both sides.

## Case matching

Cases are comparable only when their stable case ID and case revision digest
match exactly. The report uses that intersection for both denominators and
lists baseline-only, candidate-only, and same-ID/different-revision cases
separately. A comparison with no shared case revision is rejected. No case is
silently dropped or pooled with a different fixture revision.

Each side is rescored from the stored case-level adjudication decisions for
the shared intersection. This keeps recall, Finding precision, clean-case
accuracy, clean false-positive rate, and completion numerators and
denominators traceable to the same evidence. Incomplete cases remain outside
semantic quality denominators while their completion and termination
categories remain visible.

## Interpretation boundary

The report emits each metric as baseline, candidate, and candidate-minus-
baseline delta. Runtime totals, averages, and the delta are included as
operational evidence. It intentionally does not calculate a weighted universal
score, statistical significance, automatic promotion, CI gate, or leaderboard.
Caller governance decides how to act on the tradeoffs.
