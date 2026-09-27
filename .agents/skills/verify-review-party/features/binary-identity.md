# Binary identity

Review Party exposes build provenance through its `version` command, while doctor binds the installed executable to the current verification run.

## Sub-features

- `BIN-IDENTITY`
- `BIN-REVISION-GUARD`

## Source evidence

- `newVersionCommand` and `writeVersion` in `cmd/review-party/version.go`
  expose human and JSON provenance through `review-party version`.
- `doctor` and `source_tree` in
  `.agents/skills/verify-review-party/scripts/verify.sh` bind the run to the
  recorded source revision, working-tree fingerprint, binary digest, binary
  identity, and run-owned resources.

Drift: none. The live recipe below exercises both version formats and the
revision guard.

## How to get to it (user POV)

- Run `review-party version` for human output.
- Run `review-party version --format json` for structured output.

## Driving it with the CLI

Preconditions: Start from a fresh launched baseline.

- **Check readiness.** Run `.agents/skills/verify-review-party/scripts/verify.sh doctor "$run_dir"`. It prints `doctor: ready` and exits `0`.
- **Read human identity.** Run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" binary-identity/version-human.txt -- review-party version`. Stdout begins with `Review Party` and the capture records exit code `0`.
- **Read structured provenance.** Run `.agents/skills/verify-review-party/scripts/verify.sh capture "$run_dir" binary-identity/version-json.txt -- review-party version --format json`. Stdout is one JSON object and the capture records exit code `0`.

Negative control: Run `mkdir -p "$run_dir/evidence/binary-identity"`, then run `VERIFY_REVIEW_PARTY_EXPECTED_REVISION=deliberately-wrong .agents/skills/verify-review-party/scripts/verify.sh doctor "$run_dir" >"$run_dir/evidence/binary-identity/negative-doctor.stdout" 2>"$run_dir/evidence/binary-identity/negative-doctor.stderr"`. Record `$?` in `negative-doctor.exit`. The revision check must exit nonzero and stderr must contain `source revision mismatch`. Restore the baseline with `unset VERIFY_REVIEW_PARTY_EXPECTED_REVISION`, then rerun doctor and require `doctor: ready`.

## Gotchas

- Development builds may emit an empty JSON provenance object. The human command still names Review Party, and doctor uses the recorded executable digest, Git revision, and working-tree fingerprint for the run binding.
- A dirty checkout is allowed. The manifest's `source_dirty` entry says whether the binary includes uncommitted changes; report it with the evidence. Any source edit after launch makes doctor fail with `source tree mismatch`.
- Running a `review-party` found elsewhere on `PATH` invalidates the proof. `capture` always selects the owned binary.
