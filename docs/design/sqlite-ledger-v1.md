# SQLite ledger v1

Status: accepted for local implementation on 2026-08-10.

## Decision

The local ledger uses `modernc.org/sqlite` v1.56.0 through `database/sql`.
It is a pure-Go SQLite driver, so Review Party retains its supported Go
cross-compilation path and does not require a C compiler or an external
`sqlite3` executable. The CGO alternative, `github.com/mattn/go-sqlite3`, is
well maintained but requires CGO and a C toolchain on supported build targets;
that operational requirement is not justified for this local CLI. The selected
driver is maintained separately from Review Party and brings its SQLite
translation dependencies; the module graph is recorded in `go.mod`/`go.sum`.

Sources: [modernc sqlite package](https://pkg.go.dev/modernc.org/sqlite),
[driver DSN documentation](https://pkg.go.dev/modernc.org/sqlite#Driver), and
[mattn/go-sqlite3 compilation documentation](https://github.com/mattn/go-sqlite3).

## Interface and layout

`internal/store.LedgerRecordStore` owns connection lifecycle, state
preparation, migrations, and history. Its private `reviewRecordProjection`
module owns the complete Review Record-to-SQL mapping, transactional aggregate
replacement, hydration traversal, child ordering, and nullable-value rules.
The Conductor continues to
use `Save`, `Load`, and a small `History` query; neither it nor the CLI sees
rows or transactions. Construction is deferred so `profiles` and other
read-only commands do not create state.

`HistoryQuery` is the typed operational-query boundary. It filters the stored
Subject repository/identity, effective Profile/Reviewer, lifecycle,
termination category, and creation time without consulting current user
configuration. Results are deterministic and bounded, and `HistoryPage`
reports the applied limit and whether another page exists. This is selection
and summarization only; replay remains a separate Conductor behavior.

Review Party owns one state root at `$XDG_STATE_HOME/review-party`, falling
back to `$HOME/.local/state/review-party` when `XDG_STATE_HOME` is unset. The
ledger is `ledger.sqlite` and filesystem artifacts are rooted beneath
`artifacts/` in that same directory. The CLI does not expose a storage-path
override; tests and dogfood isolate state through `XDG_STATE_HOME`.

`review-party init` is the only ordinary command that prepares or migrates the
ledger. It validates the selected repository and prepares the default managed
root. An explicit advanced `--state-dir` choice is stored once in the normal
XDG user configuration; Review, inspect, and history resolve that remembered
location but only open already prepared state. A conflicting later selection
fails without moving, replacing, or abandoning existing state.

## Projection and durability

The ledger has a `reviews` table for scalar Review facts and JSON values that
are value objects (Subject, Profile revision/snapshot, termination, runtime,
and timings). `passes`, `attempts`, `findings`, and `artifacts` are relational
children with foreign keys. Eval state is a second aggregate hierarchy:
`eval_suite_runs` owns ordered `eval_runs`, and `adjudication_revisions` stores
immutable decisions derived from their ordinary Review Records. There is no
serialized full Review or Eval aggregate in the database: public records are
reconstructed from these projections. Each Eval Run freezes its full Eval Case
Revision as `case_revision` JSON and carries an `updated_at` checkpoint time;
these are ledger facts distinct from Review prompts, native output, and other
large evidence that remain filesystem artifacts.

The projection module has no exported projection Interface. Review callers use
`LedgerRecordStore.Save` and `Load`. Eval callers use the stable
`CreateEvalSuiteRun`, `CheckpointEvalRun`, `TerminateEvalSuiteRun`, and Eval
Run/Suite Run load seams. SQL rows, transactions, traversal, and
pooled-connection requirements remain private.

Each Review `Save` uses one transaction: it replaces the Review aggregate
projection and commits only after every child is written. `CreateEvalSuiteRun`
atomically creates a Suite Run and all planned child Eval Runs;
`CheckpointEvalRun` atomically persists one child transition and its parent
counts, lifecycle, termination, and timestamp, updating the child's
`updated_at`. A stop before the first Running checkpoint uses the parent-only
`TerminateEvalSuiteRun` and deliberately leaves planned children Pending.
Foreign keys are enabled per connection for both hierarchies. The database enables WAL,
`synchronous=FULL`, and a five-second busy timeout. Concurrent readers and
bounded Eval Review workers share the connection pool; SQLite serializes their
short write transactions, and a writer that remains busy after the timeout
returns the SQLite error rather than being retried or silently redirected.

Disk-full, corruption, and migration errors are returned to the caller with no
fallback store. SQLite's atomic commit protects a prior committed aggregate.
Review Party never deletes, silently repairs, or downgrades state.

## State preparation and migrations

Migrations are versioned, embedded SQL files. Startup creates the migration
table and applies only the next known versions in a transaction. A database
newer than this binary fails explicitly and is never modified. Explicit
initialization of new or supported existing state is idempotent. Review and
read-only operations require an existing ledger and do not create a missing
state root. Corrupt or inaccessible
state produces a precise diagnostic without a JSON fallback or repair path.

Migration 2 adds the two indexes justified by the first operational queries:
one for deterministic newest-first traversal and one for effective Reviewer
filtering in that same order. Other filters remain unindexed until measured
fixtures demonstrate a useful access path rather than accumulating speculative
indexes.

Migration 3 adds nullable `replays_review_id` lineage with a foreign key to the
source Review and a source/time/ID index. The relationship is part of the public
aggregate projection rather than an event log; source and replay remain
independent durable Review Records.

Migration 4 adds Eval Suite Run and Eval Run relations. An Eval Run freezes its
Eval Case Revision; that migration initially required every child to point to
one ordinary Review Record. Migration 6 supersedes that constraint: Pending,
Running, and no-Review Incomplete children have no Review link, while terminal
successes and review-linked Incomplete children point to the ordinary Review
they produced. The parent retains the suite revision/digest, effective
Experiment Configuration, ordered Eval Run IDs, completion counts, and timing;
adjudication and scores remain separate.

Migration 7 adds `attempts.retry_after_ms`. A structured provider delay is
durable reliability evidence and may lengthen retry backoff up to the frozen
Retry Policy maximum; it is never an instruction to change Reviewer, model, or
transport.

Migration 5 adds immutable Adjudication Revisions. Each row transactionally
stores the human decision document and pure derived score under a unique suite
revision number. Publishing a correction inserts another row; it never updates
an earlier adjudication or its source Review Results.

Migration 6 adds explicit `lifecycle` and `termination` values to Eval Suite
Runs, then rebuilds `eval_runs` so `review_id` is nullable and every child has
an `updated_at` checkpoint timestamp. Existing child execution and
adjudication states are preserved, and their initial `updated_at` is copied
from `created_at`. Existing parent rows with `completed_at` are normalized to
`completed`; parents without it become `incomplete`, because the older schema
cannot prove that an interrupted suite is still pending or running. New writes
use `CreateEvalSuiteRun` and `CheckpointEvalRun` so child state and parent
progress cannot commit independently.
