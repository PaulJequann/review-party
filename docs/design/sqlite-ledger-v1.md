# SQLite ledger v1

Status: implemented; state preparation amended by PR #11 on 2026-08-24.

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
preparation, schema validation, and history. Its private `reviewRecordProjection`
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

`review-party init` is the only ordinary command that prepares the ledger. It
validates the selected repository and prepares the default managed root. An explicit advanced `--state-dir` choice is stored once in the normal
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

Disk-full, corruption, and schema errors are returned to the caller with no
fallback store. SQLite's atomic commit protects a prior committed aggregate.
Review Party never deletes, silently repairs, or downgrades state.

## State preparation and schema

PR #11 replaced the eight-step pre-release migration chain with one embedded
`initial.sql` schema. New ledgers contain the complete Review, history, replay,
Eval, adjudication, retry-delay, and Review Bundle tables and indexes. Review
Party does not import or upgrade retired local schemas.

Initialization creates the schema transactionally. It is idempotent for a
ledger that already matches the current schema. A collision with an unrelated
or retired table fails before Review Party claims the state is prepared. The
caller must select a fresh state root and run `review-party init`; Review Party
does not rename, delete, repair, or reinterpret the old database.

Review and read-only operations require prepared state and never create a
missing ledger. Corrupt, inaccessible, or incompatible state produces a
specific error without a JSON fallback. This direct replacement is deliberate
while Review Party remains pre-release.
