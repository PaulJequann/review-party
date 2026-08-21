# Human adjudication and scoring v1

An Adjudication Revision is an immutable human decision over one completed Eval
Suite Run. It maps stored expected Findings to the ordinary Review Records'
reported Findings without rewriting either source. Corrections publish another
revision with a monotonically increasing revision number.

## Human workflow

```sh
review-party eval adjudication export esr_... > decisions.json
# A human or assisting agent edits disposition, mapping, and notes fields.
review-party eval score esr_... --adjudication decisions.json
review-party eval inspect ar_... --format json
```

The export renders cases before aggregates and uses stable public identities:
Eval Run ID, Eval Case ID, expected Finding ID, and reported Finding ordinal.
It includes complete expected and reported evidence. SQLite keys and SQL are not
part of the interface.

Export requires every planned Eval Run to link an ordinary Review Record. It
fails explicitly for a Pending, Running, or no-Review Incomplete case; the
Caller must first obtain a review-linked terminal run for every case rather
than silently scoring partial coverage.

For every completed case, each expected Finding must be exactly one of
`matched`, `missed`, or `uncertain`. Each reported Finding must be exactly one
of `matched_expected`, `novel_valid`, `false_positive`, or `uncertain`. A match
is valid only when both sides name the same pair. Membership and evidence are
checked against the frozen Eval/Review records before publication.

Incomplete cases require no guessed dispositions. They remain unscored and
retain their termination category.

A result-contract validation failure is therefore measured as an operational
compatibility failure, not automatically as a semantic miss. The raw assistant
artifact may support diagnosis, but V1 never promotes malformed prose into a
reported Finding during adjudication.

## Metrics

- Defect recall is matched expected Findings divided by matched plus missed
  expected Findings.
- Finding precision is matched plus novel-valid reported Findings divided by
  all adjudicated non-uncertain reported Findings.
- Clean-case accuracy is zero-reported-Finding known-clean cases divided by all
  completed known-clean cases.
- Clean false-positive rate is false positives divided by reported Findings on
  completed known-clean cases. A zero denominator remains undefined.
- Completion rate is completed Eval Runs divided by all Eval Runs.

Uncertain expected/reported counts, Incomplete case count, and termination
categories are separate facts. Every ratio stores its numerator, denominator,
and value when defined. The evaluator is pure; persistence transactionally
stores the exact Adjudication Document and its derived Eval Score.

No LLM judge, fuzzy matching, terminal scraping, comparison, or delivery policy
is part of this revision.
