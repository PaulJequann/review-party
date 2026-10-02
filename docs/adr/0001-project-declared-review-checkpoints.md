# Review Checkpoints are declared by the project, not decided by Review Party

Review Party's own definition rejects being a delivery gate, yet `review-party init` installs Checkpoint Integrations that can refuse a commit or push. We reconcile the two by splitting ownership. The repository declares which Review Checkpoints exist, what each requires, and the team floor of Integrations in its committed Repository Configuration; each Caller may add Integrations of their own. Review Party only reports Coverage facts and runs the Integrations it was asked to install. A blocking Integration refuses because the project declared that requirement, not because Review Party judged the change.

## Considered Options

- Advisory instructions only. Rejected because some teams want hard gates and an agent can ignore an instruction.
- Hooks that run Reviews. Rejected because Reviews take minutes and need Caller judgment over Findings, so a hook can only check that judgment already happened.
- Individual-only enforcement. Rejected because a shared repository could not rely on any teammate's setup.

## Consequences

No hook stops a determined bypass (`--no-verify`, editing agent settings). A team floor therefore means `init` and `doctor` report a missing Integration, not that removal is prevented.
