package contracts

import (
	"context"
	"time"
)

// RunRecord is the currently collected run-level fact used by weekly metrics,
// windowed/day run history views, and failure-pattern reference joins.
//
// Today it intentionally remains small: run URL, job identity, basic PR
// metadata, pass/fail state, and occurred-at timestamp.
//
// It is not yet a full Prow-style run history record. In particular it does not
// currently carry duration/build identifiers or richer terminal run-state
// details, and some raw-failure rows may still reference runs whose RunRecord
// needs backfill/lookup.
type RunRecord struct {
	Environment    string `json:"environment"`
	RunURL         string `json:"run_url"`
	JobName        string `json:"job_name"`
	PRNumber       int    `json:"pr_number"`
	PRState        string `json:"pr_state"`
	PRSHA          string `json:"pr_sha"`
	FinalMergedSHA string `json:"final_merged_sha"`
	MergedPR       bool   `json:"merged_pr"`
	PostGoodCommit bool   `json:"post_good_commit"`
	Failed         bool   `json:"failed"`
	OccurredAt     string `json:"occurred_at"`
}

type PullRequestRecord struct {
	PRNumber       int    `json:"pr_number"`
	State          string `json:"state"`
	Merged         bool   `json:"merged"`
	HeadSHA        string `json:"head_sha"`
	MergeCommitSHA string `json:"merge_commit_sha"`
	MergedAt       string `json:"merged_at"`
	ClosedAt       string `json:"closed_at"`
	UpdatedAt      string `json:"updated_at"`
	LastCheckedAt  string `json:"last_checked_at"`
}

type ArtifactFailureRecord struct {
	Environment string `json:"environment"`
	// ArtifactRowID is the deterministic failure occurrence identifier.
	// It should be stable for the exact testcase failure row within one run.
	ArtifactRowID string `json:"artifact_row_id"`
	RunURL        string `json:"run_url"`
	TestName      string `json:"test_name"`
	TestSuite     string `json:"test_suite"`
	// SignatureID is the deterministic failure fingerprint:
	// sha256(normalized failure text).
	SignatureID string `json:"signature_id"`
	FailureText string `json:"failure_text"`
}

type RawFailureRecord struct {
	Environment string `json:"environment"`
	RowID       string `json:"row_id"`
	RunURL      string `json:"run_url"`
	// NonArtifactBacked marks synthetic rows generated from failed runs when no
	// artifact-backed JUnit failure rows are available.
	NonArtifactBacked bool   `json:"non_artifact_backed"`
	TestName          string `json:"test_name"`
	TestSuite         string `json:"test_suite"`
	// SignatureID is the deterministic failure fingerprint:
	// sha256(normalized failure text).
	SignatureID    string `json:"signature_id"`
	OccurredAt     string `json:"occurred_at"`
	RawText        string `json:"raw_text"`
	NormalizedText string `json:"normalized_text"`
}

type MetricDailyRecord struct {
	Environment string  `json:"environment"`
	Date        string  `json:"date"`
	Metric      string  `json:"metric"`
	Value       float64 `json:"value"`
}

type TestMetadataDailyRecord struct {
	Environment            string  `json:"environment"`
	Date                   string  `json:"date"`
	Release                string  `json:"release"`
	Period                 string  `json:"period"`
	TestName               string  `json:"test_name"`
	TestSuite              string  `json:"test_suite"`
	CurrentPassPercentage  float64 `json:"current_pass_percentage"`
	CurrentRuns            int     `json:"current_runs"`
	PreviousPassPercentage float64 `json:"previous_pass_percentage"`
	PreviousRuns           int     `json:"previous_runs"`
	NetImprovement         float64 `json:"net_improvement"`
	IngestedAt             string  `json:"ingested_at"`
}

type CheckpointRecord struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	UpdatedAt string `json:"updated_at"`
}

type DeadLetterRecord struct {
	Controller string `json:"controller"`
	Key        string `json:"key"`
	Error      string `json:"error"`
	FailedAt   string `json:"failed_at"`
}

type RunStore interface {
	UpsertRuns(ctx context.Context, runs []RunRecord) error
	ListRunKeys(ctx context.Context) ([]string, error)
	ListRunDates(ctx context.Context) ([]string, error)
	ListRunsByDateRange(ctx context.Context, environment string, startTime time.Time, endTime time.Time) ([]RunRecord, error)
	GetRun(ctx context.Context, environment string, runURL string) (RunRecord, bool, error)
}

type PullRequestStore interface {
	UpsertPullRequests(ctx context.Context, rows []PullRequestRecord) error
	ListPullRequests(ctx context.Context) ([]PullRequestRecord, error)
	GetPullRequest(ctx context.Context, prNumber int) (PullRequestRecord, bool, error)
}

type ArtifactFailureStore interface {
	UpsertArtifactFailures(ctx context.Context, rows []ArtifactFailureRecord) error
	ListArtifactRunKeys(ctx context.Context) ([]string, error)
	ListArtifactFailuresByRun(ctx context.Context, environment string, runURL string) ([]ArtifactFailureRecord, error)
}

type RawFailureStore interface {
	UpsertRawFailures(ctx context.Context, rows []RawFailureRecord) error
	ListRawFailuresByRun(ctx context.Context, environment string, runURL string) ([]RawFailureRecord, error)
	ListRawFailuresByDateRange(ctx context.Context, environment string, startTime time.Time, endTime time.Time) ([]RawFailureRecord, error)
}

type MetricsStore interface {
	UpsertMetricsDaily(ctx context.Context, rows []MetricDailyRecord) error
	ListMetricsDailyByDate(ctx context.Context, environment string, date string) ([]MetricDailyRecord, error)
	ListMetricDates(ctx context.Context) ([]string, error)
	ListMetricsDailyForDates(ctx context.Context, environments []string, dates []string) ([]MetricDailyRecord, error)
	SumMetricByEnvironmentForDates(ctx context.Context, metric string, environments []string, dates []string) (map[string]float64, error)
}

type TestMetadataDailyStore interface {
	UpsertTestMetadataDaily(ctx context.Context, rows []TestMetadataDailyRecord) error
	ListTestMetadataDailyByDate(ctx context.Context, environment string, date string) ([]TestMetadataDailyRecord, error)
	ListTestMetadataDatesByEnvironment(ctx context.Context, environment string, period string) ([]string, error)
	ListBelowTargetTestMetadataByDate(ctx context.Context, environment string, date string, period string, targetPassRate float64, minRuns int, limit int) ([]TestMetadataDailyRecord, error)
}

type CheckpointStore interface {
	UpsertCheckpoints(ctx context.Context, rows []CheckpointRecord) error
	GetCheckpoint(ctx context.Context, name string) (CheckpointRecord, bool, error)
}

type DeadLetterStore interface {
	AppendDeadLetters(ctx context.Context, rows []DeadLetterRecord) error
	ListDeadLetters(ctx context.Context, limit int) ([]DeadLetterRecord, error)
}

type Store interface {
	RunStore
	PullRequestStore
	ArtifactFailureStore
	RawFailureStore
	MetricsStore
	TestMetadataDailyStore
	CheckpointStore
	DeadLetterStore

	Close() error
}
