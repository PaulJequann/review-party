# Review Party baseline v1

Status: implemented on 2026-10-07. Extends
[Guided initialization v1](guided-init-v1.md).

## Problem

A new Caller who runs `init` must invent a review composition before the first
Review. The packaged Templates already cover bugs, code quality,
documentation, and test audit, but Templates cannot execute. A committed
selection that names `global:baseline` also leaves each teammate to guess the
members of a Party the repository does not describe.

## Decision

The Review Party baseline is four Global Profiles and one Global Party.

- The Profiles are created from the Templates the engine catalog marks as
  baseline: `bugs`, `code-quality`, `documentation`, and `test-audit`. Each
  Profile has the Template's name and records its Template ID and revision.
- The Global Party `baseline` references exactly those four Global Profiles,
  in Template ID order. `init` creates it with a Concurrency Limit equal to
  the member count. An existing Party with those members counts as the
  baseline whatever its limit, which stays the Caller's to tune.
- A repository adopts the baseline by selecting `{"party": "baseline"}` in the
  Global group of its Review selection.

The baseline is a Party so that `run --party baseline` works in any repository
and a committed `global:baseline` names one composition on every machine.
Templates stay non-executable. The Party is an ordinary Global Party file,
written only when the Caller confirms its Plan.

No Reviewer, model, reasoning effort, or Attempt deadline is packaged. The
Caller chooses one execution for every Profile the baseline creates.

## Modes

| Invocation | Behavior |
|---|---|
| `init --baseline --reviewer R --model M --effort E --deadline D` | Creates missing baseline Profiles, then the Party, then adds the Party to the selection. |
| `init --baseline` | Same, when every baseline Profile exists. Refuses before writing when one is missing. |
| `init` in a terminal | The first-use journey offers the baseline as its first, preselected option, or prints why it is blocked. |
| `init` without a terminal | The report's first line is the `init --baseline` command, with execution placeholders only when a Profile is missing. |

The four execution flags are all or none and are rejected without
`--baseline`. Each step is its own Plan with its own confirmation, and writes
without a terminal need `--yes`. A step whose outcome already holds is
skipped, so a rerun after a complete run writes nothing.

When the selection step creates a new selection, its Concurrency Limit is the
member count, because a selection runs with its own limit rather than the
Party's. An existing selection keeps its limit. `init` prints a note when that
limit is below the member count.

## Partial states

Each member is Missing, Ready, Foreign, or Broken. The Party is Missing,
Ready, Differs, or Broken.

- A Missing member is created. With one execution for every member, from the
  flags or the journey's shared question, all missing members publish in one
  Plan, all or none. A Caller who declines the shared question creates each
  member through its own Profile Creation Plan, so a stop between members
  leaves the earlier ones in place for a rerun.
- A Foreign member exists but was not created from its Template. It is kept as
  it is, and `init` prints one note naming it.
- A Broken member or Party does not load. It blocks every baseline write.
- A Party that Differs references other Profiles. It blocks the Party and
  selection steps, since selecting it would run a different composition under
  the baseline name. The journey and the report never offer it as an
  ordinary Party. A repository that already selects it is reported as
  blocked rather than ready. The message names the Party file, because V1 has
  no Party edit or delete command.

Because each step publishes before the next is planned, a declined or failed
step leaves a valid configuration that a rerun completes.

## Shared repositories

A teammate whose clone selects `global:baseline` and who lacks the Party or
its Profiles sees one report line naming `init --baseline`. The journey asks
one question for the whole baseline. Either path creates the missing Profiles
and Party and leaves Repository Configuration byte-identical.

## Non-goals

- Selecting the baseline without the Caller's confirmation.
- Packaged execution values or a default Reviewer for baseline Profiles.
- Packaged executable Parties.
- Rewriting an existing Profile or Party to match the baseline.
- Per-member execution on the flag path. The journey offers it by declining
  the shared execution question.
