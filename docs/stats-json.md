# `review-party stats --format json`

`review-party stats` summarizes the Review ledger over one time window. Its
JSON form is a contract for scripts and digests. This page defines every field.

```sh
review-party stats --since 24h --format json
review-party stats --since 7d --repo . --format json
```

## Stability

`schema_version` is `1`. Additive changes keep the version: a new field, a new
key in an open map, or a new value in an open enumeration. Removing or renaming
a field, changing its type, or changing what it counts increments the version.
Consumers should ignore fields they do not know.

The command only reads the ledger. It exits `2` for an invalid `--since`, and
`1` when `--repo` is not a Git repository or the ledger cannot be read or has
not been initialized.

## Window and cohorts

- `--since` takes a positive Go duration (`24h`, `90m`), a whole number of days
  or weeks (`7d`, `2w`), or an RFC3339 timestamp. Day and week windows step back
  calendar days. The default is `7d`.
- A Review is in the window when it was created at or after `window.since`.
  The `runs`, `status`, `terminations`, `duration_ms`, `failure_diagnostics`,
  `findings`, and `verdicts` sections all describe that one cohort of Reviews.
- Checkpoint Waivers and Review Bundles are counted by their own creation time.
- `--repo` resolves to the canonical Git root and limits Reviews, Waivers, and
  Bundles to that repository.
- Times are RFC3339 in UTC. Days are UTC calendar dates.
- A rate is `null` when its denominator is zero. Rates are fractions from 0 to
  1, rounded to four decimal places.

## Fields

| Field | Type | Meaning |
| --- | --- | --- |
| `schema_version` | integer | The contract version described here. |
| `window.since` | time | Start of the window, inclusive. |
| `window.until` | time | When the report was taken. |
| `window.repository` | string | The `--repo` root. Omitted when every repository is counted. |
| `runs.total` | integer | Reviews created in the window. |
| `runs.per_day` | array of `{date, runs}` | One entry for every UTC date from `window.since` through `window.until`, oldest first, including days with `0` runs. |
| `runs.by_repository` | array of `{name, count}` | Reviews by repository root. |
| `runs.by_profile` | array of `{name, count}` | Reviews by Profile name. |
| `runs.by_reviewer` | array of `{name, count}` | Reviews by Reviewer id, such as `codex` or `opencode`. |
| `runs.by_selection` | array of `{name, count}` | Reviews by how their Review Bundle chose Profiles: `repository_default`, `explicit_profile`, `explicit_party`, `unknown` for a Bundle without a recorded selection, or `unbundled` for a Review that no Bundle lists. |
| `status.clean` | integer | Completed Reviews with a clean result. |
| `status.findings` | integer | Completed Reviews with at least one finding. |
| `status.failed` | integer | Incomplete Reviews not ended by cancellation. |
| `status.cancelled` | integer | Incomplete Reviews ended by cancellation. |
| `status.in_progress` | integer | Pending or running Reviews. |
| `terminations` | map of category to integer | Incomplete Reviews by termination category, such as `deadline_exceeded` or `unknown_failure`. |
| `duration_ms.samples` | integer | Completed Reviews with a recorded total duration. |
| `duration_ms.p50`, `duration_ms.p90` | integer | Nearest-rank percentiles of those durations, in milliseconds. `0` when `samples` is `0`. |
| `failure_diagnostics` | array of `{diagnostic, count, last_seen}` | The ten most frequent failed-Attempt diagnostics. |
| `findings.total` | integer | Findings in the window's Reviews. |
| `findings.per_completed_review` | number or null | `findings.total` divided by completed Reviews. |
| `findings.by_severity` | map of severity to integer | Findings by lowercased severity. |
| `verdicts.judged` | integer | Findings with a recorded Verdict. |
| `verdicts.unjudged` | integer | Findings without a Verdict. |
| `verdicts.accepted`, `.rejected`, `.deferred` | integer | Findings by their latest Verdict. |
| `verdicts.accepted_rate`, `.rejected_rate`, `.deferred_rate` | number or null | Each count divided by `verdicts.judged`. |
| `checkpoint_waivers.total` | integer | Checkpoint Waivers recorded in the window. |
| `checkpoint_waivers.by_waived_by` | map of source to integer | Waivers by how they were granted, such as `terminal` or `non-interactive`. |
| `bundles.total` | integer | Review Bundles created in the window. |
| `bundles.completed`, `.incomplete`, `.in_progress` | integer | Bundles by lifecycle. |
| `bundles.completion_rate` | number or null | `completed` divided by `completed + incomplete`. Bundles still in progress are excluded. |

Every `{name, count}` array is ordered by count, highest first, then by name.

### Failure diagnostics

Every Attempt whose outcome is not `completed` contributes its diagnostic,
including an Attempt that a later retry in the same Review recovered from. The
diagnostic is normalized so repeats of one failure group together. Terminal
color codes are removed. Timestamps become `<time>`, UUIDs become `<id>`, Review
ids become `<review>`, hex strings of 12 or more characters become `<hex>`, and
runs of 5 or more digits become `<n>`. Whitespace is collapsed and the text is
cut to 200 characters. An empty diagnostic is reported as
`(no diagnostic) <outcome>`. Groups are ordered by count, highest first, then
by text. `last_seen` is the latest completion time in the group.

### Caller and trigger

The ledger does not record whether an agent, a hook, or a person started a
Review, so stats cannot report it. `runs.by_selection` is the closest recorded
fact.

## Example

```json
{
  "schema_version": 1,
  "window": {"since": "2026-10-12T12:51:39.293938Z", "until": "2026-10-13T12:51:39.293938Z"},
  "runs": {
    "total": 17,
    "per_day": [{"date": "2026-10-12", "runs": 12}, {"date": "2026-10-13", "runs": 5}],
    "by_repository": [{"name": "/work/app", "count": 12}, {"name": "/work/api", "count": 5}],
    "by_profile": [{"name": "bugs", "count": 5}, {"name": "code-quality", "count": 4}, {"name": "documentation", "count": 4}, {"name": "test-audit", "count": 4}],
    "by_reviewer": [{"name": "codex", "count": 17}],
    "by_selection": [{"name": "repository_default", "count": 16}, {"name": "unbundled", "count": 1}]
  },
  "status": {"clean": 5, "findings": 11, "failed": 0, "cancelled": 1, "in_progress": 0},
  "terminations": {"cancelled": 1},
  "duration_ms": {"samples": 16, "p50": 68526, "p90": 113345},
  "failure_diagnostics": [{"diagnostic": "context canceled", "count": 1, "last_seen": "2026-10-13T05:24:31.57711Z"}],
  "findings": {"total": 14, "per_completed_review": 0.875, "by_severity": {"medium": 14}},
  "verdicts": {"judged": 14, "unjudged": 0, "accepted": 7, "rejected": 5, "deferred": 2, "accepted_rate": 0.5, "rejected_rate": 0.3571, "deferred_rate": 0.1429},
  "checkpoint_waivers": {"total": 3, "by_waived_by": {"non-interactive": 3}},
  "bundles": {"total": 4, "completed": 3, "incomplete": 1, "in_progress": 0, "completion_rate": 0.75}
}
```
