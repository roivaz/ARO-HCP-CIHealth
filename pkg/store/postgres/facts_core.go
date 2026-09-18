package postgres

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	storecontracts "github.com/roivaz/ARO-HCP-CIHealth/pkg/store/contracts"

	"github.com/jackc/pgx/v5"
)

func (s *Store) upsertRunsImpl(ctx context.Context, rows []storecontracts.RunRecord) error {
	if len(rows) == 0 {
		return nil
	}
	normalizedRows := make([]storecontracts.RunRecord, 0, len(rows))
	for _, row := range rows {
		normalized := normalizeRunRecord(row)
		if runRecordKey(normalized) == "" {
			return fmt.Errorf("run record missing environment and/or run_url")
		}
		normalizedRows = append(normalizedRows, normalized)
	}

	return s.withTx(ctx, func(tx pgx.Tx) error {
		for _, row := range normalizedRows {
			_, err := tx.Exec(ctx, `
INSERT INTO cfa_runs (
  environment, run_url, job_name, pr_number, pr_state, pr_sha,
  final_merged_sha, merged_pr, post_good_commit, failed, occurred_at,
  started_at, completed_at, timing_metadata_state, timing_metadata_first_checked_at, timing_metadata_checked_at,
  region, region_metadata_state, region_metadata_first_checked_at, region_metadata_checked_at
) VALUES (
  $1, $2, $3, $4, $5, $6,
  $7, $8, $9, $10, $11,
  $12, $13, $14, $15, $16,
  $17, $18, $19, $20
)
ON CONFLICT (environment, run_url)
DO UPDATE SET
  job_name = EXCLUDED.job_name,
  pr_number = EXCLUDED.pr_number,
  pr_state = EXCLUDED.pr_state,
  pr_sha = EXCLUDED.pr_sha,
  final_merged_sha = EXCLUDED.final_merged_sha,
  merged_pr = EXCLUDED.merged_pr,
  post_good_commit = EXCLUDED.post_good_commit,
  failed = EXCLUDED.failed,
  occurred_at = EXCLUDED.occurred_at,
  started_at = CASE
    WHEN EXCLUDED.timing_metadata_state = '' THEN cfa_runs.started_at
    ELSE EXCLUDED.started_at
  END,
  completed_at = CASE
    WHEN EXCLUDED.timing_metadata_state = '' THEN cfa_runs.completed_at
    ELSE EXCLUDED.completed_at
  END,
  timing_metadata_state = CASE
    WHEN EXCLUDED.timing_metadata_state = '' THEN cfa_runs.timing_metadata_state
    ELSE EXCLUDED.timing_metadata_state
  END,
  timing_metadata_first_checked_at = CASE
    WHEN EXCLUDED.timing_metadata_state = '' THEN cfa_runs.timing_metadata_first_checked_at
    ELSE EXCLUDED.timing_metadata_first_checked_at
  END,
  timing_metadata_checked_at = CASE
    WHEN EXCLUDED.timing_metadata_state = '' THEN cfa_runs.timing_metadata_checked_at
    ELSE EXCLUDED.timing_metadata_checked_at
  END,
  region = CASE
    WHEN EXCLUDED.region_metadata_state = '' THEN cfa_runs.region
    ELSE EXCLUDED.region
  END,
  region_metadata_state = CASE
    WHEN EXCLUDED.region_metadata_state = '' THEN cfa_runs.region_metadata_state
    ELSE EXCLUDED.region_metadata_state
  END,
  region_metadata_first_checked_at = CASE
    WHEN EXCLUDED.region_metadata_state = '' THEN cfa_runs.region_metadata_first_checked_at
    ELSE EXCLUDED.region_metadata_first_checked_at
  END,
  region_metadata_checked_at = CASE
    WHEN EXCLUDED.region_metadata_state = '' THEN cfa_runs.region_metadata_checked_at
    ELSE EXCLUDED.region_metadata_checked_at
  END
`, row.Environment, row.RunURL, row.JobName, row.PRNumber, row.PRState, row.PRSHA, row.FinalMergedSHA, row.MergedPR, row.PostGoodCommit, row.Failed, row.OccurredAt, row.StartedAt, row.CompletedAt, row.TimingMetadataState, row.TimingMetadataFirstCheckedAt, row.TimingMetadataCheckedAt, row.Region, row.RegionMetadataState, row.RegionMetadataFirstCheckedAt, row.RegionMetadataCheckedAt)
			if err != nil {
				return fmt.Errorf("upsert run row (%s,%s): %w", row.Environment, row.RunURL, err)
			}
		}
		return nil
	})
}

func (s *Store) listRunKeysImpl(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT environment, run_url FROM cfa_runs`)
	if err != nil {
		return nil, fmt.Errorf("query run keys: %w", err)
	}
	defer rows.Close()

	keys := map[string]struct{}{}
	for rows.Next() {
		var environment, runURL string
		if err := rows.Scan(&environment, &runURL); err != nil {
			return nil, fmt.Errorf("scan run key row: %w", err)
		}
		key := runRecordKey(normalizeRunRecord(storecontracts.RunRecord{
			Environment: environment,
			RunURL:      runURL,
		}))
		if key == "" {
			continue
		}
		keys[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run keys: %w", err)
	}

	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Store) listRunDatesImpl(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
SELECT DISTINCT cfa_parse_rfc3339_utc_date(occurred_at)::TEXT AS occurred_date
FROM cfa_runs
WHERE cfa_parse_rfc3339_utc_date(occurred_at) IS NOT NULL
ORDER BY occurred_date
`)
	if err != nil {
		return nil, fmt.Errorf("query run dates: %w", err)
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var occurredDate string
		if err := rows.Scan(&occurredDate); err != nil {
			return nil, fmt.Errorf("scan run date row: %w", err)
		}
		trimmedDate := strings.TrimSpace(occurredDate)
		if trimmedDate == "" {
			continue
		}
		out = append(out, trimmedDate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run dates: %w", err)
	}
	return out, nil
}

func (s *Store) listRunsByDateRangeImpl(
	ctx context.Context,
	environment string,
	startTime time.Time,
	endTime time.Time,
) ([]storecontracts.RunRecord, error) {
	lookupEnv := normalizeEnvironment(environment)
	if lookupEnv == "" {
		return nil, fmt.Errorf("run lookup requires environment")
	}
	normalizedStart, normalizedEnd, err := normalizeTimestampRange(startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("run range lookup requires a valid time window: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
SELECT environment, run_url, job_name, pr_number, pr_state, pr_sha, final_merged_sha, merged_pr, post_good_commit, failed, occurred_at, started_at, completed_at, timing_metadata_state, timing_metadata_first_checked_at, timing_metadata_checked_at, region, region_metadata_state, region_metadata_first_checked_at, region_metadata_checked_at
FROM (
  SELECT
    environment,
    run_url,
    job_name,
    pr_number,
    pr_state,
    pr_sha,
    final_merged_sha,
    merged_pr,
    post_good_commit,
    failed,
    occurred_at,
    started_at,
    completed_at,
    timing_metadata_state,
    timing_metadata_first_checked_at,
    timing_metadata_checked_at,
    region,
    region_metadata_state,
    region_metadata_first_checked_at,
    region_metadata_checked_at,
    cfa_parse_rfc3339_utc_timestamp(occurred_at) AS occurred_ts
  FROM cfa_runs
  WHERE environment = $1
) runs
WHERE occurred_ts >= $2::TIMESTAMPTZ
  AND occurred_ts < $3::TIMESTAMPTZ
ORDER BY occurred_ts, occurred_at, run_url
`, lookupEnv, normalizedStart, normalizedEnd)
	if err != nil {
		return nil, fmt.Errorf("query runs by date range: %w", err)
	}
	defer rows.Close()

	out := make([]storecontracts.RunRecord, 0)
	for rows.Next() {
		var row storecontracts.RunRecord
		if err := rows.Scan(
			&row.Environment,
			&row.RunURL,
			&row.JobName,
			&row.PRNumber,
			&row.PRState,
			&row.PRSHA,
			&row.FinalMergedSHA,
			&row.MergedPR,
			&row.PostGoodCommit,
			&row.Failed,
			&row.OccurredAt,
			&row.StartedAt,
			&row.CompletedAt,
			&row.TimingMetadataState,
			&row.TimingMetadataFirstCheckedAt,
			&row.TimingMetadataCheckedAt,
			&row.Region,
			&row.RegionMetadataState,
			&row.RegionMetadataFirstCheckedAt,
			&row.RegionMetadataCheckedAt,
		); err != nil {
			return nil, fmt.Errorf("scan run row by date range: %w", err)
		}
		normalized := normalizeRunRecord(row)
		if normalized.RunURL == "" {
			continue
		}
		out = append(out, normalized)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs by date range: %w", err)
	}
	return out, nil
}

func (s *Store) listRunsNeedingTimingMetadataImpl(
	ctx context.Context,
	environments []string,
	startTime time.Time,
) ([]storecontracts.RunRecord, error) {
	normalizedEnvironments := normalizeEnvironmentSlice(environments)
	if len(normalizedEnvironments) == 0 {
		return []storecontracts.RunRecord{}, nil
	}
	if startTime.IsZero() {
		return nil, fmt.Errorf("timing metadata lookup requires a start time")
	}

	rows, err := s.pool.Query(ctx, `
SELECT environment, run_url, job_name, pr_number, pr_state, pr_sha, final_merged_sha, merged_pr, post_good_commit, failed, occurred_at, started_at, completed_at, timing_metadata_state, timing_metadata_first_checked_at, timing_metadata_checked_at, region, region_metadata_state, region_metadata_first_checked_at, region_metadata_checked_at
FROM (
  SELECT
    environment,
    run_url,
    job_name,
    pr_number,
    pr_state,
    pr_sha,
    final_merged_sha,
    merged_pr,
    post_good_commit,
    failed,
    occurred_at,
    started_at,
    completed_at,
    timing_metadata_state,
    timing_metadata_first_checked_at,
    timing_metadata_checked_at,
    region,
    region_metadata_state,
    region_metadata_first_checked_at,
    region_metadata_checked_at,
    cfa_parse_rfc3339_utc_timestamp(occurred_at) AS occurred_ts
  FROM cfa_runs
  WHERE environment = ANY($1)
    AND timing_metadata_state IN ('', 'pending')
) runs
WHERE occurred_ts >= $2::TIMESTAMPTZ
ORDER BY occurred_ts, environment, run_url
`, normalizedEnvironments, startTime.UTC())
	if err != nil {
		return nil, fmt.Errorf("query runs needing timing metadata: %w", err)
	}
	defer rows.Close()

	out := make([]storecontracts.RunRecord, 0)
	for rows.Next() {
		var row storecontracts.RunRecord
		if err := rows.Scan(
			&row.Environment,
			&row.RunURL,
			&row.JobName,
			&row.PRNumber,
			&row.PRState,
			&row.PRSHA,
			&row.FinalMergedSHA,
			&row.MergedPR,
			&row.PostGoodCommit,
			&row.Failed,
			&row.OccurredAt,
			&row.StartedAt,
			&row.CompletedAt,
			&row.TimingMetadataState,
			&row.TimingMetadataFirstCheckedAt,
			&row.TimingMetadataCheckedAt,
			&row.Region,
			&row.RegionMetadataState,
			&row.RegionMetadataFirstCheckedAt,
			&row.RegionMetadataCheckedAt,
		); err != nil {
			return nil, fmt.Errorf("scan run needing timing metadata: %w", err)
		}
		out = append(out, normalizeRunRecord(row))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs needing timing metadata: %w", err)
	}
	return out, nil
}

func (s *Store) listRunsNeedingRegionMetadataImpl(
	ctx context.Context,
	environments []string,
	startTime time.Time,
) ([]storecontracts.RunRecord, error) {
	normalizedEnvironments := normalizeEnvironmentSlice(environments)
	if len(normalizedEnvironments) == 0 {
		return []storecontracts.RunRecord{}, nil
	}
	if startTime.IsZero() {
		return nil, fmt.Errorf("region metadata lookup requires a start time")
	}

	rows, err := s.pool.Query(ctx, `
SELECT environment, run_url, job_name, pr_number, pr_state, pr_sha, final_merged_sha, merged_pr, post_good_commit, failed, occurred_at, started_at, completed_at, timing_metadata_state, timing_metadata_first_checked_at, timing_metadata_checked_at, region, region_metadata_state, region_metadata_first_checked_at, region_metadata_checked_at
FROM (
  SELECT
    environment,
    run_url,
    job_name,
    pr_number,
    pr_state,
    pr_sha,
    final_merged_sha,
    merged_pr,
    post_good_commit,
    failed,
    occurred_at,
    started_at,
    completed_at,
    timing_metadata_state,
    timing_metadata_first_checked_at,
    timing_metadata_checked_at,
    region,
    region_metadata_state,
    region_metadata_first_checked_at,
    region_metadata_checked_at,
    cfa_parse_rfc3339_utc_timestamp(occurred_at) AS occurred_ts
  FROM cfa_runs
  WHERE environment = ANY($1)
    AND region = ''
    AND region_metadata_state IN ('', 'pending')
) runs
WHERE occurred_ts >= $2::TIMESTAMPTZ
ORDER BY occurred_ts, environment, run_url
`, normalizedEnvironments, startTime.UTC())
	if err != nil {
		return nil, fmt.Errorf("query runs needing region metadata: %w", err)
	}
	defer rows.Close()

	out := make([]storecontracts.RunRecord, 0)
	for rows.Next() {
		var row storecontracts.RunRecord
		if err := rows.Scan(
			&row.Environment,
			&row.RunURL,
			&row.JobName,
			&row.PRNumber,
			&row.PRState,
			&row.PRSHA,
			&row.FinalMergedSHA,
			&row.MergedPR,
			&row.PostGoodCommit,
			&row.Failed,
			&row.OccurredAt,
			&row.StartedAt,
			&row.CompletedAt,
			&row.TimingMetadataState,
			&row.TimingMetadataFirstCheckedAt,
			&row.TimingMetadataCheckedAt,
			&row.Region,
			&row.RegionMetadataState,
			&row.RegionMetadataFirstCheckedAt,
			&row.RegionMetadataCheckedAt,
		); err != nil {
			return nil, fmt.Errorf("scan run needing region metadata: %w", err)
		}
		out = append(out, normalizeRunRecord(row))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate runs needing region metadata: %w", err)
	}
	return out, nil
}

func (s *Store) getRunImpl(ctx context.Context, environment string, runURL string) (storecontracts.RunRecord, bool, error) {
	lookup := normalizeRunRecord(storecontracts.RunRecord{
		Environment: environment,
		RunURL:      runURL,
	})
	if lookup.Environment == "" || lookup.RunURL == "" {
		return storecontracts.RunRecord{}, false, fmt.Errorf("run lookup requires environment and run_url")
	}

	var row storecontracts.RunRecord
	if err := s.pool.QueryRow(ctx, `
SELECT environment, run_url, job_name, pr_number, pr_state, pr_sha, final_merged_sha, merged_pr, post_good_commit, failed, occurred_at, started_at, completed_at, timing_metadata_state, timing_metadata_first_checked_at, timing_metadata_checked_at, region, region_metadata_state, region_metadata_first_checked_at, region_metadata_checked_at
FROM cfa_runs
WHERE environment = $1 AND run_url = $2
`, lookup.Environment, lookup.RunURL).Scan(
		&row.Environment,
		&row.RunURL,
		&row.JobName,
		&row.PRNumber,
		&row.PRState,
		&row.PRSHA,
		&row.FinalMergedSHA,
		&row.MergedPR,
		&row.PostGoodCommit,
		&row.Failed,
		&row.OccurredAt,
		&row.StartedAt,
		&row.CompletedAt,
		&row.TimingMetadataState,
		&row.TimingMetadataFirstCheckedAt,
		&row.TimingMetadataCheckedAt,
		&row.Region,
		&row.RegionMetadataState,
		&row.RegionMetadataFirstCheckedAt,
		&row.RegionMetadataCheckedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return storecontracts.RunRecord{}, false, nil
		}
		return storecontracts.RunRecord{}, false, fmt.Errorf("query run: %w", err)
	}
	return normalizeRunRecord(row), true, nil
}

func (s *Store) updateRunTimingMetadataImpl(ctx context.Context, run storecontracts.RunRecord) error {
	normalized := normalizeRunRecord(run)
	if normalized.Environment == "" || normalized.RunURL == "" {
		return fmt.Errorf("run timing metadata update requires environment and run_url")
	}
	switch normalized.TimingMetadataState {
	case storecontracts.RunTimingMetadataStatePending,
		storecontracts.RunTimingMetadataStateFound,
		storecontracts.RunTimingMetadataStateForbidden,
		storecontracts.RunTimingMetadataStateMissing,
		storecontracts.RunTimingMetadataStateInvalid:
	default:
		return fmt.Errorf("run timing metadata update requires a valid state")
	}
	if normalized.TimingMetadataState == storecontracts.RunTimingMetadataStateFound {
		startedAt, err := time.Parse(time.RFC3339Nano, normalized.StartedAt)
		if err != nil {
			return fmt.Errorf("run timing metadata update requires a valid started_at: %w", err)
		}
		completedAt, err := time.Parse(time.RFC3339Nano, normalized.CompletedAt)
		if err != nil {
			return fmt.Errorf("run timing metadata update requires a valid completed_at: %w", err)
		}
		if completedAt.Before(startedAt) {
			return fmt.Errorf("run timing metadata update requires completed_at at or after started_at")
		}
	}

	commandTag, err := s.pool.Exec(ctx, `
UPDATE cfa_runs
SET started_at = $3,
    completed_at = $4,
    timing_metadata_state = $5,
    timing_metadata_first_checked_at = $6,
    timing_metadata_checked_at = $7
WHERE environment = $1 AND run_url = $2
`, normalized.Environment, normalized.RunURL, normalized.StartedAt, normalized.CompletedAt, normalized.TimingMetadataState, normalized.TimingMetadataFirstCheckedAt, normalized.TimingMetadataCheckedAt)
	if err != nil {
		return fmt.Errorf("update run timing metadata (%s,%s): %w", normalized.Environment, normalized.RunURL, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("update run timing metadata (%s,%s): run not found", normalized.Environment, normalized.RunURL)
	}
	return nil
}

func (s *Store) updateRunRegionMetadataImpl(ctx context.Context, run storecontracts.RunRecord) error {
	normalized := normalizeRunRecord(run)
	if normalized.Environment == "" || normalized.RunURL == "" {
		return fmt.Errorf("run region metadata update requires environment and run_url")
	}
	switch normalized.RegionMetadataState {
	case storecontracts.RunRegionMetadataStatePending,
		storecontracts.RunRegionMetadataStateFound,
		storecontracts.RunRegionMetadataStateForbidden,
		storecontracts.RunRegionMetadataStateMissing,
		storecontracts.RunRegionMetadataStateInvalid:
	default:
		return fmt.Errorf("run region metadata update requires a valid state")
	}

	commandTag, err := s.pool.Exec(ctx, `
UPDATE cfa_runs
SET region = $3,
    region_metadata_state = $4,
    region_metadata_first_checked_at = $5,
    region_metadata_checked_at = $6
WHERE environment = $1 AND run_url = $2
`, normalized.Environment, normalized.RunURL, normalized.Region, normalized.RegionMetadataState, normalized.RegionMetadataFirstCheckedAt, normalized.RegionMetadataCheckedAt)
	if err != nil {
		return fmt.Errorf("update run region metadata (%s,%s): %w", normalized.Environment, normalized.RunURL, err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("update run region metadata (%s,%s): run not found", normalized.Environment, normalized.RunURL)
	}
	return nil
}

func (s *Store) upsertPullRequestsImpl(ctx context.Context, rows []storecontracts.PullRequestRecord) error {
	if len(rows) == 0 {
		return nil
	}
	normalizedRows := make([]storecontracts.PullRequestRecord, 0, len(rows))
	for _, row := range rows {
		normalized := normalizePullRequestRecord(row)
		if normalized.PRNumber <= 0 {
			return fmt.Errorf("pull request record missing pr_number")
		}
		normalizedRows = append(normalizedRows, normalized)
	}

	return s.withTx(ctx, func(tx pgx.Tx) error {
		for _, row := range normalizedRows {
			_, err := tx.Exec(ctx, `
INSERT INTO cfa_pull_requests (
  pr_number, state, merged, head_sha, merge_commit_sha, merged_at, closed_at, updated_at, last_checked_at
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (pr_number)
DO UPDATE SET
  state = EXCLUDED.state,
  merged = EXCLUDED.merged,
  head_sha = EXCLUDED.head_sha,
  merge_commit_sha = EXCLUDED.merge_commit_sha,
  merged_at = EXCLUDED.merged_at,
  closed_at = EXCLUDED.closed_at,
  updated_at = EXCLUDED.updated_at,
  last_checked_at = EXCLUDED.last_checked_at
`, row.PRNumber, row.State, row.Merged, row.HeadSHA, row.MergeCommitSHA, row.MergedAt, row.ClosedAt, row.UpdatedAt, row.LastCheckedAt)
			if err != nil {
				return fmt.Errorf("upsert pull request %d: %w", row.PRNumber, err)
			}
		}
		return nil
	})
}

func (s *Store) listPullRequestsImpl(ctx context.Context) ([]storecontracts.PullRequestRecord, error) {
	rows, err := s.pool.Query(ctx, `
SELECT pr_number, state, merged, head_sha, merge_commit_sha, merged_at, closed_at, updated_at, last_checked_at
FROM cfa_pull_requests
`)
	if err != nil {
		return nil, fmt.Errorf("query pull requests: %w", err)
	}
	defer rows.Close()

	out := make([]storecontracts.PullRequestRecord, 0)
	for rows.Next() {
		var row storecontracts.PullRequestRecord
		if err := rows.Scan(
			&row.PRNumber,
			&row.State,
			&row.Merged,
			&row.HeadSHA,
			&row.MergeCommitSHA,
			&row.MergedAt,
			&row.ClosedAt,
			&row.UpdatedAt,
			&row.LastCheckedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pull request row: %w", err)
		}
		normalized := normalizePullRequestRecord(row)
		if normalized.PRNumber <= 0 {
			continue
		}
		out = append(out, normalized)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pull requests: %w", err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PRNumber < out[j].PRNumber })
	return out, nil
}

func (s *Store) getPullRequestImpl(ctx context.Context, prNumber int) (storecontracts.PullRequestRecord, bool, error) {
	if prNumber <= 0 {
		return storecontracts.PullRequestRecord{}, false, fmt.Errorf("pull request lookup requires positive pr_number")
	}

	var row storecontracts.PullRequestRecord
	if err := s.pool.QueryRow(ctx, `
SELECT pr_number, state, merged, head_sha, merge_commit_sha, merged_at, closed_at, updated_at, last_checked_at
FROM cfa_pull_requests
WHERE pr_number = $1
`, prNumber).Scan(
		&row.PRNumber,
		&row.State,
		&row.Merged,
		&row.HeadSHA,
		&row.MergeCommitSHA,
		&row.MergedAt,
		&row.ClosedAt,
		&row.UpdatedAt,
		&row.LastCheckedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return storecontracts.PullRequestRecord{}, false, nil
		}
		return storecontracts.PullRequestRecord{}, false, fmt.Errorf("query pull request %d: %w", prNumber, err)
	}
	return normalizePullRequestRecord(row), true, nil
}

func (s *Store) upsertArtifactFailuresImpl(ctx context.Context, rows []storecontracts.ArtifactFailureRecord) error {
	if len(rows) == 0 {
		return nil
	}
	normalizedRows := make([]storecontracts.ArtifactFailureRecord, 0, len(rows))
	for _, row := range rows {
		normalized := normalizeArtifactFailureRecord(row)
		if normalized.RunURL == "" {
			return fmt.Errorf("artifact failure record missing run_url")
		}
		if normalized.SignatureID == "" {
			return fmt.Errorf("artifact failure record missing signature_id")
		}
		if artifactFailureKey(normalized) == "" {
			return fmt.Errorf("artifact failure record missing environment and/or artifact_row_id")
		}
		normalizedRows = append(normalizedRows, normalized)
	}

	return s.withTx(ctx, func(tx pgx.Tx) error {
		for _, row := range normalizedRows {
			_, err := tx.Exec(ctx, `
INSERT INTO cfa_artifact_failures (
  environment, artifact_row_id, run_url, test_name, test_suite, signature_id, failure_text, artifact_path
) VALUES (
  $1, $2, $3, $4, $5, $6, $7, $8
)
ON CONFLICT (environment, artifact_row_id)
DO UPDATE SET
  run_url = EXCLUDED.run_url,
  test_name = EXCLUDED.test_name,
  test_suite = EXCLUDED.test_suite,
  signature_id = EXCLUDED.signature_id,
  failure_text = EXCLUDED.failure_text,
  artifact_path = EXCLUDED.artifact_path
`, row.Environment, row.ArtifactRowID, row.RunURL, row.TestName, row.TestSuite, row.SignatureID, row.FailureText, row.ArtifactPath)
			if err != nil {
				return fmt.Errorf("upsert artifact failure (%s,%s): %w", row.Environment, row.ArtifactRowID, err)
			}
		}
		return nil
	})
}

func (s *Store) listArtifactRunKeysImpl(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT environment, run_url FROM cfa_artifact_failures`)
	if err != nil {
		return nil, fmt.Errorf("query artifact run keys: %w", err)
	}
	defer rows.Close()

	keys := map[string]struct{}{}
	for rows.Next() {
		var environment, runURL string
		if err := rows.Scan(&environment, &runURL); err != nil {
			return nil, fmt.Errorf("scan artifact run key row: %w", err)
		}
		key := runRecordKey(storecontracts.RunRecord{
			Environment: normalizeEnvironment(environment),
			RunURL:      strings.TrimSpace(runURL),
		})
		if key == "" {
			continue
		}
		keys[key] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifact run keys: %w", err)
	}

	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out, nil
}

func (s *Store) listArtifactFailuresByRunImpl(ctx context.Context, environment string, runURL string) ([]storecontracts.ArtifactFailureRecord, error) {
	lookup := normalizeRunRecord(storecontracts.RunRecord{
		Environment: environment,
		RunURL:      runURL,
	})
	if lookup.Environment == "" || lookup.RunURL == "" {
		return nil, fmt.Errorf("artifact failure lookup requires environment and run_url")
	}

	rows, err := s.pool.Query(ctx, `
SELECT environment, artifact_row_id, run_url, test_name, test_suite, signature_id, failure_text, artifact_path
FROM cfa_artifact_failures
WHERE environment = $1 AND run_url = $2
`, lookup.Environment, lookup.RunURL)
	if err != nil {
		return nil, fmt.Errorf("query artifact failures by run: %w", err)
	}
	defer rows.Close()

	out := make([]storecontracts.ArtifactFailureRecord, 0)
	for rows.Next() {
		var row storecontracts.ArtifactFailureRecord
		if err := rows.Scan(
			&row.Environment,
			&row.ArtifactRowID,
			&row.RunURL,
			&row.TestName,
			&row.TestSuite,
			&row.SignatureID,
			&row.FailureText,
			&row.ArtifactPath,
		); err != nil {
			return nil, fmt.Errorf("scan artifact failure row: %w", err)
		}
		normalized := normalizeArtifactFailureRecord(row)
		if normalized.Environment != lookup.Environment || normalized.RunURL != lookup.RunURL {
			continue
		}
		if normalized.ArtifactRowID == "" || normalized.SignatureID == "" {
			continue
		}
		out = append(out, normalized)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifact failures by run: %w", err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TestSuite != out[j].TestSuite {
			return out[i].TestSuite < out[j].TestSuite
		}
		if out[i].TestName != out[j].TestName {
			return out[i].TestName < out[j].TestName
		}
		if out[i].ArtifactRowID != out[j].ArtifactRowID {
			return out[i].ArtifactRowID < out[j].ArtifactRowID
		}
		if out[i].SignatureID != out[j].SignatureID {
			return out[i].SignatureID < out[j].SignatureID
		}
		return out[i].FailureText < out[j].FailureText
	})
	return out, nil
}

func (s *Store) upsertRawFailuresImpl(ctx context.Context, rows []storecontracts.RawFailureRecord) error {
	if len(rows) == 0 {
		return nil
	}
	normalizedRows := make([]storecontracts.RawFailureRecord, 0, len(rows))
	touchedRuns := map[string]storecontracts.RunRecord{}
	for _, row := range rows {
		normalized := normalizeRawFailureRecord(row)
		if rawFailureKey(normalized) == "" {
			return fmt.Errorf("raw failure record missing environment and/or row_id")
		}
		run := storecontracts.RunRecord{
			Environment: normalized.Environment,
			RunURL:      normalized.RunURL,
		}
		runKey := runRecordKey(run)
		if runKey == "" {
			return fmt.Errorf("raw failure record missing run_url")
		}
		normalizedRows = append(normalizedRows, normalized)
		touchedRuns[runKey] = run
	}

	return s.withTx(ctx, func(tx pgx.Tx) error {
		for _, run := range touchedRuns {
			_, err := tx.Exec(ctx, `
DELETE FROM cfa_raw_failures
WHERE environment = $1 AND run_url = $2
`, run.Environment, run.RunURL)
			if err != nil {
				return fmt.Errorf("delete touched raw failures for (%s,%s): %w", run.Environment, run.RunURL, err)
			}
		}
		for _, row := range normalizedRows {
			_, err := tx.Exec(ctx, `
INSERT INTO cfa_raw_failures (
  environment, row_id, run_url, non_artifact_backed, test_name, test_suite,
  signature_id, occurred_at, raw_text, normalized_text, artifact_path
) VALUES (
  $1, $2, $3, $4, $5, $6,
  $7, $8, $9, $10, $11
)
ON CONFLICT (environment, row_id)
DO UPDATE SET
  run_url = EXCLUDED.run_url,
  non_artifact_backed = EXCLUDED.non_artifact_backed,
  test_name = EXCLUDED.test_name,
  test_suite = EXCLUDED.test_suite,
  signature_id = EXCLUDED.signature_id,
  occurred_at = EXCLUDED.occurred_at,
  raw_text = EXCLUDED.raw_text,
  normalized_text = EXCLUDED.normalized_text,
  artifact_path = EXCLUDED.artifact_path
`, row.Environment, row.RowID, row.RunURL, row.NonArtifactBacked, row.TestName, row.TestSuite, row.SignatureID, row.OccurredAt, row.RawText, row.NormalizedText, row.ArtifactPath)
			if err != nil {
				return fmt.Errorf("upsert raw failure (%s,%s): %w", row.Environment, row.RowID, err)
			}
		}
		return nil
	})
}

func (s *Store) listRawFailuresByRunImpl(ctx context.Context, environment string, runURL string) ([]storecontracts.RawFailureRecord, error) {
	lookup := normalizeRunRecord(storecontracts.RunRecord{
		Environment: environment,
		RunURL:      runURL,
	})
	if lookup.Environment == "" || lookup.RunURL == "" {
		return nil, fmt.Errorf("raw failure lookup requires environment and run_url")
	}

	rows, err := s.pool.Query(ctx, `
SELECT environment, row_id, run_url, non_artifact_backed, test_name, test_suite, signature_id, occurred_at, raw_text, normalized_text, artifact_path
FROM cfa_raw_failures
WHERE environment = $1 AND run_url = $2
`, lookup.Environment, lookup.RunURL)
	if err != nil {
		return nil, fmt.Errorf("query raw failures by run: %w", err)
	}
	defer rows.Close()

	out := make([]storecontracts.RawFailureRecord, 0)
	for rows.Next() {
		var row storecontracts.RawFailureRecord
		if err := rows.Scan(
			&row.Environment,
			&row.RowID,
			&row.RunURL,
			&row.NonArtifactBacked,
			&row.TestName,
			&row.TestSuite,
			&row.SignatureID,
			&row.OccurredAt,
			&row.RawText,
			&row.NormalizedText,
			&row.ArtifactPath,
		); err != nil {
			return nil, fmt.Errorf("scan raw failure by run row: %w", err)
		}
		normalized := normalizeRawFailureRecord(row)
		if normalized.Environment != lookup.Environment || normalized.RunURL != lookup.RunURL {
			continue
		}
		if normalized.RowID == "" || normalized.SignatureID == "" {
			continue
		}
		out = append(out, normalized)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate raw failures by run: %w", err)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OccurredAt != out[j].OccurredAt {
			return out[i].OccurredAt < out[j].OccurredAt
		}
		if out[i].RowID != out[j].RowID {
			return out[i].RowID < out[j].RowID
		}
		return out[i].SignatureID < out[j].SignatureID
	})
	return out, nil
}

func (s *Store) listRawFailuresByDateRangeImpl(
	ctx context.Context,
	environment string,
	startTime time.Time,
	endTime time.Time,
) ([]storecontracts.RawFailureRecord, error) {
	lookupEnv := normalizeEnvironment(environment)
	if lookupEnv == "" {
		return nil, fmt.Errorf("raw failure lookup requires environment")
	}
	normalizedStart, normalizedEnd, err := normalizeTimestampRange(startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("raw failure range lookup requires a valid time window: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
SELECT environment, row_id, run_url, non_artifact_backed, test_name, test_suite, signature_id, occurred_at, raw_text, normalized_text, artifact_path
FROM (
  SELECT
    environment,
    row_id,
    run_url,
    non_artifact_backed,
    test_name,
    test_suite,
    signature_id,
    occurred_at,
    raw_text,
    normalized_text,
    artifact_path,
    cfa_parse_rfc3339_utc_timestamp(occurred_at) AS occurred_ts
  FROM cfa_raw_failures
  WHERE environment = $1
) raw_failures
WHERE occurred_ts >= $2::TIMESTAMPTZ
  AND occurred_ts < $3::TIMESTAMPTZ
ORDER BY occurred_ts, occurred_at, run_url, row_id, signature_id
`, lookupEnv, normalizedStart, normalizedEnd)
	if err != nil {
		return nil, fmt.Errorf("query raw failures by date range: %w", err)
	}
	defer rows.Close()

	out := make([]storecontracts.RawFailureRecord, 0)
	for rows.Next() {
		var row storecontracts.RawFailureRecord
		if err := rows.Scan(
			&row.Environment,
			&row.RowID,
			&row.RunURL,
			&row.NonArtifactBacked,
			&row.TestName,
			&row.TestSuite,
			&row.SignatureID,
			&row.OccurredAt,
			&row.RawText,
			&row.NormalizedText,
			&row.ArtifactPath,
		); err != nil {
			return nil, fmt.Errorf("scan raw failure by date range row: %w", err)
		}
		normalized := normalizeRawFailureRecord(row)
		if normalized.RowID == "" || normalized.RunURL == "" || normalized.SignatureID == "" {
			continue
		}
		out = append(out, normalized)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate raw failures by date range: %w", err)
	}
	return out, nil
}

func sortRunsForStableOutput(rows []storecontracts.RunRecord) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Environment != rows[j].Environment {
			return rows[i].Environment < rows[j].Environment
		}
		if rows[i].RunURL != rows[j].RunURL {
			return rows[i].RunURL < rows[j].RunURL
		}
		if rows[i].OccurredAt != rows[j].OccurredAt {
			return rows[i].OccurredAt < rows[j].OccurredAt
		}
		if rows[i].PRNumber != rows[j].PRNumber {
			return rows[i].PRNumber < rows[j].PRNumber
		}
		if rows[i].PRSHA != rows[j].PRSHA {
			return rows[i].PRSHA < rows[j].PRSHA
		}
		if rows[i].FinalMergedSHA != rows[j].FinalMergedSHA {
			return rows[i].FinalMergedSHA < rows[j].FinalMergedSHA
		}
		if rows[i].MergedPR != rows[j].MergedPR {
			return !rows[i].MergedPR && rows[j].MergedPR
		}
		if rows[i].PostGoodCommit != rows[j].PostGoodCommit {
			return !rows[i].PostGoodCommit && rows[j].PostGoodCommit
		}
		return rows[i].JobName < rows[j].JobName
	})
}
