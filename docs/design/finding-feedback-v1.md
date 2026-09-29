# Finding feedback v1

Status: implemented locally on 2026-09-29.

A Caller agent reads a Review's Findings and decides, for each one, whether it
is real. Today that decision is lost when the Caller's session ends. Finding
feedback records it as a Finding Verdict (accepted, rejected, or deferred, each
with a required reason) from one call. Accepted and rejected Findings are equal
inputs: both are the labels later work needs to measure precision, build Eval
Cases, and tune Profiles without a human labeling runs.

End-user Callers receive no instructions from this repository. Review Party
reaches them only through its own output, help, and JSON contract, so every
byte added to a report is paid by every Caller that reads it.

## Interface

```sh
review-party finding record rp_1723200000000_0123456789abcdef <<'E'
1 accept nil map write reproduced by TestSaveFirst
2 reject guard exists at `store.go:88`
3 defer real but outside this change
E
recorded 3
```

A Finding is referenced by the Review ID in the report header and the ordinal
the report already prints. No per-Finding reference is printed.

Input is stdin lines `N accept|reject|defer REASON`. A quoted heredoc keeps
backticks, `$`, and apostrophes in a reason inert; positional reasons in double
quotes would run backticks as command substitution. `REVIEW` is required. A
Review Bundle takes one call per member Review.

`finding list [REVIEW] [--repo] [--profile]` prints current verdicts. JSON is an
object. A verdict whose Finding text has since changed shows as stale.

Exit 2 means the input was rejected before the ledger opened (grammar, verb,
reason, duplicate ordinal, flags). Exit 1 means a ledger fact, for example
`line 3: review rp_... (completed) has findings 1-3, not 4; nothing recorded`.
A batch is atomic.

The reason is required for all three verdicts: one line, 1 to 240 bytes.

## Report

Unjudged Findings render exactly as before. A judged Finding gains one line
(`accepted: REASON`) or, in JSON, a `verdict` object. While any Finding in the
report is unjudged, the report ends with one `feedback:` line (JSON: a
top-level `feedback` string) holding the `finding record` template with the
Review ID filled in. A report with no Findings, or with every Finding judged,
carries no hint. The template is Review Party's text; no Reviewer output reaches
it, and it never says a Finding blocks delivery.

`wait` and `inspect` annotate reports with misses and verdicts through one
shared step. `run` and `replay` skip it because a Review created in the same
process has neither. The hint is computed where the report is printed, so `run`
and `wait` still print a finished Review identically.

## Storage

Migration 12 adds `finding_verdicts`, append-only, with a foreign key to
`reviews(id)` only. Each row stores a 16-hex sha256 digest of the Finding's
seven text fields, computed inside the write transaction. The current verdict
for a Finding is the latest row whose digest matches the live Finding.
Re-saving a record reinserts identical Findings, so verdicts survive. An eval
retry that places different text under the same ordinal leaves the old verdict
stale instead of inherited. Repeating the current verdict writes nothing.

A Finding is eligible when its row exists, checked in the write transaction.
Incomplete Reviews with partial Findings are eligible.

## Measurement

`cmd/review-party/testdata/agent-cost.tsv` records, per agent-facing surface,
the exit code, the bytes a Caller types, the bytes it reads, and an estimated
token count (`ceil(bytes/4)`, not a tokenizer count). `TestAgentCost` drives
the real CLI entry point over fixed fixtures and fails on any difference. Run
with `-update -v` to regenerate and print the delta; the file diff is the
before/after report. A small budget table holds the promises `-update` cannot
move: it refuses to rewrite the file while a budget is broken, and
`TestFeedbackHintStaysSmall` caps the hint itself at 115 bytes.

Latency is reported, not gated, because it depends on the machine. Rerun the
`Benchmark*` functions in `agent_cost_test.go` at two commits to compare. On
2026-09-29, in process, `inspect` of three Findings took 4.35 ms before and
4.56 ms after (JSON), and `finding record` of three lines took 4.08 ms. Through
the binary, `inspect --format json` median was 19.5 ms before and 19.7 ms after.

## Not included

- No `guide` command. `finding record --help` carries the contract.
- No `--evidence` flag. Evidence fits in the reason.
- No per-verdict subcommands and no JSONL input.
