ALTER TABLE cfa_runs
  ADD COLUMN IF NOT EXISTS region_metadata_first_checked_at TEXT NOT NULL DEFAULT '';
