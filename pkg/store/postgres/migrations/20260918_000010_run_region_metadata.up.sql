ALTER TABLE cfa_runs
  ADD COLUMN IF NOT EXISTS region TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS region_metadata_state TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS region_metadata_checked_at TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS cfa_runs_region_metadata_state_idx
  ON cfa_runs (region_metadata_state);
