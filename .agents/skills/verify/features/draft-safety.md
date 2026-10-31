# Back out without losing work or publishing it

A user can inspect a change, decide against publishing it, keep working, or
explicitly discard unfinished work. Only publication changes configuration.

## Sub-features

- `DRAFT-CANCEL`: Escape from preview writes nothing.
- `DRAFT-REVISE`: revise keeps entered values.
- `DRAFT-QUIT`: Keep working and Escape from exit confirmation preserve work.
- `DRAFT-DISCARD`: discard exits without publishing drafts or undoing earlier saved changes.
- `DRAFT-SMALL-TERMINAL`: forms and confirmation remain usable at 80x24.

## How to get to it (user POV)

Open Configuration Hub → Parties → new Party; use preview, revise, Escape, and
quit. The accessible route declines “Publish this complete plan?” and then exits.

## Driving it with tmux

Preconditions: set `journey=draft-safety`; use [terminal setup](../references/terminal.md). Seeds are saved state that must survive this journey.

1. Capture baseline configuration readbacks and file hashes. Open Parties, press
   `n`, choose Global scope, name `journey-unsaved`, select `journey-global`, and
   enter concurrency `2`. Capture the plan, press `e`, and require retained name,
   member, and limit. Resize to 80x24; verify the active field and navigation
   controls remain reachable. Restore 120x30 and return to preview.
2. Press Escape from the preview. Capture the resulting browser/menu and repeat
   readback and hashes. `journey-unsaved` must be absent, with baseline hashes
   unchanged. Return to the menu, press `q`, and require “Discard unfinished
   drafts and exit?”. Accept **Keep working**. The Hub must stay open.
3. Reopen the Party draft with `n`; require the entered values. Return without
   publishing. Press `q`, then Escape from the exit confirmation: require the
   Hub to remain open again. Press `q`, explicitly choose **Discard and exit**,
   and require the owned session to end.
4. Reopen the Hub in the same runtime. Require no unfinished-draft warning on
   quit. Capture final readbacks and hashes: no Party was saved and both seeded
   Profiles remain byte-for-byte unchanged. Cleanup and confirm evidence survives.
5. In a fresh accessible run enter the same Party, decline publication with `n`,
   then select Exit. Decline discard first and verify continued interaction;
   then Exit and confirm discard. Require the same absence and seed preservation.

## Gotchas

- A canceled draft is not promised to persist across process restart. Saved
  configuration must persist; discarded drafts must not reappear.
- Do not send `p` while testing cancel. Capture the chosen confirmation, not only process exit.
- Do not infer no writes from a success banner or missing list row; compare files and readback.
