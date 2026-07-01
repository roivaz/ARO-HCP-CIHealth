ALTER TABLE cfa_artifact_failures
  ADD COLUMN IF NOT EXISTS artifact_path TEXT NOT NULL DEFAULT '';

ALTER TABLE cfa_raw_failures
  ADD COLUMN IF NOT EXISTS artifact_path TEXT NOT NULL DEFAULT '';
