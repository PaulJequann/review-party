# A Caller's Global Checkpoints apply to the repositories they enroll

ADR 0001 gives the repository ownership of its Review Checkpoints, which leaves a Caller who wants the same pre-push check in every repository they work in declaring it once per repository, and leaves a shared repository whose team declares none with no Checkpoint at all for that Caller. We let Global Configuration declare Checkpoints too, and bound them by enrollment. A repository is enrolled when it has a Repository Configuration file, so a clone that never ran `review-party init` sees no Checkpoint. The Repository `checkpoints` field, when present, replaces the Global one as a whole, so a team's declaration still wins. The hook refusal and `checkpoint check` name the Global file when it is the one that declared the Checkpoint, and say nothing extra for a Repository declaration, which is the file every teammate already reads.

## Considered Options

- Merging Global and Repository Checkpoints by name. Rejected because a Caller's declaration could then tighten a team's declared floor without the team seeing it, and because merging is the opposite of how the Review selection composes.
- Applying Global Checkpoints to every repository on the machine. Rejected because a hook installed by one repository's team would then enforce a Checkpoint in clones the Caller never set up, and because an unenrolled repository has no state directory to record its Reviews in.
- An explicit `enrolled` field. Rejected because the presence of the Repository Configuration file already records that the Caller set the repository up, and a second flag could disagree with it.

## Consequences

`init` offers enrollment when the Global Configuration declares Checkpoints and the repository has no Configuration file. `init --enroll` writes a file holding only the schema version. A git hook in an unenrolled repository exits silently. A team that declares an empty `checkpoints` object turns the Caller's Global Checkpoints off for that repository.
