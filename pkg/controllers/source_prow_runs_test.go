package controllers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"

	semanticcontracts "ci-failure-atlas/pkg/semantic/contracts"
	sourceoptions "ci-failure-atlas/pkg/source/options"
	"ci-failure-atlas/pkg/source/prowjobs"
	"ci-failure-atlas/pkg/store/contracts"
)

func TestMapProwJobToRunRecord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		environment string
		job         prowjobs.Job
		want        contracts.RunRecord
	}{
		{
			name:        "dev presubmit failure",
			environment: "dev",
			job: prowjobs.Job{
				Spec: prowjobs.JobSpec{
					Job: "pull-ci-Azure-ARO-HCP-main-e2e-parallel",
					Refs: &prowjobs.Refs{
						Pulls: []prowjobs.Pull{
							{Number: 4313, SHA: "abc123"},
						},
					},
				},
				Status: prowjobs.JobStatus{
					State: "failure",
					URL:   "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4313/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
				},
			},
			want: contracts.RunRecord{
				Environment:    "dev",
				RunURL:         "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/Azure_ARO-HCP/4313/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
				JobName:        "pull-ci-Azure-ARO-HCP-main-e2e-parallel",
				PRNumber:       4313,
				PRSHA:          "abc123",
				MergedPR:       false,
				PostGoodCommit: false,
				Failed:         true,
			},
		},
		{
			name:        "periodic success",
			environment: "int",
			job: prowjobs.Job{
				Spec: prowjobs.JobSpec{
					Job: "periodic-ci-Azure-ARO-HCP-main-periodic-integration-e2e-parallel",
				},
				Status: prowjobs.JobStatus{
					State: "success",
					URL:   "gs://test-platform-results/logs/periodic-ci-Azure-ARO-HCP-main-periodic-integration-e2e-parallel/2029578186907455499",
				},
			},
			want: contracts.RunRecord{
				Environment:    "int",
				RunURL:         "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-Azure-ARO-HCP-main-periodic-integration-e2e-parallel/2029578186907455499",
				JobName:        "periodic-ci-Azure-ARO-HCP-main-periodic-integration-e2e-parallel",
				MergedPR:       true,
				PostGoodCommit: true,
				Failed:         false,
			},
		},
		{
			name:        "dev batch success",
			environment: "dev",
			job: prowjobs.Job{
				Spec: prowjobs.JobSpec{
					Job: "pull-ci-Azure-ARO-HCP-main-e2e-parallel",
				},
				Status: prowjobs.JobStatus{
					State: "success",
					URL:   "gs://test-platform-results/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455498",
				},
			},
			want: contracts.RunRecord{
				Environment:    "dev",
				RunURL:         "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455498",
				JobName:        "pull-ci-Azure-ARO-HCP-main-e2e-parallel",
				MergedPR:       false,
				PostGoodCommit: false,
				Failed:         false,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			startedAt := "2026-04-20T10:00:00Z"
			completedAt := "2026-04-20T10:45:00Z"
			tt.job.Status.StartTime = mustParseRFC3339(t, startedAt)
			tt.job.Status.CompletionTime = mustParseRFC3339(t, completedAt)
			tt.want.OccurredAt = startedAt

			got, ok := mapProwJobToRunRecord("https://prow.ci.openshift.org", tt.environment, tt.job)
			if !ok {
				t.Fatalf("expected job to map successfully")
			}
			if got != tt.want {
				t.Fatalf("mapped record mismatch:\n got=%+v\nwant=%+v", got, tt.want)
			}
		})
	}
}

func TestMapProwJobToRunRecordDropsAbortedJob(t *testing.T) {
	t.Parallel()

	job := prowjobs.Job{
		Spec: prowjobs.JobSpec{
			Job: "pull-ci-Azure-ARO-HCP-main-e2e-parallel",
		},
		Status: prowjobs.JobStatus{
			State:          "aborted",
			URL:            "gs://test-platform-results/pr-logs/pull/Azure_ARO-HCP/4313/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			StartTime:      mustParseRFC3339(t, "2026-04-20T10:00:00Z"),
			CompletionTime: mustParseRFC3339(t, "2026-04-20T10:45:00Z"),
		},
	}

	if _, ok := mapProwJobToRunRecord("https://prow.ci.openshift.org", "dev", job); ok {
		t.Fatalf("expected aborted job to be ignored")
	}
}

func TestMapProwJobToRunRecordKeepsTerminalJobWithoutCompletionTime(t *testing.T) {
	t.Parallel()

	job := prowjobs.Job{
		Spec: prowjobs.JobSpec{
			Job: "pull-ci-Azure-ARO-HCP-main-e2e-parallel",
			Refs: &prowjobs.Refs{
				Pulls: []prowjobs.Pull{{Number: 4313, SHA: "abc123"}},
			},
		},
		Status: prowjobs.JobStatus{
			State:     "failure",
			URL:       "gs://test-platform-results/pr-logs/pull/Azure_ARO-HCP/4313/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
			StartTime: mustParseRFC3339(t, "2026-04-20T10:00:00Z"),
		},
	}

	record, ok := mapProwJobToRunRecord("https://prow.ci.openshift.org", "dev", job)
	if !ok {
		t.Fatalf("expected terminal job without completion time to still map")
	}
	if record.OccurredAt != "2026-04-20T10:00:00Z" {
		t.Fatalf("unexpected occurred_at: got=%q", record.OccurredAt)
	}
}

func TestSyncOnceUsesSharedSnapshotForAllEnvironments(t *testing.T) {
	t.Parallel()

	opts := testSourceOptions(t, []string{"dev", "int"})
	devJobName, _ := sourceoptions.ProwJobNameForEnvironment("dev")
	intJobName, _ := sourceoptions.ProwJobNameForEnvironment("int")
	devStartedAt := time.Now().UTC().Add(-20 * time.Minute).Truncate(time.Second)
	intStartedAt := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)

	client := &fakeProwSnapshotClient{
		jobs: []prowjobs.Job{
			{
				Spec: prowjobs.JobSpec{
					Job: devJobName,
				},
				Status: prowjobs.JobStatus{
					State:     "failure",
					URL:       "gs://test-platform-results/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
					StartTime: devStartedAt,
				},
			},
			{
				Spec: prowjobs.JobSpec{
					Job: intJobName,
				},
				Status: prowjobs.JobStatus{
					State:     "success",
					URL:       "gs://test-platform-results/logs/periodic-ci-Azure-ARO-HCP-main-periodic-integration-e2e-parallel/2029578186907455499",
					StartTime: intStartedAt,
				},
			},
			{
				Spec: prowjobs.JobSpec{
					Job: devJobName,
				},
				Status: prowjobs.JobStatus{
					State:     "pending",
					URL:       "gs://test-platform-results/pr-logs/pull/Azure_ARO-HCP/4313/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455497",
					StartTime: time.Now().UTC().Add(-5 * time.Minute),
				},
			},
		},
	}
	store := &fakeProwRunsStore{
		runs:        map[string]contracts.RunRecord{},
		checkpoints: map[string]contracts.CheckpointRecord{},
	}
	controller, err := newSourceProwRunsController(logr.Discard(), Dependencies{
		Store:  store,
		Source: opts,
	}, client)
	if err != nil {
		t.Fatalf("newSourceProwRunsController returned error: %v", err)
	}

	if err := controller.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce returned error: %v", err)
	}
	if client.listCalls != 1 {
		t.Fatalf("expected exactly one snapshot fetch, got=%d", client.listCalls)
	}
	if store.upsertRunsCalls != 2 {
		t.Fatalf("expected one run upsert per environment, got=%d", store.upsertRunsCalls)
	}

	devRunURL := "https://prow.ci.openshift.org/view/gs/test-platform-results/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488"
	intRunURL := "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-Azure-ARO-HCP-main-periodic-integration-e2e-parallel/2029578186907455499"
	if _, found := store.GetStoredRun("dev", devRunURL); !found {
		t.Fatalf("expected dev batch run to be stored")
	}
	if _, found := store.GetStoredRun("int", intRunURL); !found {
		t.Fatalf("expected int periodic run to be stored")
	}
	if len(store.runs) != 2 {
		t.Fatalf("expected exactly two stored runs, got=%d", len(store.runs))
	}

	devCheckpoint, found := store.checkpoints[prowRunsCheckpointNameForEnvironment("dev")]
	if !found {
		t.Fatalf("expected dev checkpoint to be stored")
	}
	if devCheckpoint.Value != devStartedAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected dev checkpoint value: got=%q want=%q", devCheckpoint.Value, devStartedAt.Format(time.RFC3339Nano))
	}
	intCheckpoint, found := store.checkpoints[prowRunsCheckpointNameForEnvironment("int")]
	if !found {
		t.Fatalf("expected int checkpoint to be stored")
	}
	if intCheckpoint.Value != intStartedAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected int checkpoint value: got=%q want=%q", intCheckpoint.Value, intStartedAt.Format(time.RFC3339Nano))
	}
}

func TestFilterCompletedJobsByNameAndSinceIncludesBatchRuns(t *testing.T) {
	t.Parallel()

	since := mustParseRFC3339(t, "2026-04-20T09:00:00Z")
	jobs := []prowjobs.Job{
		{
			Spec: prowjobs.JobSpec{Job: "pull-ci-Azure-ARO-HCP-main-e2e-parallel"},
			Status: prowjobs.JobStatus{
				State:     "success",
				URL:       "gs://test-platform-results/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488",
				StartTime: mustParseRFC3339(t, "2026-04-20T12:00:00Z"),
			},
		},
		{
			Spec: prowjobs.JobSpec{Job: "pull-ci-Azure-ARO-HCP-main-e2e-parallel"},
			Status: prowjobs.JobStatus{
				State:     "pending",
				StartTime: mustParseRFC3339(t, "2026-04-20T12:01:00Z"),
			},
		},
	}
	filtered := filterCompletedJobsByNameAndSince(jobs, "pull-ci-Azure-ARO-HCP-main-e2e-parallel", since)
	if len(filtered) != 1 {
		t.Fatalf("unexpected filtered job count: got=%d want=1", len(filtered))
	}
	if filtered[0].Status.URL != "gs://test-platform-results/pr-logs/pull/batch/pull-ci-Azure-ARO-HCP-main-e2e-parallel/2029578186907455488" {
		t.Fatalf("expected batch run to remain after filtering, got=%q", filtered[0].Status.URL)
	}
}

func TestFilterCompletedJobsByNameAndSinceUsesStartTime(t *testing.T) {
	t.Parallel()

	since := mustParseRFC3339(t, "2026-04-22T18:00:00Z")
	jobs := []prowjobs.Job{
		{
			Spec: prowjobs.JobSpec{Job: "pull-ci-Azure-ARO-HCP-main-e2e-parallel"},
			Status: prowjobs.JobStatus{
				State:          "failure",
				StartTime:      mustParseRFC3339(t, "2026-04-22T17:45:00Z"),
				CompletionTime: mustParseRFC3339(t, "2026-04-22T19:10:00Z"),
			},
		},
		{
			Spec: prowjobs.JobSpec{Job: "pull-ci-Azure-ARO-HCP-main-e2e-parallel"},
			Status: prowjobs.JobStatus{
				State:          "failure",
				StartTime:      mustParseRFC3339(t, "2026-04-22T18:05:00Z"),
				CompletionTime: mustParseRFC3339(t, "2026-04-22T18:07:00Z"),
			},
		},
	}

	filtered := filterCompletedJobsByNameAndSince(jobs, "pull-ci-Azure-ARO-HCP-main-e2e-parallel", since)
	if len(filtered) != 1 {
		t.Fatalf("unexpected filtered job count: got=%d want=1", len(filtered))
	}
	if !filtered[0].Status.StartTime.Equal(mustParseRFC3339(t, "2026-04-22T18:05:00Z")) {
		t.Fatalf("expected filter to use start time cutoff, got start=%s", filtered[0].Status.StartTime.Format(time.RFC3339))
	}
}

func TestComputeNextProwRunsCheckpointUsesStartTime(t *testing.T) {
	t.Parallel()

	previous := mustParseRFC3339(t, "2026-04-22T18:00:00Z")
	jobs := []prowjobs.Job{
		{
			Status: prowjobs.JobStatus{
				State:          "failure",
				StartTime:      mustParseRFC3339(t, "2026-04-22T18:32:18Z"),
				CompletionTime: mustParseRFC3339(t, "2026-04-22T19:01:07Z"),
			},
		},
		{
			Status: prowjobs.JobStatus{
				State:          "failure",
				StartTime:      mustParseRFC3339(t, "2026-04-22T18:32:22Z"),
				CompletionTime: mustParseRFC3339(t, "2026-04-22T19:02:18Z"),
			},
		},
		{
			Status: prowjobs.JobStatus{
				State:          "failure",
				StartTime:      mustParseRFC3339(t, "2026-04-22T18:27:55Z"),
				CompletionTime: mustParseRFC3339(t, "2026-04-22T19:36:06Z"),
			},
		},
	}

	got := computeNextProwRunsCheckpoint(previous, jobs)
	want := mustParseRFC3339(t, "2026-04-22T18:32:22Z")
	if !got.Equal(want) {
		t.Fatalf("checkpoint should advance by latest start time: got=%s want=%s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}

func mustParseRFC3339(t *testing.T, value string) time.Time {
	t.Helper()

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse RFC3339 %q: %v", value, err)
	}
	return parsed.UTC()
}

func testSourceOptions(t *testing.T, environments []string) *sourceoptions.Options {
	t.Helper()

	raw := sourceoptions.DefaultOptions()
	raw.Environments = environments

	validated, err := raw.Validate()
	if err != nil {
		t.Fatalf("validate source options: %v", err)
	}
	completed, err := validated.Complete(context.Background())
	if err != nil {
		t.Fatalf("complete source options: %v", err)
	}
	return completed
}

type fakeProwSnapshotClient struct {
	jobs      []prowjobs.Job
	listCalls int
}

func (f *fakeProwSnapshotClient) ListJobs(_ context.Context) ([]prowjobs.Job, error) {
	f.listCalls++
	return append([]prowjobs.Job(nil), f.jobs...), nil
}

func (f *fakeProwSnapshotClient) GetJobHistoryPage(_ context.Context, historyPathOrURL string) (prowjobs.JobHistoryPage, error) {
	return prowjobs.JobHistoryPage{}, fmt.Errorf("unexpected history page request %q", historyPathOrURL)
}

type fakeProwRunsStore struct {
	runs            map[string]contracts.RunRecord
	checkpoints     map[string]contracts.CheckpointRecord
	upsertRunsCalls int
}

func (f *fakeProwRunsStore) GetStoredRun(environment string, runURL string) (contracts.RunRecord, bool) {
	row, found := f.runs[f.runKey(environment, runURL)]
	return row, found
}

func (f *fakeProwRunsStore) UpsertRuns(_ context.Context, runs []contracts.RunRecord) error {
	f.upsertRunsCalls++
	for _, run := range runs {
		f.runs[f.runKey(run.Environment, run.RunURL)] = run
	}
	return nil
}

func (f *fakeProwRunsStore) ListRuns(_ context.Context) ([]contracts.RunRecord, error) {
	rows := make([]contracts.RunRecord, 0, len(f.runs))
	for _, row := range f.runs {
		rows = append(rows, row)
	}
	return rows, nil
}

func (f *fakeProwRunsStore) ListRunKeys(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(f.runs))
	for key := range f.runs {
		keys = append(keys, key)
	}
	return keys, nil
}

func (f *fakeProwRunsStore) ListRunDates(_ context.Context) ([]string, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) ListRunsByDate(_ context.Context, environment string, date string) ([]contracts.RunRecord, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) GetRun(_ context.Context, environment string, runURL string) (contracts.RunRecord, bool, error) {
	row, found := f.GetStoredRun(environment, runURL)
	return row, found, nil
}

func (f *fakeProwRunsStore) UpsertPullRequests(_ context.Context, rows []contracts.PullRequestRecord) error {
	return nil
}

func (f *fakeProwRunsStore) ListPullRequests(_ context.Context) ([]contracts.PullRequestRecord, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) GetPullRequest(_ context.Context, prNumber int) (contracts.PullRequestRecord, bool, error) {
	return contracts.PullRequestRecord{}, false, nil
}

func (f *fakeProwRunsStore) UpsertArtifactFailures(_ context.Context, rows []contracts.ArtifactFailureRecord) error {
	return nil
}

func (f *fakeProwRunsStore) ListArtifactRunKeys(_ context.Context) ([]string, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) ListArtifactFailuresByRun(_ context.Context, environment string, runURL string) ([]contracts.ArtifactFailureRecord, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) UpsertRawFailures(_ context.Context, rows []contracts.RawFailureRecord) error {
	return nil
}

func (f *fakeProwRunsStore) ListRawFailures(_ context.Context) ([]contracts.RawFailureRecord, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) ListRawFailureRunKeys(_ context.Context) ([]string, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) ListRawFailuresByRun(_ context.Context, environment string, runURL string) ([]contracts.RawFailureRecord, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) ListRawFailuresByDate(_ context.Context, environment string, date string) ([]contracts.RawFailureRecord, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) UpsertMetricsDaily(_ context.Context, rows []contracts.MetricDailyRecord) error {
	return nil
}

func (f *fakeProwRunsStore) ListMetricsDaily(_ context.Context) ([]contracts.MetricDailyRecord, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) ListMetricsDailyByDate(_ context.Context, environment string, date string) ([]contracts.MetricDailyRecord, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) ListMetricDates(_ context.Context) ([]string, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) ListMetricsDailyForDates(_ context.Context, environments []string, dates []string) ([]contracts.MetricDailyRecord, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) SumMetricByEnvironmentForDates(_ context.Context, metric string, environments []string, dates []string) (map[string]float64, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) UpsertTestMetadataDaily(_ context.Context, rows []contracts.TestMetadataDailyRecord) error {
	return nil
}

func (f *fakeProwRunsStore) ListTestMetadataDailyByDate(_ context.Context, environment string, date string) ([]contracts.TestMetadataDailyRecord, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) ListTestMetadataDatesByEnvironment(_ context.Context, environment string, period string) ([]string, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) ListBelowTargetTestMetadataByDate(_ context.Context, environment string, date string, period string, targetPassRate float64, minRuns int, limit int) ([]contracts.TestMetadataDailyRecord, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) UpsertCheckpoints(_ context.Context, rows []contracts.CheckpointRecord) error {
	for _, row := range rows {
		f.checkpoints[row.Name] = row
	}
	return nil
}

func (f *fakeProwRunsStore) GetCheckpoint(_ context.Context, name string) (contracts.CheckpointRecord, bool, error) {
	row, found := f.checkpoints[name]
	return row, found, nil
}

func (f *fakeProwRunsStore) AppendDeadLetters(_ context.Context, rows []contracts.DeadLetterRecord) error {
	return nil
}

func (f *fakeProwRunsStore) ListDeadLetters(_ context.Context, limit int) ([]contracts.DeadLetterRecord, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) ReplaceMaterializedWeek(_ context.Context, week contracts.MaterializedWeek) error {
	return nil
}

func (f *fakeProwRunsStore) ListFailurePatterns(_ context.Context) ([]semanticcontracts.FailurePatternRecord, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) GetSemanticWeekSummary(_ context.Context) (contracts.SemanticWeekSummary, error) {
	return contracts.SemanticWeekSummary{}, nil
}

func (f *fakeProwRunsStore) ListReviewQueue(_ context.Context) ([]semanticcontracts.ReviewItemRecord, error) {
	return nil, nil
}

func (f *fakeProwRunsStore) Close() error {
	return nil
}

func (f *fakeProwRunsStore) runKey(environment string, runURL string) string {
	return environment + "\x00" + runURL
}
