# Choose what runs here and return to shared defaults

A repository can override Global Reviews, reorder and remove its selections,
and later inherit the shared selection again without deleting saved definitions.

## Sub-features

- `REV-SOURCE`: the in-force source and file targets stay distinct.
- `REV-ORDER`: move and remove affect the selected scoped entry only.
- `REV-LIMIT`: concurrency edits are previewed and validated.
- `REV-INHERIT`: inheritance restores Global defaults and preserves Profiles and Parties.

## How to get to it (user POV)

Configuration Hub → Reviews → Global or Repository Configuration. Use `a` add,
`m` move, `r` remove, `c` concurrency, and `i` inherit Global as offered in the footer.
The accessible Reviews menu names the same operations.

## Driving it with tmux

Preconditions: set `journey=review-inheritance`; seed both Profiles with
[terminal setup](../references/terminal.md). Start with no selection.

1. In Reviews highlight Global Configuration, press `a`, add the Global Profile,
   inspect the Global save path and resolved preview, then publish. Capture
   readbacks: Global Reviews selects `journey-global`, Repository inherits it.
2. Highlight Repository Configuration, press `a`, and add the Repository Profile.
   Before publishing inspect the **entire** proposed selection; record whether
   inherited entries are retained. Publish only if the preview matches the
   intended selection. Require Repository Configuration to become in force and
   Global Configuration to remain unchanged. Read back both files.
3. Add the Global Profile to Repository selection if not already present, so
   that both scopes are represented. Use `m` on an entry and inspect available
   destinations: movement is within that scope's group, not across groups.
   With one entry in a group there is no meaningful reorder; report that
   precondition and create a second Profile in that scope through the public
   create command before testing reorder. Capture original and resulting order.
4. Press `c` on Repository Configuration; enter `0`. Require validation to block
   publication. Correct it to `2`, inspect resolved effective concurrency, then
   publish. Use `r` on the Repository Profile and inspect preview before publishing;
   require the Global entry to survive. Capture readback after each operation.
5. On Repository Configuration press `i`. Capture the plan and resolved Global
   selection. Cancel once with Escape and require the override to remain.
   Reopen inheritance, publish, and require Global Configuration to be in force.
   Repository file readback must no longer declare Reviews; unrelated repository
   settings and both saved Profiles must remain. Reopen the Hub and verify the
   same source badge. Repeat in a fresh accessible run using named operations.

## Gotchas

- `scope` names a referenced definition; the highlighted file is the edit target.
  A Global reference inside Repository Reviews does not mean editing Global Reviews.
- Do not delete a configuration file to simulate inheritance: use the public operation.
- An empty explicit selection and an inherited selection are different states.
- Do not invoke `run` to inspect selection; it launches Reviewers. Preview and config readback prove this journey without external execution.
