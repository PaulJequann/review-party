# Skill Templates

Review Party imports each skill in the caller's `~/.agents/skills` and
`~/.claude/skills` as a Review Profile Template named `skill:NAME`. A Profile
created from one records the skill's revision, reports drift when the skill
changes, and reports an unavailable source when the skill is removed while its
saved instructions keep running. A skill whose `SKILL.md` cannot become a
Template is reported as skipped.

## Sub-features

- `SKILL-TEMPLATE-CREATE`
- `SKILL-TEMPLATE-DRIFT-UPDATE`
- `SKILL-TEMPLATE-SOURCE-UNAVAILABLE`
- `SKILL-TEMPLATE-SKIPPED`
- `SKILL-TEMPLATE-HUB`

## Source evidence

- `callerHomeSkillRoots` in `internal/engine/config.go` resolves the two skill roots
  from HOME, never from the reviewed repository.
- `SkillTemplates`, `loadSkillTemplate`, `skillInstructions`, and
  `skillBundledFiles` in `internal/configuration/skill_template.go` strip the
  frontmatter, frame the body with the read-only preamble, derive the
  `sha256-` revision, and list bundled files while skipping `SKILL.md`,
  `agents/`, and dot entries. `SkillTemplates` returns a `SkillTemplateSet`
  whose `Skipped` entries name each unreadable, oversized, or empty
  `SKILL.md`.
- `unknownTemplateError` in `internal/configuration/template_lookup.go`
  names the skip reason when a create requests a skipped skill.
- `TemplateDriftForProfiles` in `internal/configuration/domain_storage.go`
  reports drift with `status: update_available`, or `source_unavailable`
  when a recorded Template is gone.
- `doctorTemplateDriftLine` in `cmd/review-party/doctor.go` renders both drift
  lines, and `executeDoctor` lists skipped Templates in text and in JSON
  `skipped_templates`.
- `appendTemplateDrift`, `templateDriftMessage`, and `templateDriftDetail` in
  `internal/configurationhub/snapshot.go` render the Hub Overview warning and
  the Profiles detail suffix, and `buildSnapshot` there adds one Overview
  warning per skipped Template; `profileTemplateOptions` in
  `internal/configurationhub/interactive_forms.go` lists the Template picker.

Drift: none.

## How to get to it (user POV)

- Put a skill at `~/.agents/skills/NAME/SKILL.md`.
- Run `review-party config profile create PROFILE --template skill:NAME ...`,
  or choose `skill:NAME` in the Configuration Hub Template picker.
- Run `review-party doctor` after editing or removing the skill.

## Driving it with the CLI

Preconditions: a fresh launched baseline and a passing doctor. Every command
below writes the fixture skill under the run-owned HOME, never the operator's.
Set up the shell once:

```sh
verify=.agents/skills/verify-review-party/scripts/verify.sh
home=$("$verify" path "$run_dir" home)
: "${home:?owned home is required}"
config_file=$("$verify" path "$run_dir" config_file)
target_repository=$("$verify" path "$run_dir" target_repository)
skill="$home/.agents/skills/verify-audit"
mkdir -p "$skill/references" "$skill/.cache"
printf -- '---\nname: verify-audit\ndescription: frontmatter must not reach the Reviewer\n---\n\nFlag tests that assert nothing.\n' > "$skill/SKILL.md"
printf 'rubric\n' > "$skill/references/rubric.md"
printf '{}\n' > "$skill/.cache/state.json"
```

### Negative control

```sh
set +e
"$verify" capture "$run_dir" skill-templates/negative-unknown.txt -- review-party config profile create audit --template skill:nope --reviewer codex --model gpt-5.6-luna --effort high --deadline 8m --yes --repo "$target_repository" --config "$config_file"
set -e
```

Require exit 1 and `unknown Review Profile Template "skill:nope"` on stderr.
No `audit` Profile may exist afterward. Run doctor.

### Skipped skill

```sh
empty="$home/.agents/skills/verify-empty"
mkdir -p "$empty"
printf -- '---\nname: verify-empty\n---\n' > "$empty/SKILL.md"
"$verify" capture "$run_dir" skill-templates/skipped.txt -- review-party doctor --repo "$target_repository" --config "$config_file"
set +e
"$verify" capture "$run_dir" skill-templates/negative-skipped.txt -- review-party config profile create audit --template skill:verify-empty --reviewer codex --model gpt-5.6-luna --effort high --deadline 8m --yes --repo "$target_repository" --config "$config_file"
set -e
rm -rf "$empty"
```

Require `skipped.txt` to exit 0 and print `Template skipped:
skill:verify-empty: .../verify-empty/SKILL.md has no instructions after its
frontmatter`. Require `negative-skipped.txt` to exit 1 with
`Review Profile Template "skill:verify-empty" was skipped:` on stderr, and no
`audit` Profile may exist afterward. Run doctor.

### Create, drift, update, unavailable

```sh
"$verify" capture "$run_dir" skill-templates/create.txt -- review-party config profile create audit --template skill:verify-audit --reviewer codex --model gpt-5.6-luna --effort high --deadline 8m --yes --repo "$target_repository" --config "$config_file"
"$verify" capture "$run_dir" skill-templates/readback.json.txt -- review-party explain audit --repo "$target_repository" --config "$config_file" --format json
printf -- '---\nname: verify-audit\n---\n\nFlag tests that assert nothing or only mocks.\n' > "$skill/SKILL.md"
"$verify" capture "$run_dir" skill-templates/drift.json.txt -- review-party doctor --repo "$target_repository" --config "$config_file" --format json
"$verify" capture "$run_dir" skill-templates/update.txt -- review-party config profile update-template audit --yes --repo "$target_repository" --config "$config_file"
"$verify" capture "$run_dir" skill-templates/after-update.json.txt -- review-party doctor --repo "$target_repository" --config "$config_file" --format json
rm -rf "$skill"
"$verify" capture "$run_dir" skill-templates/unavailable.txt -- review-party doctor --repo "$target_repository" --config "$config_file"
"$verify" capture "$run_dir" skill-templates/unavailable.json.txt -- review-party doctor --repo "$target_repository" --config "$config_file" --format json
```

Require, from each transcript's `.stdout` and `.stderr`:

1. `create.txt` exits 0, its plan shows
   `template=skill:verify-audit@sha256-`, and stdout carries exactly one
   `warning:` line naming `references/rubric.md` and not `.cache/state.json`.
2. `readback.json.txt` `instructions` starts with
   ``This Profile was imported from the `verify-audit` skill.``, ends with
   `Flag tests that assert nothing.`, and omits `frontmatter must not reach`.
3. `drift.json.txt` has one `template_drift` entry for `audit` with
   `status: update_available`, a `template_revision` equal to the create
   revision, a different `available_revision`, and `customized: false`.
4. `update.txt` publishes the new revision, and `after-update.json.txt` has
   `template_drift: []` and `skipped_templates: []`.
5. `unavailable.txt` exits 0 and prints
   `Template source unavailable: global:audit skill:verify-audit@sha256-...;
   saved instructions still run`. `unavailable.json.txt` has `valid: true`
   and `status: source_unavailable` with no `available_revision`.

## Driving it in the Configuration Hub

Use a fresh launched baseline. Seed the same fixture skill with the setup
block above, then start the Hub with the terminal procedure in
`features/README.md`. Captures go to `$evidence/skill-templates/NAME.txt`.

1. **Template picker.** Open Profiles, send `n`, enter `hub-audit` as the
   name, choose `codex`, the packaged `gpt-5.6-luna`, `high`, and `8m`, then
   accept the `Template` instruction source. Capture `hub-template-picker.txt`;
   it must list `skill:verify-audit (Review Profile Template sha256-...)`
   after the packaged Templates.
2. **Plan preview.** Move to `skill:verify-audit`, send Enter, and accept the
   default editor answer. Wait for `Plan preview` and capture
   `hub-plan-preview.txt`. It must show
   `▲ Template skill:verify-audit bundles 1 file the Reviewer cannot read:
   references/rubric.md` and `template=skill:verify-audit@sha256-`.
3. **Publish and exit.** Send `p`, wait for the Profiles inventory to list
   `[global] hub-audit`, then send `q` and require the session to exit.
4. **Unavailable source.** Run `rm -rf "$skill"` and start a fresh Hub
   session with the same command. Capture `hub-overview-unavailable.txt`; the
   Overview must warn `global Profile "hub-audit": Template
   skill:verify-audit source unavailable; saved instructions still run`. Send
   `Down`, then `Enter`, capture `hub-profiles-unavailable.txt`, and require
   `hub-audit` to carry `· Template source unavailable`. Send `q`.
5. **Skipped skill.** Seed the empty fixture from the CLI "Skipped skill"
   section without removing it:

   ```sh
   empty="$home/.agents/skills/verify-empty"
   mkdir -p "$empty"
   printf -- '---\nname: verify-empty\n---\n' > "$empty/SKILL.md"
   ```

   Start a fresh Hub session and capture `hub-overview-skipped.txt`. The
   Overview must warn `Template skill:verify-empty skipped:
   .../verify-empty/SKILL.md has no instructions after its frontmatter`.
   Send `q`, then run `rm -rf "$empty"`.

Read-only second view:

```sh
"$verify" capture "$run_dir" skill-templates/hub-readback.json.txt -- review-party explain hub-audit --repo "$target_repository" --config "$config_file" --format json
```

Require the readback to carry the imported preamble and `codex`,
`gpt-5.6-luna`, `high`, and `8m`. Run doctor before cleanup.

## Gotchas

- The fixture lives under the run-owned HOME. Keep the `:?` guard on `home`;
  an empty value would point `mkdir` and `rm -rf` at `/.agents`.
- Skills are read on each command, so edits need no relaunch. Editing a file
  in this repository does invalidate the run.
- The owned HOME hides Reviewer executables installed under the operator's
  HOME, so the Hub reports `codex` as unavailable and still offers the
  packaged model. That is expected here and proves nothing about the Reviewer.
- The Profiles detail suffix wraps in the 38-column list pane. Match the
  Detail pane, which keeps it on one line.
