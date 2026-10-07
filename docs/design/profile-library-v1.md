# Filesystem-backed Review Profiles V1

Status: implemented by Configuration Hub Slice 4

## Decision

A Review Profile is a complete executable package owned by Global or Repository Configuration. The Configuration Manager loads, validates, resolves, and publishes the package. Callers do not assemble execution settings at run time.

Packaged `bugs`, `code-quality`, `documentation`, and `test-audit` content is available only as Review Profile Templates. A Template contains judgment instructions and a stable revision. It has no Reviewer, model, reasoning effort, or Attempt deadline, so it cannot execute.

## Filesystem contract

Global Configuration:

```text
${XDG_CONFIG_HOME:-$HOME/.config}/review-party/
└── profiles/<name>/
    ├── profile.json
    └── instructions.md
```

Repository Configuration:

```text
<repository>/.reviewparty/
└── profiles/<name>/
    ├── profile.json
    └── instructions.md
```

`profile.json` uses this strict schema:

```json
{
  "schema_version": 1,
  "name": "code-quality",
  "reviewer": "opencode",
  "model": "meta/muse-spark-1.2-contributor",
  "reasoning_effort": "high",
  "attempt_deadline": "3m",
  "template_id": "code-quality",
  "template_revision": "code-quality-v1"
}
```

Every executable Profile requires a known Reviewer, non-empty model, non-empty reasoning effort, positive Attempt deadline no greater than 24 hours, and non-empty `instructions.md`. Template ID and revision must appear together. The directory name and metadata name must match.

The old `profiles/<name>.md` representation has no reader. Unknown metadata fields fail strict decoding.

## Resolution and provenance

Global and Repository Profiles may share a name. An unqualified lookup checks Repository before Global. Qualified references select one exact scope. Resolved Profiles retain their scope, source path, Template provenance, and a revision derived from metadata plus the exact instruction bytes.

A Template never participates in Profile lookup. Profile Creation copies Template instructions into `instructions.md`; later Template changes do not alter the saved Profile.

## Skill Templates

An Agent Skills directory on the Caller's machine is also a Template source. Review Party reads `$HOME/.agents/skills` and then `$HOME/.claude/skills`. The first root wins on a duplicate skill name. Missing roots are skipped, and symlinked skill directories are followed.

Each directory with a regular `SKILL.md` becomes Template `skill:<directory name>`. Directories whose name starts with `.` are skipped. So is a `SKILL.md` that is unreadable, larger than the document limit, or empty after its frontmatter. Discovery never blocks opening configuration.

Instructions are a fixed preamble followed by the `SKILL.md` body with its leading YAML frontmatter removed. The preamble tells the Reviewer it is read-only, confined to the repository, and must report through the result block. It also says to mark evidence the skill requires but the Reviewer could not gather as unverified. Instructions never contain the skill's filesystem path, because repository-scoped Profiles may be committed.

The revision is `sha256-` plus the first 12 hex characters of the instructions digest. Editing a skill therefore surfaces as Template drift, and `config profile update-template` refreshes the Profile. Deleting a skill surfaces as an unavailable Template source; the Profile keeps running its saved instructions.

Reviewers cannot read files bundled beside `SKILL.md`. Creation and Template update plans warn with the count and the first five paths. Files under `agents/` are Codex UI metadata, and dot entries are tooling state. Neither is counted.

Skills are never read from the reviewed repository. A Subject author must not be able to write their own Reviewer's instructions.

## Publication

Profile Creation and copy produce reviewed, snapshot-bound plans. The agent-facing commands are `config profile create` and `config profile copy`. Publication writes `profile.json` and `instructions.md` as one rollback-protected unit. A failure cannot leave new metadata paired with absent or stale instructions. Existing Profiles are never overwritten by creation or copy.
The same Configuration Manager instance must validate and publish a Plan; a
Plan cannot be published through a different Manager instance.

Opening configuration, listing Templates, and resolving absent Profiles create no files or directories.

## Discovery and onboarding

Profile editors may present cached, configured, and packaged model choices
immediately, then refresh one Reviewer-specific observation in the background.
The discovery boundary is bounded and observational: it does not write
Review Party-owned configuration, persist credentials, or launch
authentication. An external harness may read already configured, allowlisted
credentials for its read-only provider query and write its own state under its
normal HOME/XDG/CODEX_HOME locations. Successful
observations are disposable cache material and are not proof that a later paid
Review will succeed.

Manual model IDs remain valid when discovery is unavailable, incomplete, or
unsupported. The CLI reports a warning when the exact ID is not immediately
known, and the reviewed Plan still requires normal TTY confirmation or explicit
`--yes` authorization before publication. Authentication results expose only
status, diagnostics, and a documented sign-in action for the Caller to choose.

The typed `ProfileOnboarding` flow holds Template or blank instructions and the
complete Profile draft in memory, validates through `PlanProfileCreation`, and
publishes only after the reviewed Plan is confirmed. Cancellation retains the
draft without changing either Profile file; explicit discard clears it.

## Non-goals

- Executable packaged Profiles.
- Ordinary Reviewer, model, effort, deadline, or instruction overrides.
- Profile inheritance or variants.
- Compatibility readers for Markdown Profiles.
- Profile rename or deletion in V1.
