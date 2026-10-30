-- Published blueprint versions. Publishing runs on GitHub Actions
-- (.github/workflows/publish-kit.yml): a row starts pending and the
-- workflow reports back published (with the image digest) or failed.
CREATE TABLE blueprint_versions (
  blueprint_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  slug TEXT NOT NULL,
  kind TEXT NOT NULL,            -- workload | mixin
  name TEXT NOT NULL,
  repository TEXT NOT NULL,      -- ghcr.io/<owner>/sbx-kit-<slug>
  digest TEXT,
  yaml TEXT NOT NULL,            -- the descriptor, stamped version: "N"
  status TEXT NOT NULL,          -- pending | published | failed
  error TEXT,
  run_url TEXT,
  token_hash TEXT,               -- proves the workflow's report (one use)
  published_by TEXT,
  created_at TEXT NOT NULL,
  finished_at TEXT,
  PRIMARY KEY (blueprint_id, version)
);
CREATE INDEX blueprint_versions_kind ON blueprint_versions (kind, status, finished_at);
