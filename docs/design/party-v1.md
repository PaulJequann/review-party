# Parties and repository review selection V1

Status: implemented by Configuration Hub Slice 4

## Party definition

A Party is a named ordered list of scoped Review Profile references with one Concurrency Limit. It is flat. Parties cannot contain Parties, inherit another Party, or override Profile execution settings.

```json
{
  "schema_version": 1,
  "name": "release-gate",
  "description": "Pre-delivery review set",
  "concurrency_limit": 2,
  "profiles": [
    {"scope": "global", "profile": "bugs"},
    {"scope": "repository", "profile": "supabase-rls"}
  ]
}
```

Global Parties may reference only Global Profiles. Repository Parties may reference Global and Repository Profiles. Every reference carries an explicit scope, so same-named Profiles remain distinct.

Party files live at `parties/<name>.json` under the Global or Repository Configuration root. Strict decoding rejects `extends`, nested Party references, and member Reviewer, model, effort, or deadline fields. The file name and `name` field must match. Concurrency must be positive and the member list must not be empty.

## Repository review selection

Repository Configuration owns the complete default roll-up:

```json
{
  "schema_version": 1,
  "reviews": {
    "concurrency_limit": 3,
    "global": [
      {"party": "baseline"},
      {"profile": "documentation"}
    ],
    "repository": [
      {"profile": "supabase-rls"}
    ]
  }
}
```

Each item selects exactly one Profile or Party. Global selections precede Repository selections, and order within each array is preserved. Merely creating a Global Profile or Party enables nothing.

Selection editing uses typed add, remove, move, and concurrency operations through the Configuration Manager. Callers do not edit dotted JSON paths.

## Resolution contract for Slice 5

Slice 5 will expand Parties in member order and deduplicate exact scoped Profile identities at first occurrence. `global:code-quality` and `repository:code-quality` are different identities and must both remain selected, with a warning for the Caller.

Slice 5 must also fail preflight on a missing reference, incomplete Profile, unavailable saved Reviewer, or rejected model before an Agent Harness launches. Review Bundles will retain the authored selection, expanded scoped identities, Profile Revisions, deduplication facts, warnings, and provenance.

Slice 4 establishes the accepted storage and domain contract. The transitional execution commands are replaced in Slice 5.

## Non-goals

- Party `extends` or nesting.
- Inherited concurrency.
- Per-member execution settings.
- Packaged executable Parties.
- Automatic Global baseline selection.
- Party rename or deletion in V1.
