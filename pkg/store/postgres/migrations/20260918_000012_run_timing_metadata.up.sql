ALTER TABLE cfa_runs
  ADD COLUMN IF NOT EXISTS started_at TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS completed_at TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS timing_metadata_state TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS timing_metadata_first_checked_at TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS timing_metadata_checked_at TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS cfa_runs_timing_metadata_state_idx
  ON cfa_runs (timing_metadata_state);
