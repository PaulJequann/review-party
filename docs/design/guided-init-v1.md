# Guided initialization v1

Status: accepted on 2026-10-01; not implemented. Slice 1 of
[Review Checkpoints v1](review-checkpoints-v1.md).

## Problem

`review-party init` prepares managed state and then prints "Next: configure a
saved Review Profile". A Caller who knows only `init` reaches a repository that
cannot run a Review. In a shared repository the gap is wider: the committed
selection can name `global:baseline`, which exists only on the machine of the
person who wrote it.

`init` is the one command a Caller needs to know. It must bring a repository to
a runnable state.

## Decision

Review Party Initialization composes existing operations instead of replacing
them. It prepares state, then leads the Caller through Profile Creation and the
repository's Review selection where the repository still lacks them. Each of
those operations keeps its own reviewed Plan.

Run without setup flags in a terminal, `init` opens the Configuration Hub's
first-use journey. There is no separate wizard; `init` and `config` drive the
same focused forms.

## Modes

| Invocation | Behavior |
|---|---|
| `review-party init` in a terminal | Prepares state, then runs the first-use journey. |
| `review-party init` without a terminal | Prepares state, then reports each missing piece with the exact flagged command that adds it. Writes nothing else. |
| `review-party init` with any setup flag | Applies only the flags given, without prompting. Pieces the flags do not name stay unchanged. Without a terminal, writes need `--yes`. |

Setup flags in this slice are `--profile NAME` and `--party NAME`, each
repeatable. An unqualified name resolves Repository before Global, the same as
`run`, and is added to the selection array of the scope it resolved to. Adding
a reference that is already selected changes nothing.

## First-use journey

Every step is skipped when its outcome already holds. A cancelled `init` loses
nothing that a rerun cannot finish.

1. Prepare state. This step prints nothing unless state recovery is needed.
2. Bind the selection.
	- If the repository declares a selection, resolve each Global name it
	  names. For each unresolved Profile, offer Profile Creation with the
	  Template of the same name preselected when one exists. For each
	  unresolved Party, offer Party creation.
	- If the repository declares no selection, offer the Caller's existing
	  Global Parties and Profiles, or Profile Creation.
3. Report what remains, one line each.

Slices 3 and 4 of Review Checkpoints v1 add Checkpoint and Integration steps
between steps 2 and 3.

## Shared repositories

Repository Configuration is committed and shared. A selected Global Profile or
Party is a name that every Caller binds in their own Global Configuration. The
repository decides what is reviewed; each Caller decides which Reviewer and
model review it. A teammate who clones the repository and runs `init` is asked
for exactly the names the repository expects.

A Profile still fixes its Reviewer and model together with its instructions.
Separating the two is out of scope.

## Open question

A Global Party in a committed selection carries no composition. A teammate who
binds `global:baseline` must invent its members. A Repository Party that
references Global Profiles commits the composition and leaves only Profile
names to bind. Recommendation: in a repository without a selection, the
journey offers to save the chosen Global Party's composition as a Repository
Party, so that teammates bind Profiles, not Parties.

## Out of scope

- Checkpoints, Integrations, and `doctor` changes. See Review Checkpoints v1.
- A combined Plan for the whole journey. Each step publishes through its own
  Plan.
- Changes to Profile or Party semantics.
