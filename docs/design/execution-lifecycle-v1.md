# Execution lifecycle v1

Status: accepted 2026-10-07. Implemented in two steps. The first adds
`internal/hostrun` and its platform tests. The second moves every caller onto
it.

## Problem

Review Party runs on someone else's machine. Before this design it left
residue in three places:

- `git worktree add` registered a worktree inside the user's repository for
  every Attempt.
- Temporary files went straight into the host temp directory under unrelated
  names.
- Reviewer processes and their descendants could outlive a killed
  review-party process and keep running with their working directory inside
  a checkout.

The user's rule is that host hygiene ranks slightly above Review Party
functionality. That rule decides each tradeoff below.

## Requirements

- R1. Everything a process creates at runtime lives in one per-process run
  directory inside one runtime root under the host temp directory.
- R2. Execution views write nothing into the user's repository.
- R3. Each run has one execution view per Subject, and every member shares
  it. The view is built lazily, after the Eval concurrency gate admits the
  Attempt.
- R4. A run that ends normally leaves nothing of its own in the runtime root.
- R5. The next invocation of any command reaps a run that died. Reaping kills
  the run's Reviewers if they are still alive, then deletes its directory. PID
  reuse cannot fool liveness.
- R6. On Linux and Windows, Reviewers and their descendants cannot outlive
  the review-party process. On macOS the next reap kills them.
- R7. Reaping and cleanup never fail a command. A failure becomes a warning
  that names `review-party clean`.
- R8. Windows and macOS builds pass, and CI runs the platform code on its
  platform.
- R9. Every run directory describes itself. `footprint` can list each run's
  owner, start time, size, and whether it is live. `clean` can remove dead
  runs without guessing.

## On-disk contract

```
<host temp>/review-party-runtime-<uid>/   runtime root, 0700; Windows: review-party-runtime
  root.lock                               empty and permanent; serializes create, claim, final removal
  <run-id>/                               one run, 0700; run-id = <unix seconds base36>-<4 hex>
    lease                                 the owner holds an exclusive lock on it for its whole life
    owner.json                            {schema, id, owner:{pid,start,boot}, created, command, version, executable}
    t/                                    TMPDIR, TMP and TEMP for the owner and every child
    v/<n>/                                execution views, one per Subject key
    p/<sentinel-pid>.json                 {schema, sentinel:{pid,start,boot}, reviewer:{pid,start,boot}, group, started, program, mode}
```

Three facts are kept separate, and none is inferred from another:

| Fact | Source of truth |
|---|---|
| A run is alive | Its `lease` is locked. The kernel releases the lock when the owner dies. |
| A process is one particular instance | `(pid, start stamp, boot id)` |
| A dead run is being removed | The remover holds that run's `lease` lock |

Liveness never reads a PID. The lease is opened close-on-exec on Unix and
non-inheritable on Windows, so no child can keep a dead owner looking alive.
The runtime root name is distinct from any durable state path.

`root.lock` stays after the last run ends. Removing a lock file that another
process may be waiting on would let two holders lock different inodes. One
empty file is the cost of an atomic create and claim.

## Module

`internal/hostrun` owns where runtime files live, how long they live, and how
processes die. `subject` owns what an execution view contains. `engine`
decides when to ask for one.

| Surface | Hides |
|---|---|
| `Open(Options) (*Run, error)`, `(*Run).Close()` | Root validation, the root lock, claiming and background removal of dead runs, lease ordering, Windows retries, warning wording |
| `(*Run).TempDir() (string, error)`, `HostTempDir()` | Lazy run creation, the per-user root name, the TMPDIR, TMP and TEMP redirect |
| `(*Run).View(ctx, ViewSource) (string, error)` | Memoization by key, single-flight build, unmemoized failure, `v/<n>` naming |
| `(*Run).Start(*exec.Cmd) (*Process, error)`, `Process.Done/Wait/Stop` | Sentinel re-exec, control and status pipes, process groups, subreaper, Pdeathsig, Job Objects, the sweep after a normal exit, `p/` records |
| `WithRun`, `From`, `ErrNoRun`, `Init` | The context key and sentinel dispatch |
| `Inventory(root)`, `(*Run).Reap()`, `Report` | Lease probing and identity checks, for `footprint` and `clean` |

A `Run` reaches callers through the context. The context already carries the
Eval attempt gate on this path, so no constructor gains a parameter.
`From(ctx)` returns `ErrNoRun` when no run is present. Callers never fall back
to the host temp directory.

`ViewSource` has two methods, `ViewKey() string` and
`BuildView(ctx, dir string) error`. `subject.Execution` satisfies it
structurally, so `subject` never imports `hostrun`.

## Lifecycle

### Open

`Open` validates the root. It refuses a symlink, a foreign owner, or a mode
wider than 0700, and creates the root when it is missing. It then takes
`root.lock`, waiting at most 10 seconds. Under the lock it tries to lock each
run's `lease` without blocking. A successful try means the run is dead. The
claimer keeps that lease locked as the claim. A run directory with no `lease`
is also dead, because creation happens entirely under the root lock. `Open`
releases the root lock and returns. A background goroutine then removes the
claimed runs, and `Close` joins it.

`Open` does not create a run directory. `version`, `help`, and read-only
commands therefore leave nothing behind except the root and `root.lock`.

### Lazy run creation

The first `TempDir`, `View`, or `Start` creates the run under the root lock.
It makes the directory, locks `lease`, writes `owner.json` by rename, and
creates `t/`. It then sets TMPDIR, TMP, and TEMP for the whole process to
`<run>/t`. From that point `os.TempDir()` in any package, git, `$EDITOR`, and
code not yet written all land inside the run. `HostTempDir()` is captured in
`Init`, before any redirect, and is the only input to the root path.

Commands that launch Reviewers create the run before resolving the Subject.
This keeps the private index directory used by delta measurement inside the
run.

### Removing a claimed or closing run

Removal of a claimed run follows these steps:

1. Kill each `p/` record whose identity still matches. On Unix, signal the
   recorded group when no process has `pid == group`, or when that process's
   identity matches the recorded Reviewer. Linux never reuses a process group
   ID while a member lives, which was measured in a PID namespace, so a
   missing leader is safe. Skip every kill when the boot id differs.
2. Delete everything except `lease`. ENOENT counts as success.
3. Under the root lock, close `lease`, unlink it, and remove the directory.

The owner's `Close` runs the same steps on its own run. Before step 1 it
stops any live `Process` and joins the background removal. Step 3 runs under
the root lock, so no other reaper holds a probe handle on `lease` at that
moment. Without that guarantee, Windows would refuse the unlink, because Go
opens files without `FILE_SHARE_DELETE`.

Removal is idempotent. When a reaper dies partway through, the lease of the
run it was removing is released, and the next `Open` claims what remains.
Nothing is renamed, so a foreign open handle on Windows only delays the
removal and never breaks it.

### Warnings

`Options.Warn` receives one line per failure. Each line names the path and
`review-party clean`. `Close` returns nothing. When `Open` returns an error,
`execute` turns it into a warning, and any later `From` returns `ErrNoRun`.
Per-resource early cleanups ignore their own errors, because removing the run
directory is the authoritative cleanup and it warns when it fails. Warnings
go to stderr, never stdout, so `--json` stays clean.

## Reviewer processes

Every supervised command runs under a sentinel. The sentinel is the
review-party binary re-executed with `argv[1] == "--review-party-sentinel-v1"`.
`hostrun.Init()` is the first statement of `main` and of `TestMain` in every
package whose tests start supervised processes. It intercepts the token
before cobra parses arguments.

The owner talks to the sentinel over two pipes. The control pipe runs from
the owner to the sentinel. It carries the command spec, then terminate and
kill requests. EOF on the control pipe means the owner is gone. The status
pipe runs from the sentinel to the owner. It carries the Reviewer's identity
and group, or a start error. The owner waits at most 10 seconds for that
line, then kills the sentinel and `Start` fails. The Reviewer inherits stdin,
stdout, and stderr directly. The sentinel exits with the Reviewer's exit code, so existing
`exec.ExitError` handling works unchanged.

| Platform | Tree root | Owner dies by any means | Normal exit |
|---|---|---|---|
| Linux | The sentinel is a child subreaper with its OS thread locked for its whole life. The Reviewer gets `Setpgid` and `Pdeathsig: SIGKILL`. | EOF. The sentinel kills the group and every adopted orphan. | The sentinel sweeps the group and the orphans, then exits. |
| macOS | The Reviewer gets `Setpgid`. | EOF. The sentinel kills the group. | The sentinel kills the group. |
| Windows | The sentinel creates a Job with `KILL_ON_JOB_CLOSE`, joins it, then starts the Reviewer with `CREATE_NEW_PROCESS_GROUP`. | EOF, and the sentinel exits. Closing the job handle kills the tree. | The sentinel terminates every job member except itself. |

The owner starts each sentinel in its own process group on Unix, and with
`CREATE_NEW_PROCESS_GROUP` on Windows. A terminal interrupt therefore reaches
only review-party, which stops Reviewers through the control pipe. When a
nested job is refused, the sentinel records `mode: "job-degraded"` and falls
back to killing the console process group. `Close` warns once. The fallback
is named, not silent.

The sentinel's locked thread must never exit. A thread exit fires Pdeathsig,
measured on Linux 7.2.

Known gaps. On macOS a descendant that calls `setsid` leaves the group; the
sentinel cannot see it, and the reap cannot find it. On Linux, when the
sentinel itself is SIGKILLed while the owner lives, the owner kills the
recorded group as it reaps the sentinel, and only a descendant that called
`setsid` survives. A Windows Reviewer that allocates its own console misses
the `CTRL_BREAK` terminate phase, and the Job kill still ends it. On Unix
other than Linux and macOS, process identity is unavailable, so a reaper
kills nothing and only removes the dead run's files.

## Execution views

| Subject | View |
|---|---|
| Working changes | The repository itself, with nothing created |
| Captured change | A copy of the captured tree in `v/<n>`. Symlinks and `.git` are refused, as before. |
| Committed range and unreviewed delta | A git view in `v/<n>` |

The git view recipe was measured on git 2.56. It wrote nothing into the
source `.git`, and a global `required` smudge filter did not run.

```
git -C <repo> rev-parse --path-format=absolute --git-path objects   -> <objects>
git -C <repo> rev-parse --show-object-format                        -> <fmt>
git init -q --template= --object-format=<fmt> <dir>
write <dir>/.git/objects/info/alternates = <objects>
write <dir>/.git/info/attributes         = * -filter -ident -working-tree-encoding
git -C <dir> -c core.hooksPath=<os.DevNull> -c core.fsmonitor=false -c core.longpaths=true \
    checkout -q --force --detach <HeadObject>
env: GIT_LFS_SKIP_SMUDGE=1 GIT_TERMINAL_PROMPT=0
```

The recipe keeps the user's global config, so `safe.directory`,
`core.autocrlf`, and `core.longpaths` still apply. Reviewers see LFS pointer
files, which the user accepted. Unreachable delta commits check out by object
ID through the alternates file.

Every member's Subject value yields the same view key, so a bundle shares one
view. `executeAttempt` acquires the Eval gate first, then calls `View`. Views
last as long as the run, which removes every per-Attempt cleanup path.

## Rejected alternatives

- **Pdeathsig without a supervisor.** Pdeathsig signals only the direct
  child. A grandchild survived its parent's SIGKILL, measured. That fails R6
  on Linux.
- **A process-wide singleton instead of a context carrier.** It has the
  smallest caller surface. It also leaks a run between tests when one test
  forgets `Close`, and the context already carries the attempt gate.
- **`Config.Runspace` through `engine.New`.** It makes 27 constructor sites
  change for one value.
- **Claiming a dead run by renaming it into the claimer's directory.** On
  Windows any foreign open handle fails the rename. Nested claims also make
  `footprint` recurse.
- **A shared clone with global config disabled.** Setting
  `GIT_CONFIG_GLOBAL=/dev/null` drops `safe.directory`, `core.longpaths`, and
  `autocrlf`. The `info/attributes` override neutralizes filters without that
  cost.
- **An environment mark found by scanning `/proc` or `procargs2` at reap.** A
  descendant that rebuilds its environment carries no mark. A recorded group
  plus identity finds that descendant and needs no parser.
- **Durable state fallback under the host temp directory.** It is removed.
  Without `XDG_STATE_HOME`, a home directory, or a configured
  `state_directory`, Review Party reports that one is required. Durable data in temp storage disappears on reboot, and it
  shared a name with runtime files.

## Verification

`hostrun` tests re-execute the test binary as a helper owner or Reviewer.
`.github/workflows/platform.yml` runs them on `ubuntu-latest`,
`macos-latest`, and `windows-latest`. The required cases are:

- A normal `Open`, `View`, `Start`, and `Close` leaves only `root.lock`.
- A SIGKILLed owner's Reviewer tree dies within 2 seconds. On Linux this
  includes a `setsid` escapee. The next `Open` removes the run.
- A live run is never claimed. Two concurrent reapers remove one dead run
  without a warning.
- A fabricated record naming a live process with the wrong start stamp is
  never signalled.
- `Stop` escalates past a Reviewer that ignores terminate.
- A normal exit sweeps a background grandchild before `Wait` returns.
- On Windows, a foreign open handle becomes a warning, and the next `Open`
  removes the run.
- The git view leaves the source `.git` unchanged, runs no hook or filter,
  and checks out an unreferenced commit.
- Bundle members share one view, and the view waits for the Eval gate.
- `review-party version` reaps a planted dead run.

The real-binary check runs `review-party run`, sends `kill -9` partway
through, and expects no Reviewer process to remain. It then expects the next
command to leave only `root.lock`, and expects `git worktree list` and `.git`
to be unchanged.
