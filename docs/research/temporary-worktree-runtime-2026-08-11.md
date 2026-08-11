# Temporary Worktree Runtime for Committed Reviews

**Date:** 2026-08-11  
**Status:** research — recommended input to Slice 15, not an accepted architecture decision  
**Question:** How should Review Party create, provision, secure, and clean up a temporary Git worktree when a Reviewer must inspect an exact committed head?

## Recommendation

Treat the worktree as a short-lived **source-view sandbox**, not as another developer checkout.

1. Create one detached worktree per Review attempt at the recorded full head OID.
2. Do **not** copy or symlink the caller's `node_modules`, `.env*`, ignored files, or other untracked state.
3. Do **not** run a package-manager install by default. Static repository read/search is the baseline capability; provisioning is a separate, explicit policy for a future “execute repository tools” capability.
4. If provisioning is explicitly enabled, detect and honor the committed package manager and lockfile, use its frozen/CI install mode, retain its normal user-level cache/store, and install into the temporary worktree. Never symlink another checkout's whole `node_modules`.
5. Pass only the environment needed by the selected Reviewer adapter. Do not discover or import project `.env` files. For Bun commands, use `--no-env-file` unless an explicit environment policy says otherwise.
6. On every terminal outcome—Complete, Incomplete, cancellation, timeout, launch failure, or panic recovery—stop the Reviewer process tree and remove the worktree. Persist the Review Record and frozen Subject facts, not the checkout.
7. On startup (and via a maintenance command), reconcile stale Review Party-owned worktrees after crashes, then prune only their stale Git administrative records.

This gives reproducible source visibility with bounded disk use. Package caches remain shared by the package manager, while each attempt's writable install layout remains isolated.

## Why dependency installation is not the baseline

The Slice 15 guarantee is that the prompt and repository tools observe the recorded head. File listing, reading, searching, blame, and history do not require application dependencies. Automatically installing dependencies would add network access, latency, disk writes, package lifecycle behavior, and a new failure mode unrelated to review quality.

Installation can also execute repository-selected behavior. Bun runs the project's own `preinstall`, `postinstall`, and `prepare` scripts and permits dependency lifecycle scripts through `trustedDependencies`; npm's install path likewise has script-control settings. Therefore “prepare this repository” is an execution capability, not harmless checkout setup. [Bun install and lifecycle scripts](https://bun.sh/docs/pm/cli/install), [npm `ignore-scripts` configuration](https://docs.npmjs.com/cli/v11/using-npm/config/#ignore-scripts)

Review Party should initially report that runtime dependencies are unavailable if a Reviewer tries to execute project tools. A later opt-in policy can provision them honestly and record the package manager, command, version, exit status, duration, and whether network access was allowed.

## Package-manager behavior and efficient choices

| Repository evidence | Explicit provisioning command | What is shared safely | Review Party decision |
|---|---|---|---|
| `bun.lock` / `bun.lockb` | `bun install --frozen-lockfile` with the repository's committed linker configuration | Bun's global download cache; on Linux the default backend hardlinks package files. Current Bun isolated installs can also use a global virtual store at `<cache>/links/` by default. | Good warm-install disk behavior without a cross-worktree `node_modules` symlink. Bun does not document a Git-worktree-specific mode; its cache, hardlink/clone backend, isolated linker, and global store are the relevant mechanisms. |
| `pnpm-lock.yaml` | `pnpm install --frozen-lockfile` | pnpm's content-addressable store; package files in `node_modules/.pnpm` are hardlinks to store content, with symlinks forming the dependency graph. | Let pnpm use its configured/default store. Do not invent a Review Party virtual-store path or link another checkout's modules. |
| `package-lock.json` / `npm-shrinkwrap.json` | `npm ci` | npm's user-level content-addressable download cache (`~/.npm` on POSIX by default). | Expect a fresh materialized `node_modules`; `npm ci` removes any existing tree first and does not modify manifests or lockfiles. This is less disk-efficient than Bun/pnpm but still cache-efficient. |
| No recognized committed lockfile | none | none | Fail provisioning as unsupported rather than resolving a new dependency graph or writing a lockfile. Static review still proceeds. |

Sources: [Bun global cache](https://bun.sh/docs/pm/global-cache), [Bun install strategies and platform backends](https://bun.sh/docs/pm/cli/install), [Bun isolated installs](https://bun.sh/docs/pm/isolated-installs), [Bun `install.globalStore`](https://bun.sh/docs/runtime/bunfig#install-globalstore), [pnpm symlinked `node_modules`](https://pnpm.io/symlinked-node-modules-structure), [pnpm frozen/offline install modes](https://pnpm.io/cli/install), [pnpm store settings](https://pnpm.io/settings/store), [npm cache](https://docs.npmjs.com/cli/v11/commands/npm-cache/), [npm `ci`](https://docs.npmjs.com/cli/v11/commands/npm-ci/).

### Why not symlink the caller's `node_modules`?

A whole-tree symlink is fast but semantically unsafe:

- it may represent a different lockfile, platform, architecture, runtime, linker mode, or lifecycle-script result than the committed head;
- tools can write caches, generated files, native rebuilds, or metadata through the link, contaminating the caller's checkout and concurrent Reviews;
- workspace links may point back into the caller's current source rather than the recorded head, violating the central Slice 15 guarantee;
- deleting or reinstalling from either checkout can break the other.

Package-manager-owned immutable/content-addressed sharing is the useful optimization. Review Party should not build its own dependency-sharing layer.

## `.env` and process environment policy

An ordinary linked worktree checks out committed files. Untracked or ignored `.env` files from the caller's checkout are absent, which is the correct default: copying or symlinking them would silently grant repository code and the Reviewer access to secrets and would make replay depend on mutable local state.

There are two distinct channels to control:

1. **Files:** never copy, symlink, search for, or synthesize `.env`, `.env.local`, ignored credentials, package registry auth files, or other untracked project state. A committed `.env` is part of the recorded Subject and cannot be hidden without making the source view inaccurate, but Review Party should redact secret-looking values from its own logs and artifacts.
2. **Inherited process environment:** launch the Reviewer with an adapter-owned allowlist sufficient for the selected tool (for example `PATH`, home/config locations it genuinely needs, terminal/locale settings, and that Reviewer's credential variables), rather than blindly inheriting the entire caller environment. Record variable **names/policy**, never values.

Bun requires special care: it automatically loads `.env`, mode-specific `.env.*`, and `.env.local` from the working directory, in increasing precedence. It provides `--no-env-file` to disable that behavior and `--env-file` for an explicit file. Review Party-managed Bun execution should default to `--no-env-file`; explicit secret/environment injection should happen through the child environment or a separately authorized file contract. [Bun environment loading](https://bun.sh/docs/runtime/environment-variables#setting-environment-variables), [Bun `--env-file` and `--no-env-file`](https://bun.sh/docs/runtime/environment-variables#manually-specifying-env-files), [Node.js environment and dotenv specification](https://nodejs.org/api/environment_variables.html).

Registry authentication is a separate provisioning concern. pnpm explicitly treats authorization configuration separately and warns that expanding environment variables in repository-controlled registry URLs can leak secrets to an attacker-controlled registry. Review Party should not provision private dependencies until a policy defines trusted registries and credential exposure. [pnpm configuration security warning](https://pnpm.io/settings#settings-pnpm-workspaceyaml)

## Worktree lifecycle

Git explicitly recommends detached worktrees for throwaway experimental/testing use and says to remove a linked worktree with `git worktree remove` when finished. `remove` normally requires a clean worktree; `--force` permits removal after generated or untracked files. `prune` removes administrative records for already-missing worktree directories; it is recovery, not the normal destructor. `lock` protects long-lived worktrees on intermittently mounted storage from pruning and also prevents ordinary removal, so it is not appropriate for these short-lived local worktrees. [Git `worktree` documentation](https://git-scm.com/docs/git-worktree.html)

Recommended state machine:

```text
Allocated -> Added(detached at head OID) -> Running -> Removing -> Removed
     |              |                         |           |
     +--------------+---- failure/cancel -----+-----------+
                                      persisted Review is Incomplete
```

Operational rules:

- Allocate beneath a Review Party-owned runtime/cache root, with a unique Review/Attempt identifier and an ownership metadata file outside the checked-out tree.
- Add with `git worktree add --detach <path> <full-head-oid>`. Do not create a branch.
- Never reuse a writable worktree concurrently. A Reviewer may create files even when Review Party asks it only to inspect.
- Before removal, terminate and reap the complete Reviewer process group. Then verify that the target is both inside the owned root and registered to this repository/attempt.
- Use `git worktree remove --force <exact-path>` for the owned temporary tree because generated/untracked files are expected. Do not recursively delete an unresolved path.
- If normal removal fails, retain enough non-secret diagnostic metadata to retry and mark cleanup pending; do not call the Review clean or lose its substantive result solely because cleanup failed.
- At startup, enumerate with the stable `git worktree list --porcelain -z` format, correlate only Review Party-owned entries with inactive attempts, remove stale owned trees, and finally run a bounded `git worktree prune` for missing owned entries. Never remove or prune a user's unrelated worktree by path guesswork.
- A separate `review-party maintenance worktrees` (name provisional) should support inspect/dry-run and cleanup. Age alone is insufficient while an attempt is live; use an attempt lease/PID plus ledger state, with age as crash-recovery evidence.

## Decision matrix

| Question | Slice 15 default | Possible later opt-in |
|---|---|---|
| Exact source at head | Detached temporary worktree | Same |
| Dependencies | None | Locked install using detected committed package manager |
| Shared disk optimization | Git objects only | Package-manager-owned cache/store/hardlinks |
| Reuse caller `node_modules` | Never | Never |
| Project `.env` files | No untracked files copied; no secret discovery | Explicit named environment contract, separately authorized |
| Child environment | Reviewer-adapter allowlist | Declared additions, names/provenance recorded, values never persisted |
| Bun automatic dotenv | Disable for managed Bun execution | Explicit environment contract only |
| Network during review | Reviewer transport only as required | Separately recorded package provisioning permission |
| Cleanup | Immediate on every terminal path | Same, plus startup/maintenance reconciliation |
| Pooling/reuse | No | Consider only after measuring checkout cost and proving reset/isolation semantics |

## Consequence for the implementation plan

Slice 15 should promise **exact committed repository visibility and a complete worktree lifecycle**, not “a fully runnable clone.” Its acceptance criteria should include dependency and secret non-copying, child-environment policy, cleanup on success/failure/timeout/cancellation, and crash recovery. Dependency provisioning should be deferred into its own capability and provenance contract unless a currently supported Reviewer demonstrably requires it.

