-- Kit Maker documents (internal/lite): blocks, blueprints and revisions,
-- each a JSON body. Starter content ships in starter.json; a row here
-- replaces the starter document with the same kind and id.
CREATE TABLE docs (
  kind TEXT NOT NULL,            -- block | blueprint | revision
  id TEXT NOT NULL,
  parent TEXT,                   -- a revision's blueprint
  rev INTEGER NOT NULL,          -- bumped on every write (optimistic locking)
  body TEXT NOT NULL,
  updated_at TEXT NOT NULL,      -- ISO time, for "changed since"
  updated_by TEXT,
  PRIMARY KEY (kind, id)
);
CREATE INDEX docs_updated ON docs (updated_at);
CREATE INDEX docs_parent ON docs (kind, parent);

-- A write whose expected rev is stale inserts a 1 here, which fails the
-- CHECK and rolls back the whole batch (D1 batches are transactions).
CREATE TABLE rev_guard (ok INTEGER NOT NULL CHECK (ok = 0));

-- Failed sign-ins, for the lockout (5 in 15 minutes, as the server edition).
CREATE TABLE login_failures (
  email TEXT NOT NULL,
  at TEXT NOT NULL
);
CREATE INDEX login_failures_email ON login_failures (email, at);
