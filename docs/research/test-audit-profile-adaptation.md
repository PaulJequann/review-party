# Test audit Profile adaptation

Status: implemented locally; evaluation and live dogfood evidence remain
separate acceptance steps

Date: 2026-10-07

## Source

Review Party adapts the judgment in the `skeptical-test-audit` Agent Skill, a
local Caller skill rather than a published document. The skill judges each
automated test by the regression it catches instead of by coverage, passing
status, or assertion count. The burden of proof sits on keeping a test, the
harsher verdict wins a tie, and tests written in the current session get no
benefit of the doubt.

Its workflow has five steps. The Reviewer gathers evidence, then judges each
test with a Keep, Improve, Merge, or Remove verdict in a table whose rows name
the incorrect implementation, harmful result, and affected caller that the test
alone catches. It then maps every new branch, toggle, filter, and acceptance
criterion to a proving test to find gaps, applies changes when asked, and
summarizes with counts. A "What to flag" catalogue names tautologies, change
detectors, removed-behavior guards, regression tests, fixtures, fakes,
implementation coupling, contract level, specialized tests, and smells.

## What was kept

The packaged `test-audit` Template keeps the skill's judgment unchanged: the
burden of proof on keeping, the harsher verdict winning a tie, no benefit of the
doubt for newly added tests, the defect-naming discipline, the four verdict
definitions, the gap mapping including combination with indexing, pagination,
and ordering, and the full "What to flag" catalogue.

## What changed for a read-only Reviewer

A Review Party Reviewer reads and searches the repository and has no shell, so
three parts of the skill cannot run as written.

The skill's mutation step removes a protection in a scratch copy and observes
the test fail. The Template replaces it with a trace from the protection to the
test's assertion under the test's own fixture. When reading cannot settle the
outcome, as with scheduling, timing, or generated output, the Reviewer states
that the "would fail" claim is unverified. This matches the preamble Review
Party adds to imported Skill Templates in
[`internal/configuration/skill_template.go`](../../internal/configuration/skill_template.go).
The mutation the Reviewer could not run moves into the Finding's `Test:` field
as the check the Caller should run.

The skill reports a verdict table, verdict counts, before and after test
counts, and a summary. The Template reports only through the canonical result
contract that the prompt appends, so verdicts map onto Findings. Improve,
Merge, and Remove each become a Finding that names the test, the defect it
misses or the false-green it permits, and the smallest repair. Keep produces
nothing. A gap becomes a Finding that names the unproven behavior and the
regression that would ship green. The `Test:` field carries the production
mutation that the repaired or new test must fail on, or the surviving test for
a Merge or Remove.

The skill's step 4 applies changes. The Template drops it entirely because
Reviewers never edit the Subject, and the caller and repository governance own
remediation.

The Template does not restate what the prompt already supplies. The tool rules
in [`internal/engine/library.go`](../../internal/engine/library.go) forbid
shell and write tools and direct the Reviewer to project instructions, and the
result contract in
[`internal/result/contract.go`](../../internal/result/contract.go) fixes the
block format, the eight-Finding cap, and the severity floor.

## Materiality

The skill audits every test in scope and lists every verdict. A Profile must
instead report only Findings worth acting on, so the Template reports a Finding
in three cases. A test gives false confidence when it claims a behavior its
assertions cannot observe or passes on a fixture or fake production cannot
produce. A gap leaves a material changed behavior unproven. A test the Subject
adds catches no plausible defect or duplicates a sibling, so it costs edits
without protecting anything. The third case keeps the skill's Remove and Merge
verdicts reportable, because change detectors and removed-behavior guards are
the regressions a test audit exists to stop. It ranks below the first two.

The Template drops naming-only comments unless the name misleads about the
contract, smells that do not change what a test proves, and preferences about
structure or assertion count.

Scope covers the tests the Subject adds or changes, the fixtures, fakes, and
helpers it changes, and gaps in the production behavior it changes. Pre-existing
tests are out of scope unless the Subject relies on them to prove changed
behavior. A deleted test is in scope only when its removal leaves shipped
behavior unproven.

## Relation to `bugs`

The `bugs` Profile lists test validity as one risk among many and asks for the
false-green behind a test gap. `test-audit` is the dedicated test-suite audit.
It judges every test in scope, applies the full flag catalogue, and reports
tests that protect nothing, which a bug review treats as out of scope. A
production defect enters a `test-audit` Finding only as the regression a
missing or weak test lets ship.

## Eval implications

A future packaged suite should run the actual `test-audit` Profile through the
ordinary Review path, as the `code-quality` suite does. Candidate case
families:

- a test named for a protection, such as a lock or recheck, whose assertion
  cannot observe the protection's removal;
- a tautology that asserts the fake's own canned response;
- a change detector that asserts substrings of a template or configuration
  file;
- a removed-behavior guard added alongside a feature deletion;
- a fixture production cannot produce, with another test covering the real
  shape;
- a fake that adds a failure mode the real adapter cannot produce;
- a mock-call assertion with no external interaction contract;
- a new production branch or filter with no proving test, including one that
  interacts with pagination or ordering;
- a table case that duplicates a sibling's defect;
- a deleted test that leaves shipped behavior unproven; and
- clean cases: a test that traces cleanly from protection to assertion, an
  absence check that guards a security or privacy contract, a mock-call
  assertion on an audit or delivery contract, and a multi-step workflow test
  that proves one behavior. These cases keep the Profile from being rewarded
  for flagging every test.

Grading should also check that an unverifiable "would fail" claim carries an
unverified label rather than a confident assertion.

## Architectural inference

Like `code-quality`, this adaptation needs no new execution seam. The packaged
Template registry, Profile Creation, and the existing result contract carry it
unchanged.
