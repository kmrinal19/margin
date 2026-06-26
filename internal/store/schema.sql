-- margin comment store. PRAGMAs are applied per-connection via the DSN
-- (WAL, busy_timeout, synchronous=NORMAL, foreign_keys=ON, temp_store=MEMORY),
-- not here. foreign_keys must be ON per-connection or ON DELETE CASCADE no-ops.

CREATE TABLE IF NOT EXISTS doc (
  id            INTEGER PRIMARY KEY,
  slug          TEXT NOT NULL UNIQUE,      -- filename stem; docs/<slug>.md
  title         TEXT,
  last_rendered TEXT                       -- ISO8601, informational
);

CREATE TABLE IF NOT EXISTS comment_thread (
  id          INTEGER PRIMARY KEY,
  doc_id      INTEGER NOT NULL REFERENCES doc(id) ON DELETE CASCADE,
  status      TEXT NOT NULL DEFAULT 'open',   -- 'open' | 'resolved'
  orphaned    INTEGER NOT NULL DEFAULT 0,     -- 1 if last re-anchor failed
  created_at  TEXT NOT NULL,
  resolved_at TEXT,
  resolved_by TEXT
);
CREATE INDEX IF NOT EXISTS ix_thread_doc_status ON comment_thread(doc_id, status);

CREATE TABLE IF NOT EXISTS anchor (             -- 1:1 with thread
  thread_id       INTEGER PRIMARY KEY REFERENCES comment_thread(id) ON DELETE CASCADE,
  block_id        TEXT NOT NULL,                -- content-hash id of the START block
  end_block_id    TEXT,                         -- END block for a multi-block selection (else = block_id)
  quote_exact     TEXT NOT NULL,                -- single block: full quote; multi: head (in start block)
  quote_tail      TEXT,                         -- multi block: tail (in end block)
  quote_prefix    TEXT,                         -- up to 32 chars before the start
  quote_suffix    TEXT,                         -- up to 32 chars after the end
  char_start      INTEGER,                      -- offset in the start block
  char_end        INTEGER,                      -- offset in the END block
  last_confidence REAL                          -- 1.0 exact … lower = fuzzier
);

CREATE TABLE IF NOT EXISTS comment (
  id         INTEGER PRIMARY KEY,
  thread_id  INTEGER NOT NULL REFERENCES comment_thread(id) ON DELETE CASCADE,
  author     TEXT NOT NULL,                     -- 'human' | LAN name | 'ai'
  body       TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS ix_comment_thread ON comment(thread_id);
