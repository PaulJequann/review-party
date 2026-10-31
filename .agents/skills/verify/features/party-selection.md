# Build a review team and select it

A user groups saved Profiles into a Party, then selects that Party for ordinary
reviews. Saving the Party alone must not silently select or execute it.

## Sub-features

- `TEAM-SCOPE`: Global Parties offer only Global Profiles; Repository Parties can reference scoped Profiles.
- `TEAM-PUBLISH`: the plan saves exactly the selected members and concurrency.
- `TEAM-SELECT`: Reviews expands the chosen Party and shows effective concurrency before publication.
- `TEAM-ACCESSIBLE`: the accessible adapter reaches the same persisted result.

## How to get to it (user POV)

Run `review-party config`, open Parties, create a Party, then open Reviews and
add it to Repository Configuration. Repeat via `config --accessible` in a fresh run.

## Driving it with tmux

Preconditions: set `journey=party-selection`; follow [terminal setup](../references/terminal.md), including both scoped Profile seeds.

1. Open **Parties**, press `n`. Keep Configuration scope **Global**. Enter Party
   name `journey-team`, leave Description empty. In Profiles require
   `[global] journey-global` and no Repository Profile. Toggle that member with
   Space. At Concurrency limit enter `2`; submit with Enter. Capture each field
   and the complete Plan preview. Require Global Configuration, one member,
   concurrency 2, and the run-owned Global Party save path. Before publication,
   read back Global configuration: the Party must still be absent.
2. Press `e` to revise. Confirm the chosen values survive. Return to the plan,
   press `p`, and capture the publication receipt. Read back Global configuration
   and verify `journey-team` references only `journey-global` in Global scope.
   Configuration must still have no selected Reviews: creation is not selection.
3. Return to the menu with Escape; open **Reviews**. Highlight **Repository
   Configuration**, press `a`, choose `[global] party journey-team`, then Enter.
   Capture the complete plan: Repository save location, Global Party reference,
   resolved `journey-global`, and effective concurrency 1. The Party's limit 2
   must not silently raise the repository selection's limit. Press `p`.
4. Capture the refreshed Reviews screen and all three CLI readbacks. Repository
   Reviews must contain the Global Party; the resolved selection must contain
   its sole Global Profile. No Review Record or Attempt should have been created.
5. In a fresh run repeat Party creation with **Repository** scope and name
   `journey-mixed`, choosing both seeded Profiles. Require both scoped references
   in the plan and readback. In another fresh run repeat steps 1–4 using
   `--accessible`: choose named menu entries by their printed numbers, finish
   multiselect with the displayed completion option, and answer publication `y`.

## Gotchas

- A Global Party cannot reference a Repository Profile. Do not remove the local
  seed to make the picker appear correct.
- Tab navigation can rebuild fields. Check focus after every action.
- The accessible and redrawing adapters need separate evidence; one does not prove the other.
- Reviewer/model values are configuration fixtures, not an availability claim.
