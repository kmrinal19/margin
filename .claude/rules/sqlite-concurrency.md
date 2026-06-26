---
paths:
  - "internal/store/**/*.go"
---

# SQLite concurrency (the load-bearing design)

margin is hit by **multiple concurrent AI sessions + the human browser**. SQLite
allows exactly one writer. The pattern below makes "database is locked" impossible
and keeps reads parallel. (Verified: River, kerkour.com, berthub.eu, 2026 benchmarks.)

## Two pools on one file
- Open **two** `*sql.DB` handles to the same `data/margin.db`:
  - **Writer pool:** `db.SetMaxOpenConns(1)` + `&_txlock=immediate`. Route **every** INSERT/UPDATE/DELETE and every transaction here. Writes serialize fairly in Go's queue (no `SQLITE_BUSY`) instead of at SQLite's lock layer.
  - **Reader pool:** 4–8 conns. Route **every** SELECT here.
- Both pools: `SetMaxIdleConns == MaxOpenConns`, `SetConnMaxLifetime(0)`, `SetConnMaxIdleTime(0)` (local single-file DB — keep conns warm; reopening re-runs PRAGMAs).

## DSN pragmas (per-connection)
```
file:<path>?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)&_pragma=temp_store(MEMORY)
```
Add `&_txlock=immediate` on the **writer DSN only**.
- `foreign_keys(ON)` is **REQUIRED per-connection** or the schema's `ON DELETE CASCADE` silently no-ops.
- `synchronous(NORMAL)` is corruption-safe under WAL and far faster than FULL.
- `_txlock=immediate` makes `BeginTx` start with `BEGIN IMMEDIATE`, so a read-then-upgrade write can't hit an instant `SQLITE_BUSY`.

## Transaction discipline
- **Keep write transactions to single-digit milliseconds.** NEVER hold the single
  writer conn across a network read, a goldmark render, or the re-anchor cascade —
  doing so starves every other AI session and the browser.
- Always `defer rows.Close()` on every `sql.Rows`. An unclosed `Rows` pins a pooled
  conn; with the 1-conn writer that is a total write outage.

## Lifecycle
- Driver: `modernc.org/sqlite` (pure-Go), `database/sql` driver name `"sqlite"`.
- On graceful shutdown: run `PRAGMA wal_checkpoint(TRUNCATE)` (or `PRAGMA optimize`),
  then `Close()` both pools (writer, then reader) after `srv.Shutdown` returns.
