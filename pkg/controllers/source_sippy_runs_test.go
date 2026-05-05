package controllers

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/go-logr/logr"

	sippysource "ci-failure-atlas/pkg/source/sippy"
	"ci-failure-atlas/pkg/store/contracts"
)

func TestSyncEnvironmentFetchesAllConfiguredSippyJobs(t *testing.T) {
	t.Parallel()

	opts := testSourceOptions(t, []string{"int"})
	periodicJobName := "periodic-ci-Azure-ARO-HCP-main-periodic-integration-e2e-parallel"
	branchJobName := "branch-ci-Azure-ARO-HCP-main-e2e-integration-e2e-parallel"
	periodicStartedAt := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second)
	branchStartedAt := time.Now().UTC().Add(-8 * time.Minute).Truncate(time.Second)

	client := &fakeSippyRunsClient{
		runsByJobName: map[string][]sippysource.JobRun{
			periodicJobName: {
				{
					RunURL:    "gs://test-platform-results/logs/periodic-ci-Azure-ARO-HCP-main-periodic-integration-e2e-parallel/2029578186907455499",
					JobName:   periodicJobName,
					StartedAt: periodicStartedAt,
					Failed:    true,
				},
			},
			branchJobName: {
				{
					RunURL:    "gs://test-platform-results/logs/branch-ci-Azure-ARO-HCP-main-e2e-integration-e2e-parallel/2029578186907455500",
					JobName:   branchJobName,
					StartedAt: branchStartedAt,
					Failed:    false,
				},
			},
		},
	}
	store := &fakeProwRunsStore{
		runs:        map[string]contracts.RunRecord{},
		checkpoints: map[string]contracts.CheckpointRecord{},
	}

	controller, err := newSourceSippyRunsController(logr.Discard(), Dependencies{
		Store:  store,
		Source: opts,
	}, client)
	if err != nil {
		t.Fatalf("newSourceSippyRunsController returned error: %v", err)
	}

	if err := controller.syncEnvironment(context.Background(), "int"); err != nil {
		t.Fatalf("syncEnvironment returned error: %v", err)
	}

	if got := client.recordedJobNames(); len(got) != 2 || got[0] != branchJobName || got[1] != periodicJobName {
		t.Fatalf("unexpected job names queried: got=%v", got)
	}

	periodicRunURL := "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-Azure-ARO-HCP-main-periodic-integration-e2e-parallel/2029578186907455499"
	if row, found := store.GetStoredRun("int", periodicRunURL); !found {
		t.Fatalf("expected periodic int run to be stored")
	} else if row.JobName != periodicJobName {
		t.Fatalf("unexpected periodic job name: got=%q want=%q", row.JobName, periodicJobName)
	}

	branchRunURL := "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/branch-ci-Azure-ARO-HCP-main-e2e-integration-e2e-parallel/2029578186907455500"
	if row, found := store.GetStoredRun("int", branchRunURL); !found {
		t.Fatalf("expected branch int run to be stored")
	} else if row.JobName != branchJobName {
		t.Fatalf("unexpected branch job name: got=%q want=%q", row.JobName, branchJobName)
	}

	checkpoint, found := store.checkpoints[checkpointNameForEnvironment("int")]
	if !found {
		t.Fatalf("expected int checkpoint to be stored")
	}
	if checkpoint.Value != branchStartedAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected checkpoint value: got=%q want=%q", checkpoint.Value, branchStartedAt.Format(time.RFC3339Nano))
	}
}

func TestSyncSingleRunByKeySearchesAllConfiguredSippyJobs(t *testing.T) {
	t.Parallel()

	opts := testSourceOptions(t, []string{"int"})
	branchJobName := "branch-ci-Azure-ARO-HCP-main-e2e-integration-e2e-parallel"
	runURL := "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/branch-ci-Azure-ARO-HCP-main-e2e-integration-e2e-parallel/2029578186907455500"

	client := &fakeSippyRunsClient{
		runsByJobName: map[string][]sippysource.JobRun{
			branchJobName: {
				{
					RunURL:    runURL,
					JobName:   branchJobName,
					StartedAt: time.Now().UTC().Add(-15 * time.Minute).Truncate(time.Second),
					Failed:    true,
				},
			},
		},
	}
	store := &fakeProwRunsStore{
		runs:        map[string]contracts.RunRecord{},
		checkpoints: map[string]contracts.CheckpointRecord{},
	}

	controller, err := newSourceSippyRunsController(logr.Discard(), Dependencies{
		Store:  store,
		Source: opts,
	}, client)
	if err != nil {
		t.Fatalf("newSourceSippyRunsController returned error: %v", err)
	}

	if err := controller.syncSingleRunByKey(context.Background(), "int|"+runURL); err != nil {
		t.Fatalf("syncSingleRunByKey returned error: %v", err)
	}

	row, found := store.GetStoredRun("int", runURL)
	if !found {
		t.Fatalf("expected synced run to be stored")
	}
	if row.JobName != branchJobName {
		t.Fatalf("unexpected synced job name: got=%q want=%q", row.JobName, branchJobName)
	}
	if got := client.recordedJobNames(); len(got) != 2 {
		t.Fatalf("expected both configured job queries, got=%v", got)
	}
}

type fakeSippyRunsClient struct {
	runsByJobName map[string][]sippysource.JobRun
	jobRunCalls   []sippysource.ListJobRunsOptions
}

func (f *fakeSippyRunsClient) ListPullRequests(_ context.Context, opts sippysource.ListPullRequestsOptions) ([]sippysource.PullRequest, error) {
	return nil, fmt.Errorf("unexpected pull request request: %+v", opts)
}

func (f *fakeSippyRunsClient) ListJobRuns(_ context.Context, opts sippysource.ListJobRunsOptions) ([]sippysource.JobRun, error) {
	f.jobRunCalls = append(f.jobRunCalls, opts)
	return append([]sippysource.JobRun(nil), f.runsByJobName[opts.JobName]...), nil
}

func (f *fakeSippyRunsClient) ListTests(_ context.Context, opts sippysource.ListTestsOptions) ([]sippysource.TestSummary, error) {
	return nil, fmt.Errorf("unexpected tests request: %+v", opts)
}

func (f *fakeSippyRunsClient) recordedJobNames() []string {
	out := make([]string, 0, len(f.jobRunCalls))
	for _, call := range f.jobRunCalls {
		out = append(out, call.JobName)
	}
	sort.Strings(out)
	return out
}
