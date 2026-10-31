# Review Party verification map

Read this index before selecting a journey. Run all commands from the repository
root with `.agents/skills/verify-review-party/scripts/verify.sh` as the harness.
The helper is shared with the existing `verify-review-party` skill.

## Baseline

Follow Launch and Doctor in [the skill](../SKILL.md). Each journey needs a fresh
run: an owned binary, empty HOME and XDG roots, disposable Git repository, and
initialized managed state. No authentication or Review Records are seeded.
Parallel runs use distinct run directories; never reuse another run's identity.
Keep evidence in `$run_dir/evidence/` and preserve it during cleanup.

## Driving conventions

Capture every public CLI command with the shared helper. Expected negative
controls require exit-on-error disabled; record their exit status and named
rejection, then run doctor before continuing. Inspect JSON values and durable
side effects, rather than treating exit zero as proof. Report unmet
preconditions as failures or unverified coverage with the exact reason; never
weaken a journey or claim another entry point covers it.

## Journey selection

For TUI or configuration changes, choose by the user's risk, not the shortest
recipe. These are the first journeys to inspect:

| Priority | Journey | User consequence and regression risk |
| --- | --- | --- |
| 1 | [Draft safety](draft-safety.md) | Accidental publication or lost work; crosses form, preview, back, quit, and restart states. |
| 1 | [Party to selected reviews](party-selection.md) | A saved team never runs, or the wrong scoped members run; crosses definitions, references, selection, and concurrency. |
| 1 | [Review inheritance](review-inheritance.md) | Repository overrides silently replace shared intent; exercises source, ordering, removal, and inheritance. |
| 1 | [Interrupted first use](first-use-recovery.md) | A new user gets stuck after partial setup; crosses several separately confirmed publications. |
| 2 | [Configuration Hub](../../verify-review-party/features/configuration-hub.md) | Profile creation/edit, current model retention, model refresh, and both terminal adapters. |
| 2 | [Global Checkpoints](../../verify-review-party/features/global-checkpoints.md) | Declaring a policy and enrolling hooks are distinct actions with Git side effects. |
| 2 | [Skill Templates](../../verify-review-party/features/skill-templates.md) | Missing or changed source instructions must not silently produce the wrong review. |

Read [the terminal recipe](../references/terminal.md) for seeded scoped Profiles,
owned tmux sessions, action transcripts, readback, and cleanup. Normal and
accessible modes each need proof. Run the changed journey and its negative
control; a broad TUI change warrants the four priority-1 journeys in both modes.
The table prioritizes verification effort; it does not impose a delivery gate.
Unexecuted journeys are coverage plans, not passed verification.

## Baseline features

- [Binary identity](binary-identity.md): human and JSON version output, plus a revision mismatch control.
- [Managed initialization and history](managed-initialization.md): missing repository rejection, repeated initialization, and empty history.
- [Profile configuration](profile-configuration.md): confirmation rejection, Profile publication, explain readback, and validation.

## Additional surfaces

[The extended map](../../verify-review-party/features/README.md) owns the detailed
terminal harness and journeys for Configuration Hub, Global configuration, Global
Checkpoints, and skill Templates. Read the corresponding feature before driving
one of those surfaces. Terminal and accessible prompt paths require separate
proof. The three local recipes do not verify live Reviewer execution, hooks,
Eval, or every CLI command. Use the dogfood skill for a bounded real review of
this checkout; do not infer that configuration success proves Reviewer access.
